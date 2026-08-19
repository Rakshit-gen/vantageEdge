package router

import "testing"

func TestMatchPath(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"exact match", "/health", "/health", true},
		{"exact mismatch", "/health", "/healthz", false},
		{"trailing wildcard match", "/api/users/*", "/api/users/123", true},
		{"trailing wildcard nested path", "/api/users/*", "/api/users/123/profile", true},
		{"trailing wildcard requires prefix", "/api/users/*", "/api/orders/1", false},
		{"prefix wildcard bare", "/api/public/posts*", "/api/public/posts", true},
		{"prefix wildcard suffix", "/api/public/posts*", "/api/public/posts/1", true},
		{"middle wildcard", "/api/*/posts", "/api/v2/posts", true},
		{"middle wildcard no match", "/api/*/posts", "/api/v2/comments", false},
		{"only wildcard matches everything", "*", "/anything/at/all", true},
		{"empty pattern never matches", "", "/health", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchPath(tc.pattern, tc.path)
			if got != tc.want {
				t.Errorf("MatchPath(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestMatchMethod(t *testing.T) {
	cases := []struct {
		name    string
		methods []string
		method  string
		want    bool
	}{
		{"exact match", []string{"GET", "POST"}, "GET", true},
		{"case insensitive", []string{"get"}, "GET", true},
		{"no match", []string{"POST"}, "GET", false},
		{"empty list allows all", nil, "DELETE", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchMethod(tc.methods, tc.method)
			if got != tc.want {
				t.Errorf("MatchMethod(%v, %q) = %v, want %v", tc.methods, tc.method, got, tc.want)
			}
		})
	}
}
