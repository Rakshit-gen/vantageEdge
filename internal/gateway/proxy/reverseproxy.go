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
	return &ReverseProxy{
		client: &http.Client{
			Timeout: maxTimeout,
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
	proxyReq.Header.Set("X-Forwarded-Proto", req.Proto)
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

	// Copy body
	_, err := io.Copy(w, resp.Body)
	resp.Body.Close()

	return err
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
