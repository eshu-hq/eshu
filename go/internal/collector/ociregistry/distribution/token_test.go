package distribution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
