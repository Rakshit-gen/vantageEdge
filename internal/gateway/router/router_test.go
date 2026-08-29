package router

import (
	"net/http"
	"testing"
)

func TestCacheableResponse(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{"plain 200", http.Header{}, true},
		{"public cache-control", http.Header{"Cache-Control": {"public, max-age=60"}}, true},
		{"no-store", http.Header{"Cache-Control": {"no-store"}}, false},
		{"no-cache", http.Header{"Cache-Control": {"no-cache"}}, false},
		{"private", http.Header{"Cache-Control": {"private, max-age=60"}}, false},
		{"set-cookie", http.Header{"Set-Cookie": {"session=abc"}}, false},
		{"event stream", http.Header{"Content-Type": {"text/event-stream"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheableResponse(&http.Response{Header: tt.header}); got != tt.want {
				t.Errorf("cacheableResponse(%v) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestResolveSubdomain(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		header  string
		want    string
		wantErr bool
	}{
		{name: "from host", host: "acme.vantageedge.dev", want: "acme"},
		{name: "from host with port", host: "acme.localhost:8000", want: "acme"},
		{name: "header overrides host", host: "vantageedge-gateway.onrender.com", header: "acme", want: "acme"},
		{name: "header used when host has no subdomain", host: "localhost:8000", header: "acme", want: "acme"},
		{name: "no header and unusable host", host: "localhost", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Host: tt.host, Header: http.Header{}}
			if tt.header != "" {
				r.Header.Set("X-Tenant-Subdomain", tt.header)
			}
			got, err := resolveSubdomain(r)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
