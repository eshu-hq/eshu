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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
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

// TestHostMintRetryableClampedToCap proves the retry-contract fix for the
// host mint path: a provider backoff past MaxRetryAfterDelay is clamped at
// classification, and the host verdict record carries the clamped delay plus
// the requested seconds so the retry-after gauge and requeue math read it.
func TestHostMintRetryableClampedToCap(t *testing.T) {
	t.Parallel()

	provider := func(context.Context, workflow.WorkItem) (string, error) {
		return "", app.NewRetryableError(app.ClassRateLimited, 3, 2*time.Hour, errors.New("mint github app token: status 429"))
	}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	recorder := &recordingResultRecorder{}
	source := mustNewSourceWithProvider(t, runner, recorder, provider, nil)

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want retryable mint failure")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false on mint failure")
	}
	var classified interface{ RetryAfterDelay() time.Duration }
	if !errors.As(err, &classified) {
		t.Fatal("mint failure does not expose RetryAfterDelay")
	}
	if got := classified.RetryAfterDelay(); got != MaxRetryAfterDelay {
		t.Fatalf("RetryAfterDelay() = %v, want clamped %v", got, MaxRetryAfterDelay)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d, want 0", len(runner.requests))
	}
	if len(recorder.results) != 1 {
		t.Fatalf("result records = %d, want exactly 1", len(recorder.results))
	}
	record := recorder.results[0]
	if record.Origin != ResultOriginHost || record.State != sdkcollector.ResultRetryable {
		t.Fatalf("result record = %+v, want host retryable verdict", record)
	}
	if record.RetryAfter != MaxRetryAfterDelay {
		t.Fatalf("record RetryAfter = %v, want %v", record.RetryAfter, MaxRetryAfterDelay)
	}
	if want := int((MaxRetryAfterDelay) / time.Second); record.RetryAfterRequestedSeconds != want {
		t.Fatalf("record RetryAfterRequestedSeconds = %d, want %d", record.RetryAfterRequestedSeconds, want)
	}
}

// e2eMintedToken is the token the stub mint server returns. The stub
// satellite asserts it arrives through the stdio config channel; the test
// asserts the satellite's verdict comes back through the real runner.
const e2eMintedToken = "e2e-minted-token-9182"

// hostMintE2ESatellite is the stub satellite: it runs in a re-executed test
// binary, reads one Request from stdin, proves the host-minted token arrived
// as config.host_github_app_token {token, scope_id} with no key material on
// the wire, and returns a complete SDK result echoing the claim.
func hostMintE2ESatellite(t *testing.T) {
	t.Helper()

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatalf("ReadAll(stdin) error = %v, want nil", err)
	}
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("stdio request carries host key material")
	}
	var request Request
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("Unmarshal(stdin) error = %v, want nil", err)
	}
	injected, ok := request.Config[HostMintedGitHubTokenKey].(map[string]any)
	if !ok {
		t.Fatalf("config[%q] = %v, want object {token, scope_id}", HostMintedGitHubTokenKey, request.Config[HostMintedGitHubTokenKey])
	}
	if got := injected["token"]; got != e2eMintedToken {
		t.Fatalf("injected token = %v, want %q", got, e2eMintedToken)
	}
	if got := injected["scope_id"]; got != request.Claim.Scope.ID {
		t.Fatalf("injected scope_id = %v, want claim scope %q", got, request.Claim.Scope.ID)
	}
	result := sdkcollector.Result{
		ProtocolVersion: request.ProtocolVersion,
		State:           sdkcollector.ResultComplete,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:         request.Claim.GenerationID,
			ObservedAt: testObservedAt(),
		},
		Statuses: []sdkcollector.Status{{
			Class:     sdkcollector.StatusComplete,
			FactCount: 0,
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatalf("Encode(stdout) error = %v, want nil", err)
	}
}

// TestHostMintedTokenEndToEndThroughRealSatellite proves the full host wire:
// the production provider mints per claim, the Source injects the token into
// the stdio Request, a real subprocess satellite spends it, and the complete
// verdict flows back. No key material crosses the boundary in either
// direction.
func TestHostMintedTokenEndToEndThroughRealSatellite(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_HOST_MINT_E2E") == "satellite" {
		hostMintE2ESatellite(t)
		os.Exit(0)
	}
	t.Parallel()

	keyPEM := appProviderTestKey(t)
	mintServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/access_tokens") {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": e2eMintedToken})
	}))
	t.Cleanup(mintServer.Close)
	provider, err := NewAppTokenProvider(AppTokenProviderConfig{
		AppID:         "42",
		PrivateKey:    keyPEM,
		Installations: map[string]string{"repo-eshu": "777"},
		BaseURL:       mintServer.URL,
		HTTPClient:    mintServer.Client(),
	})
	if err != nil {
		t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
	}
	item := testWorkItem()
	// A real subprocess needs a live lease: the shared fixture's fixed
	// LeaseExpiresAt is in the past, which a fake runner ignores but
	// exec.CommandContext enforces.
	item.LeaseExpiresAt = time.Now().Add(2 * time.Minute)
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHostMintedTokenEndToEndThroughRealSatellite"},
		Env:     []string{"ESHU_EXTENSIONHOST_HOST_MINT_E2E=satellite"},
	}
	source := mustNewSourceWithProvider(t, runner, &recordingStatusRecorder{}, provider, nil)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true for a complete satellite verdict")
	}
	_ = collected
}
