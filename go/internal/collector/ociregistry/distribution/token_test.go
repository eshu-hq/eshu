// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestFetchBearerTokenUsesScopeAndCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "token" {
			t.Fatalf("BasicAuth = %q/%q/%v, want user/token/true", username, password, ok)
		}
		if got, want := r.URL.Query().Get("service"), "registry.example"; got != want {
			t.Fatalf("service = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("scope"), "repository:team/api:pull"; got != want {
			t.Fatalf("scope = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"bearer-token"}`))
	}))
	defer server.Close()

	token, err := FetchBearerToken(context.Background(), TokenConfig{
		Realm:    server.URL,
		Service:  "registry.example",
		Scope:    "repository:team/api:pull",
		Username: "user",
		Password: "token",
	})
	if err != nil {
		t.Fatalf("FetchBearerToken() error = %v", err)
	}
	if token != "bearer-token" {
		t.Fatalf("token = %q, want bearer-token", token)
	}
}

func TestFetchBearerTokenAcceptsAccessTokenField(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
	}))
	defer server.Close()

	token, err := FetchBearerToken(context.Background(), TokenConfig{Realm: server.URL})
	if err != nil {
		t.Fatalf("FetchBearerToken() error = %v", err)
	}
	if token != "access-token" {
		t.Fatalf("token = %q, want access-token", token)
	}
}

func TestFetchBearerTokenRequiresTokenField(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := FetchBearerToken(context.Background(), TokenConfig{Realm: server.URL}); err == nil {
		t.Fatal("FetchBearerToken() error = nil")
	}
}

func TestFetchBearerTokenStatusFailureWrapsSDKHTTPErrorWithoutLeakingRealm(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private registry token response", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := FetchBearerToken(context.Background(), TokenConfig{
		Realm:   server.URL + "/token?existing=secret",
		Service: "private-registry",
		Scope:   "repository:team/api:pull",
	})
	if err == nil {
		t.Fatal("FetchBearerToken() error = nil, want status failure")
	}
	if got := failureClass(err); got != "registry_auth_denied" {
		t.Fatalf("FailureClass() = %q, want registry_auth_denied", got)
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchBearerToken() error = %T %[1]v, want SDK HTTPError cause", err)
	}
	if got := httpErr.StatusCode; got != http.StatusForbidden {
		t.Fatalf("SDK HTTPError StatusCode = %d, want %d", got, http.StatusForbidden)
	}
	for _, leaked := range []string{"team/api", "private-registry", "existing=secret", "private registry token"} {
		if strings.Contains(err.Error(), leaked) || strings.Contains(failureDetails(err), leaked) {
			t.Fatalf("token status failure leaked %q: error=%q details=%q", leaked, err.Error(), failureDetails(err))
		}
	}
}

func TestFetchBearerTokenTransportFailureWrapsSDKHTTPErrorWithoutLeakingRealm(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial denied for registry.example.test/token")
	_, err := FetchBearerToken(context.Background(), TokenConfig{
		Realm:   "https://registry.example.test/token",
		Service: "private-registry",
		Scope:   "repository:team/api:pull",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})},
	})
	if err == nil {
		t.Fatal("FetchBearerToken() error = nil, want transport failure")
	}
	if got := failureClass(err); got != "registry_retryable_failure" {
		t.Fatalf("FailureClass() = %q, want registry_retryable_failure", got)
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchBearerToken() error = %T %[1]v, want SDK HTTPError cause", err)
	}
	if httpErr.StatusCode != 0 {
		t.Fatalf("SDK HTTPError StatusCode = %d, want 0 for transport failure", httpErr.StatusCode)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("FetchBearerToken() error = %v, want transport cause", err)
	}
	for _, leaked := range []string{"registry.example.test", "team/api", "private-registry"} {
		if strings.Contains(err.Error(), leaked) || strings.Contains(failureDetails(err), leaked) {
			t.Fatalf("token transport failure leaked %q: error=%q details=%q", leaked, err.Error(), failureDetails(err))
		}
	}
}
