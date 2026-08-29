package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vantageedge/backend/internal/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type ReverseProxy struct {
	client *http.Client
}

// NewReverseProxy builds a ReverseProxy with one shared client and
// transport (connection pooling across requests). maxTimeout is an
// absolute ceiling; per-request timeouts (from route/origin config) are
// applied via context and can only be shorter, never longer.
func NewReverseProxy(maxTimeout time.Duration) *ReverseProxy {
	if maxTimeout <= 0 {
		maxTimeout = 60 * time.Second
	}
	// The default transport keeps only 2 idle connections per host, which
	// throttles a gateway that funnels most traffic to a handful of
	// origins into constant connection churn (and ephemeral-port
	// exhaustion under load). Raise the pool and set explicit dial/handshake
	// timeouts so a slow origin can't tie a request up past its deadline.
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &ReverseProxy{
		client: &http.Client{
			Timeout:   maxTimeout,
			Transport: transport,
			// A gateway forwards the origin's 3xx to the caller verbatim;
			// it must not chase Location itself (wrong body/status, and a
			// redirect to an internal address would be an SSRF vector).
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ProxyRequest forwards a request to an origin and returns the response.
// If timeout is positive, it bounds this attempt via the request context
// (in addition to the client's own ceiling timeout).
//
// The returned cancel func must be called by the caller only after it has
// finished reading resp.Body (e.g. after WriteResponse), not deferred
// immediately here: canceling the context as soon as headers arrive would
// abort the in-flight body stream and truncate every response.
func (rp *ReverseProxy) ProxyRequest(
	ctx context.Context,
	req *http.Request,
	origin *models.Origin,
	pathRewrite *PathRewrite,
	timeout time.Duration,
) (*http.Response, context.CancelFunc, error) {
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}

	// Clone the request
	proxyReq := req.Clone(ctx)

	// Build the target URL
	targetURL := origin.URL
	if pathRewrite != nil {
		targetURL += pathRewrite.RewritePath(req.URL.Path)
	} else {
		targetURL += req.URL.Path
	}

	if req.URL.RawQuery != "" {
		targetURL += "?" + req.URL.RawQuery
	}

	// Parse and set the target URL
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		cancel()
		return nil, cancel, err
	}
	proxyReq.URL = parsedURL
	proxyReq.RequestURI = ""

	// Send the origin's host in the Host header, not the gateway's inbound
	// host. req.Clone copies the caller's Host (e.g. the tenant subdomain
	// or the gateway's own domain), and any origin that virtual-hosts —
	// most SaaS backends, httpbin, anything behind a shared proxy — 404s or
	// misroutes on an unrecognised Host. The caller's original host is
	// still forwarded as X-Forwarded-Host below.
	proxyReq.Host = parsedURL.Host

	// Remove hop-by-hop headers
	removeHopByHopHeaders(proxyReq)

	// Add forwarding headers. Append to (rather than overwrite) any
	// existing X-Forwarded-For so a chain of proxies is preserved, and
	// strip the port from RemoteAddr since X-Forwarded-For is
	// conventionally just the client IP.
	clientIP := req.RemoteAddr
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		clientIP = host
	}
	if existing := req.Header.Get("X-Forwarded-For"); existing != "" {
		proxyReq.Header.Set("X-Forwarded-For", existing+", "+clientIP)
	} else {
		proxyReq.Header.Set("X-Forwarded-For", clientIP)
	}
	// X-Forwarded-Proto is the request scheme (http/https), not the HTTP
	// version. Trust an inbound value first (the TLS-terminating load
	// balancer in front of the gateway sets it), then fall back to whether
	// this hop itself is TLS.
	proto := req.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
		if req.TLS != nil {
			proto = "https"
		}
	}
	proxyReq.Header.Set("X-Forwarded-Proto", proto)
	proxyReq.Header.Set("X-Forwarded-Host", req.Host)

	// Propagate the gateway's trace context so an OTel-instrumented origin
	// continues the same distributed trace instead of starting a new one.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(proxyReq.Header))

	// Send the request
	resp, err := rp.client.Do(proxyReq)
	if err != nil {
		cancel()
		return nil, cancel, err
	}

	return resp, cancel, nil
}

// WriteResponse writes a proxied response back to the client
func (rp *ReverseProxy) WriteResponse(w http.ResponseWriter, resp *http.Response) error {
	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Remove hop-by-hop headers
	removeHopByHopHeadersFromWriter(w)

	// Write status code
	w.WriteHeader(resp.StatusCode)

	defer resp.Body.Close()

	// Server-Sent Events (and any body the origin sends without a length)
	// must reach the caller as they arrive; a plain io.Copy buffers them
	// until the connection closes, which for an event stream is never.
	if isStreaming(resp) {
		return copyAndFlush(w, resp.Body)
	}

	_, err := io.Copy(w, resp.Body)
	return err
}

// isStreaming reports whether a response should be flushed to the client
// incrementally rather than copied in one shot.
func isStreaming(resp *http.Response) bool {
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return true
	}
	// ContentLength -1 means the origin did not declare a length (chunked
	// or streamed); a known length is a bounded body that io.Copy handles.
	return resp.ContentLength < 0
}

// copyAndFlush streams src to w, flushing after every chunk so the caller
// sees data without waiting for the whole body.
func copyAndFlush(w http.ResponseWriter, src io.Reader) error {
	rc := http.NewResponseController(w)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			rc.Flush() // best effort: ErrNotSupported is harmless here
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

// PathRewrite handles path rewriting rules
type PathRewrite struct {
	Pattern string
	Target  string
}

func (pr *PathRewrite) RewritePath(originalPath string) string {
	if pr == nil || pr.Pattern == "" {
		return originalPath
	}

	// Simple string replacement
	// For complex patterns, consider using regex
	return strings.ReplaceAll(originalPath, pr.Pattern, pr.Target)
}

// removeHopByHopHeaders removes hop-by-hop headers from a request
func removeHopByHopHeaders(req *http.Request) {
	hopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, header := range hopHeaders {
		req.Header.Del(header)
	}
}

// removeHopByHopHeadersFromWriter removes hop-by-hop headers from response writer
func removeHopByHopHeadersFromWriter(w http.ResponseWriter) {
	hopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, header := range hopHeaders {
		w.Header().Del(header)
	}
}
