package web

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin rejects unsafe-method requests that a browser marks as
// cross-origin, using Sec-Fetch-Site with an Origin fallback. Requests that
// carry neither header (non-browser clients) are allowed.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !isSameOriginRequest(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isSameOriginRequest reports whether r was issued from the same origin, or
// by a client that sends neither Sec-Fetch-Site nor Origin.
func isSameOriginRequest(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
