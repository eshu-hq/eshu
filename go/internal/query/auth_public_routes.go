package query

import (
	"net/http"
	"strings"
)

func publicHTTPRoute(r *http.Request) bool {
	if publicHTTPPaths[r.URL.Path] {
		return true
	}
	if r.Method == http.MethodGet &&
		(r.URL.Path == "/api/v0/auth/oidc/login" ||
			r.URL.Path == "/api/v0/auth/oidc/callback") {
		return true
	}
	return publicSAMLHTTPRoute(r)
}

func publicSAMLHTTPRoute(r *http.Request) bool {
	const prefix = "/api/v0/auth/saml/providers/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	providerID, suffix, found := strings.Cut(rest, "/")
	if !found || providerID == "" {
		return false
	}
	switch suffix {
	case "metadata", "login":
		return r.Method == http.MethodGet
	case "acs":
		return r.Method == http.MethodPost
	default:
		return false
	}
}
