// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func appProviderTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// appProviderTestServer answers every installation-token mint with a token
// derived from the installation in the path, and records every request. A
// production provider must mint per claim through this endpoint only — never
// from a static value — and must select the installation by claim scope.
func appProviderTestServer(t *testing.T, mu *sync.Mutex, seen *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*seen = append(*seen, r.Method+" "+r.URL.Path+"|"+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/app/installations/") ||
			!strings.HasSuffix(r.URL.Path, "/access_tokens") {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		installation := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/app/installations/"), "/access_tokens")
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.Contains(auth, "PRIVATE") {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok-for-" + installation})
	}))
	t.Cleanup(server.Close)
	return server
}

func appProviderClaimItem(scopeID string) workflow.WorkItem {
	item := testWorkItem()
	item.ScopeID = scopeID
	return item
}

// TestNewAppTokenProviderSelectsInstallationPerScope proves the production
// constructor mints through the GitHub endpoint once per claim and selects
// the installation by claim scope: two scopes mint against two installation
// paths and spend distinct tokens.
func TestNewAppTokenProviderSelectsInstallationPerScope(t *testing.T) {
	t.Parallel()

	keyPEM := appProviderTestKey(t)
	var mu sync.Mutex
	var seen []string
	server := appProviderTestServer(t, &mu, &seen)
	provider, err := NewAppTokenProvider(AppTokenProviderConfig{
		AppID:      "42",
		PrivateKey: keyPEM,
		Installations: map[string]string{
			"scope-a": "111",
			"scope-b": "222",
		},
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
	}
	for scopeID, installation := range map[string]string{"scope-a": "111", "scope-b": "222"} {
		token, err := provider(context.Background(), appProviderClaimItem(scopeID))
		if err != nil {
			t.Fatalf("provider(%q) error = %v, want nil", scopeID, err)
		}
		if want := "tok-for-" + installation; token != want {
			t.Fatalf("provider(%q) = %q, want %q", scopeID, token, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("mint requests = %d, want 2 (one per claim, no cache)", len(seen))
	}
	for _, entry := range seen {
		if !strings.Contains(entry, "/app/installations/") {
			t.Fatalf("mint request = %q, want installation token endpoint", entry)
		}
	}
}

// TestNewAppTokenProviderUnknownScopeFailsClosed proves a claim for a scope
// with no mapped installation fails with a bounded config error instead of
// minting against a default installation.
func TestNewAppTokenProviderUnknownScopeFailsClosed(t *testing.T) {
	t.Parallel()

	provider, err := NewAppTokenProvider(AppTokenProviderConfig{
		AppID:         "42",
		PrivateKey:    appProviderTestKey(t),
		Installations: map[string]string{"scope-a": "111"},
	})
	if err != nil {
		t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
	}
	_, err = provider(context.Background(), appProviderClaimItem("scope-unknown"))
	if err == nil {
		t.Fatal("provider() error = nil, want unknown-scope denial")
	}
	var configErr *app.ConfigError
	if !errors.As(err, &configErr) {
		t.Fatalf("provider() error type = %T, want *app.ConfigError", err)
	}
}

// TestNewAppTokenProviderRejectsBadConfig pins construction-time validation:
// blank credentials, no installations, and blank map entries all fail before
// any claim can burn.
func TestNewAppTokenProviderRejectsBadConfig(t *testing.T) {
	t.Parallel()

	keyPEM := appProviderTestKey(t)
	tests := []struct {
		name string
		cfg  AppTokenProviderConfig
	}{
		{name: "empty app id", cfg: AppTokenProviderConfig{PrivateKey: keyPEM, Installations: map[string]string{"s": "1"}}},
		{name: "empty private key", cfg: AppTokenProviderConfig{AppID: "42", Installations: map[string]string{"s": "1"}}},
		{name: "no installations", cfg: AppTokenProviderConfig{AppID: "42", PrivateKey: keyPEM}},
		{name: "blank scope", cfg: AppTokenProviderConfig{AppID: "42", PrivateKey: keyPEM, Installations: map[string]string{" ": "1"}}},
		{name: "blank installation", cfg: AppTokenProviderConfig{AppID: "42", PrivateKey: keyPEM, Installations: map[string]string{"s": " "}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewAppTokenProvider(tt.cfg); err == nil {
				t.Fatalf("NewAppTokenProvider() error = nil, want rejection of %s", tt.name)
			}
		})
	}
}

// TestNewAppTokenProviderMintDenialSurfacesBounded proves a live mint denial
// (revoked credentials) surfaces the bounded denial error instead of raw
// transport text, so the claim path can fail it terminal.
func TestNewAppTokenProviderMintDenialSurfacesBounded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "{}", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	provider, err := NewAppTokenProvider(AppTokenProviderConfig{
		AppID:         "42",
		PrivateKey:    appProviderTestKey(t),
		Installations: map[string]string{"scope-a": "111"},
		BaseURL:       server.URL,
		HTTPClient:    server.Client(),
	})
	if err != nil {
		t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
	}
	_, err = provider(context.Background(), appProviderClaimItem("scope-a"))
	if err == nil {
		t.Fatal("provider() error = nil, want mint denial")
	}
	var denied *app.AuthDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("provider() error type = %T, want *app.AuthDeniedError", err)
	}
	if got := err.Error(); strings.Contains(got, "PRIVATE") {
		t.Fatalf("denial carries key material: %q", got)
	}
}
