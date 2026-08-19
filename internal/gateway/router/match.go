package router

import "strings"

// MatchPath reports whether requestPath matches pattern.
//
// Route patterns are authored with `*` as a wildcard, e.g. "/api/users/*"
// or "/api/*/posts" (see scripts/seed.sql and README examples). The
// original implementation sent this pattern straight into a Postgres
// `path LIKE pattern` query, where `%` — not `*` — is the wildcard
// character; `*` is matched literally by LIKE. That made every non-exact
// route pattern permanently unmatchable, so the gateway could only ever
// serve routes whose pattern was byte-identical to the request path.
//
// This does substring-based glob matching entirely in Go: split the
// pattern on `*` and require requestPath to start with the first segment,
// end with the last segment, and contain the remaining segments in order.
// A `*` matches any sequence of characters, including `/`, so
// "/api/users/*" matches "/api/users/123/profile".
func MatchPath(pattern, requestPath string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return pattern == requestPath
	}

	segments := strings.Split(pattern, "*")

	if !strings.HasPrefix(requestPath, segments[0]) {
		return false
	}
	remaining := requestPath[len(segments[0]):]

	last := len(segments) - 1
	if !strings.HasSuffix(remaining, segments[last]) {
		return false
	}
	if last > 0 {
		remaining = remaining[:len(remaining)-len(segments[last])]
	}

	for _, seg := range segments[1:last] {
		if seg == "" {
			continue
		}
		idx := strings.Index(remaining, seg)
		if idx == -1 {
			return false
		}
		remaining = remaining[idx+len(seg):]
	}

	return true
}

// MatchMethod reports whether method is in the route's allowed method list.
// An empty list is treated as "all methods" to match the DB default
// (`methods` defaults to a fixed set, but defensively allow-all avoids a
// misconfigured route silently rejecting every request).
func MatchMethod(methods []string, method string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}
