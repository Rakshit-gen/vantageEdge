package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vantageedge/backend/internal/models"
)

// The origin must see its own host in the Host header, not the gateway's
// inbound host — origins that virtual-host otherwise 404 or misroute.
func TestProxyRequest_RewritesHostToOrigin(t *testing.T) {
	var gotHost, gotFwdHost string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotFwdHost = r.Header.Get("X-Forwarded-Host")
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	req := httptest.NewRequest(http.MethodGet, "http://tenant.gateway.example/get", nil)
	req.RemoteAddr = "203.0.113.5:44444"

	rp := NewReverseProxy(5_000_000_000)
	resp, cancel, err := rp.ProxyRequest(context.Background(), req, &models.Origin{URL: origin.URL}, nil, 0)
	if err != nil {
		t.Fatalf("ProxyRequest: %v", err)
	}
	defer cancel()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	originHost := origin.Listener.Addr().String()
	if gotHost != originHost {
		t.Errorf("origin saw Host %q, want %q", gotHost, originHost)
	}
	if gotFwdHost != "tenant.gateway.example" {
		t.Errorf("X-Forwarded-Host = %q, want the caller's original host", gotFwdHost)
	}
}
