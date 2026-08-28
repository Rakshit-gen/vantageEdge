package middleware

import "net/http"

// MaxControlPlaneBodyBytes caps a control-plane request body. Every handler
// here decodes small JSON (a route, an origin, an API-key request); nothing
// legitimately posts more than this, and the cap keeps a hostile or buggy
// client from making the server buffer an unbounded body.
const MaxControlPlaneBodyBytes = 1 << 20 // 1 MiB

// LimitBody wraps r.Body in http.MaxBytesReader so an oversized body fails
// the handler's json.Decode with a clear error instead of being read in full.
func LimitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxControlPlaneBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}
