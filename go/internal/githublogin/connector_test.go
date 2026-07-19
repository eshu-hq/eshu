// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGitHubServer stands in for both github.com's OAuth host and its REST
// API host (the real connector may point at two different base URLs, e.g.
// github.com vs api.github.com; tests point both at one fake server since
// the connector treats them as independently configured strings).
func fakeGitHubServer(t *testing.T, tokenOK bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if !tokenOK {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "bad_verification_code",
				"error_description": "The code passed is incorrect or expired.",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "gho_test_token",
			"token_type":   "bearer",
			"scope":        "read:org,user:email",
		})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "gho_test_token")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "login": "octocat"})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "gho_test_token")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "octocat@noreply.example.test", "verified": false, "primary": false},
			{"email": "octocat@example.test", "verified": true, "primary": true},
		})
	})
	mux.HandleFunc("/user/memberships/orgs", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "gho_test_token")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"state": "active", "organization": map[string]any{"login": "Eshu-HQ"}},
			{"state": "pending", "organization": map[string]any{"login": "some-other-org"}},
		})
	})
	mux.HandleFunc("/user/teams", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "gho_test_token")
		page := r.URL.Query().Get("page")
		if page != "1" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"slug": "developers", "organization": map[string]any{"login": "eshu-hq"}},
			{"slug": "everyone", "organization": map[string]any{"login": "some-other-org"}},
		})
	})
	return httptest.NewServer(mux)
}

func requireBearer(t *testing.T, r *http.Request, want string) {
	t.Helper()
	got := r.Header.Get("Authorization")
	if got != "Bearer "+want {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer "+want)
	}
	if r.Header.Get("X-GitHub-Api-Version") == "" && r.URL.Path != "/login/oauth/access_token" {
		t.Errorf("missing X-GitHub-Api-Version header on %s", r.URL.Path)
	}
}

func TestConnectorExchangeAndFetchIdentity(t *testing.T) {
	t.Parallel()

	server := fakeGitHubServer(t, true)
	defer server.Close()

	connector, err := NewGitHubConnector(context.Background(), ProviderConfig{
		ProviderConfigID: "github-dev",
		BaseURL:          server.URL,
		APIBaseURL:       server.URL,
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		RedirectURL:      "https://eshu.example.test/api/v0/auth/github/callback",
		AllowedOrgs:      []string{"eshu-hq"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector() error = %v", err)
	}

	authURL := connector.AuthCodeURL("state-secret")
	if !strings.Contains(authURL, server.URL+"/login/oauth/authorize") ||
		!strings.Contains(authURL, "state=state-secret") ||
		!strings.Contains(authURL, "client_id=client-id") {
		t.Fatalf("AuthCodeURL() = %q, want authorize endpoint with state and client_id", authURL)
	}

	tokens, err := connector.Exchange(context.Background(), "auth-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if tokens.AccessToken != "gho_test_token" {
		t.Fatalf("AccessToken = %q, want gho_test_token", tokens.AccessToken)
	}

	identity, err := connector.FetchIdentity(context.Background(), tokens.AccessToken, []string{"eshu-hq"})
	if err != nil {
		t.Fatalf("FetchIdentity() error = %v", err)
	}
	if identity.Subject != "12345" || identity.Login != "octocat" {
		t.Fatalf("identity subject/login = %#v, want 12345/octocat", identity)
	}
	if identity.Email != "octocat@example.test" {
		t.Fatalf("identity email = %q, want the verified primary email only", identity.Email)
	}
	if len(identity.ActiveOrgs) != 1 || identity.ActiveOrgs[0] != "eshu-hq" {
		t.Fatalf("active orgs = %v, want [eshu-hq] (case-folded, pending excluded, non-allowed excluded)", identity.ActiveOrgs)
	}
	if len(identity.TeamHandles) != 1 || identity.TeamHandles[0] != "eshu-hq/developers" {
		t.Fatalf("team handles = %v, want [eshu-hq/developers] (team in a non-allowed org excluded)", identity.TeamHandles)
	}
}

func TestConnectorExchangeSurfacesGitHubErrorResponse(t *testing.T) {
	t.Parallel()

	server := fakeGitHubServer(t, false)
	defer server.Close()

	connector, err := NewGitHubConnector(context.Background(), ProviderConfig{
		BaseURL:      server.URL,
		APIBaseURL:   server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://eshu.example.test/callback",
		AllowedOrgs:  []string{"eshu-hq"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector() error = %v", err)
	}

	if _, err := connector.Exchange(context.Background(), "bad-code"); err == nil {
		t.Fatal("Exchange() error = nil, want error for github's error_description response")
	}
}

func TestNewGitHubConnectorRequiresClientSecret(t *testing.T) {
	t.Parallel()

	_, err := NewGitHubConnector(context.Background(), ProviderConfig{
		ClientID:    "client-id",
		RedirectURL: "https://eshu.example.test/callback",
		AllowedOrgs: []string{"eshu-hq"},
	})
	if err == nil {
		t.Fatal("NewGitHubConnector() error = nil, want error when no client secret is configured")
	}
}

func TestConnectorFetchIdentityRejectsNonOKStatus(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"message":"Bad credentials"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	connector, err := NewGitHubConnector(context.Background(), ProviderConfig{
		BaseURL:      server.URL,
		APIBaseURL:   server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://eshu.example.test/callback",
		AllowedOrgs:  []string{"eshu-hq"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector() error = %v", err)
	}
	if _, err := connector.FetchIdentity(context.Background(), "bad-token", []string{"eshu-hq"}); err == nil {
		t.Fatal("FetchIdentity() error = nil, want error on a non-200 /user response")
	}
}
