package proxy

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/vantageedge/backend/internal/models"
)

type ReverseProxy struct {
	client *http.Client
}

func NewReverseProxy() *ReverseProxy {
	return &ReverseProxy{
		client: &http.Client{
			Timeout: 30 * 1000000000, // 30 seconds
		},
	}
}

// ProxyRequest forwards a request to an origin and returns the response
func (rp *ReverseProxy) ProxyRequest(
	ctx context.Context,
	req *http.Request,
	origin *models.Origin,
	pathRewrite *PathRewrite,
) (*http.Response, error) {
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
		return nil, err
	}
	proxyReq.URL = parsedURL
	proxyReq.RequestURI = ""

	// Remove hop-by-hop headers
	removeHopByHopHeaders(proxyReq)

	// Add forwarding headers
	proxyReq.Header.Set("X-Forwarded-For", req.RemoteAddr)
	proxyReq.Header.Set("X-Forwarded-Proto", req.Proto)
	proxyReq.Header.Set("X-Forwarded-Host", req.Host)

	// Send the request
	resp, err := rp.client.Do(proxyReq)
	if err != nil {
		return nil, err
	}

	return resp, nil
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
