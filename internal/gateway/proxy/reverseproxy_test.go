package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vantageedge/backend/internal/models"
)

// A gateway forwards the origin's redirect to the caller; it must not
// follow Location itself.
func TestProxyRequest_DoesNotFollowRedirects(t *testing.T) {
	var originHits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer origin.Close()

	req := httptest.NewRequest(http.MethodGet, "http://tenant.gateway.example/start", nil)
	rp := NewReverseProxy(5_000_000_000)
	resp, cancel, err := rp.ProxyRequest(context.Background(), req, &models.Origin{URL: origin.URL}, nil, 0)
	if err != nil {
		t.Fatalf("ProxyRequest: %v", err)
	}
	defer cancel()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect passed through, not followed)", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/elsewhere" {
		t.Errorf("Location = %q, want /elsewhere", got)
	}
	if originHits != 1 {
		t.Errorf("origin hit %d times, want 1 (redirect not chased)", originHits)
	}
}

// An event stream must be flushed to the client incrementally, and its
// bytes must arrive intact.
func TestWriteResponse_StreamsEventStream(t *testing.T) {
	body := "data: one\n\ndata: two\n\n"
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": {"text/event-stream"}},
		Body:          io.NopCloser(newSlowReader(body)),
		ContentLength: -1,
	}

	rec := httptest.NewRecorder()
	if err := (&ReverseProxy{}).WriteResponse(rec, resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	if !rec.Flushed {
		t.Error("expected the event stream to be flushed to the client")
	}
	if rec.Body.String() != body {
		t.Errorf("body = %q, want %q", rec.Body.String(), body)
	}
}

// newSlowReader hands back one byte per Read so a non-flushing copy would
// visibly buffer.
func newSlowReader(s string) io.Reader { return &slowReader{data: []byte(s)} }

type slowReader struct {
	data []byte
	pos  int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

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
