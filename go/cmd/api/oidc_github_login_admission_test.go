// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/githublogin"
	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestOIDCLoginAdmissionBindsNilServiceToNilPort proves the #8872 seam's
// fail-closed binding: a nil *oidclogin.Service yields an untyped nil
// port, never a value adapter wrapping nil, so the handler's serviceReady
// check fails closed to 503 instead of panicking on a nil backing
// service.
func TestOIDCLoginAdmissionBindsNilServiceToNilPort(t *testing.T) {
	t.Parallel()

	if got := oidcLoginAdmission(nil); got != nil {
		t.Fatalf("oidcLoginAdmission(nil) = %T(%v), want untyped nil", got, got)
	}
}

// TestGitHubLoginAdmissionBindsNilServiceToNilPort proves the same
// fail-closed binding for the GitHub login seam.
func TestGitHubLoginAdmissionBindsNilServiceToNilPort(t *testing.T) {
	t.Parallel()

	if got := githubLoginAdmission(nil); got != nil {
		t.Fatalf("githubLoginAdmission(nil) = %T(%v), want untyped nil", got, got)
	}
}

// TestOIDCLoginAdmissionCarriesExactAdapter proves the handler carries the
// exact injected service (identity, not merely non-nil) so eshu-api can
// later bind a core admission endpoint behind the same port.
func TestOIDCLoginAdmissionCarriesExactAdapter(t *testing.T) {
	t.Parallel()

	service := &oidclogin.Service{}
	if got := oidcLoginAdmission(service); got != query.OIDCLoginAdmission(oidcServiceAdapter{service}) {
		t.Fatalf("oidcLoginAdmission(service) = %T(%v), want the exact injected adapter %T(%v)", got, got, oidcServiceAdapter{service}, oidcServiceAdapter{service})
	}
}

// TestGitHubLoginAdmissionCarriesExactAdapter proves the handler carries
// the exact injected service for the GitHub login seam.
func TestGitHubLoginAdmissionCarriesExactAdapter(t *testing.T) {
	t.Parallel()

	service := &githublogin.Service{}
	if got := githubLoginAdmission(service); got != query.GitHubLoginAdmission(githubServiceAdapter{service}) {
		t.Fatalf("githubLoginAdmission(service) = %T(%v), want the exact injected adapter %T(%v)", got, got, githubServiceAdapter{service}, githubServiceAdapter{service})
	}
}

// TestLoginAdmissionNilServiceFailsClosed503 proves the end-to-end
// fail-closed path through the admission binding: a handler whose port
// unwrapped from a nil service answers 503 on every login route instead
// of serving through a nil backing store.
func TestLoginAdmissionNilServiceFailsClosed503(t *testing.T) {
	t.Parallel()

	oidcHandler := &query.OIDCLoginHandler{Service: oidcLoginAdmission(nil)}
	githubHandler := &query.GitHubLoginHandler{Service: githubLoginAdmission(nil)}
	oidcMux := http.NewServeMux()
	oidcHandler.Mount(oidcMux)
	githubMux := http.NewServeMux()
	githubHandler.Mount(githubMux)

	tests := []struct {
		name string
		mux  *http.ServeMux
		path string
	}{
		{"oidc login", oidcMux, "/api/v0/auth/oidc/login?provider_config_id=p&tenant_id=t&workspace_id=w"},
		{"oidc callback", oidcMux, "/api/v0/auth/oidc/callback?state=s&code=c"},
		{"github login", githubMux, "/api/v0/auth/github/login?provider_config_id=p&tenant_id=t&workspace_id=w"},
		{"github callback", githubMux, "/api/v0/auth/github/callback?state=s&code=c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			tt.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("GET %s with nil service = %d, want 503", tt.path, recorder.Code)
			}
		})
	}
}

// TestNewGitHubLoginHandlerDisabledByDefault pins the constructor's
// fail-closed default: unset env yields a nil handler and nil error,
// mirroring the OIDC path.
func TestNewGitHubLoginHandlerDisabledByDefault(t *testing.T) {
	t.Parallel()

	handler, err := newGitHubLoginHandler(func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("newGitHubLoginHandler() error = %v, want nil", err)
	}
	if handler != nil {
		t.Fatalf("newGitHubLoginHandler() = %#v, want nil when GitHub is unset", handler)
	}
}

// TestNewGitHubLoginHandlerEnabledWithoutConfigFileRequiresDatabase pins
// the DB requirement on the DB-only activation path: enabled with no
// config file and no database is a startup error, not a silent
// half-wired handler.
func TestNewGitHubLoginHandlerEnabledWithoutConfigFileRequiresDatabase(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ESHU_AUTH_GITHUB_ENABLED" {
			return "true"
		}
		return ""
	}
	if _, err := newGitHubLoginHandler(getenv, nil, nil, nil); err == nil {
		t.Fatal("newGitHubLoginHandler() error = nil, want an error when enabled with no database")
	}
}
