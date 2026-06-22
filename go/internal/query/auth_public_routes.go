package query

import "net/http"

func publicHTTPRoute(r *http.Request) bool {
	if publicHTTPPaths[r.URL.Path] {
		return true
	}
	return r.Method == http.MethodGet &&
		(r.URL.Path == "/api/v0/auth/oidc/login" ||
			r.URL.Path == "/api/v0/auth/oidc/callback")
}
