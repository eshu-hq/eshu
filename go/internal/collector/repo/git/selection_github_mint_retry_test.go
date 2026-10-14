// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
)

func mintTestPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// mintTestServer answers the installation token endpoint with statuses in
// order, then repeats the last one. A 200 carries a token.
func mintTestServer(t *testing.T, statuses []int, headers map[string]string) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/777/access_tokens" {
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusTeapot)
			return
		}
		mu.Lock()
		index := calls
		calls++
		mu.Unlock()
		if index >= len(statuses) {
			index = len(statuses) - 1
		}
		status := statuses[index]
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "minted-token"})
			return
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		http.Error(w, "{}", status)
	}))
	t.Cleanup(server.Close)
	return server, func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func mintTestConfig(t *testing.T, server *httptest.Server, rec *listRetryRecorder) RepoSyncConfig {
	t.Helper()
	return RepoSyncConfig{
		GitAuthMethod:         "githubApp",
		GitHubAppID:           "42",
		GitHubAppInstallation: "777",
		GitHubAppPrivateKey:   mintTestPrivateKeyPEM(t),
		GitHubAPI:             rest.Settings{BaseURL: server.URL, Retry: rec.config(4)},
	}
}

// TestMintGitHubAppTokenRetriesTransientFaults pins that the App token mint
// has the listing's retry budget and classes (#8084): a 502 then a 200
// recovers, and the wait schedule is the listing's.
func TestMintGitHubAppTokenRetriesTransientFaults(t *testing.T) {
	t.Parallel()

	server, calls := mintTestServer(t, []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK}, nil)
	rec := newListRetryRecorder()
	token, err := mintGitHubAppToken(context.Background(), mintTestConfig(t, server, rec))
	if err != nil || token != "minted-token" {
		t.Fatalf("mint = %q, %v; want minted-token, nil", token, err)
	}
	if got := calls(); got != 3 {
		t.Fatalf("mint calls = %d, want 3", got)
	}
	if got := rec.recorded(); len(got) != 2 || got[0] != time.Second || got[1] != 2*time.Second {
		t.Fatalf("waits = %v, want [1s 2s]", got)
	}
}

// TestMintGitHubAppTokenFinalStatusesDoNotRetry pins that 401, 403 and 404
// from the mint are final: one call, no wait, not retryable.
func TestMintGitHubAppTokenFinalStatusesDoNotRetry(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		server, calls := mintTestServer(t, []int{status}, nil)
		rec := newListRetryRecorder()
		_, err := mintGitHubAppToken(context.Background(), mintTestConfig(t, server, rec))
		if err == nil || collector.IsRetryable(err) {
			t.Fatalf("status %d: error = %v, want a final error", status, err)
		}
		if got := calls(); got != 1 {
			t.Fatalf("status %d: mint calls = %d, want 1", status, got)
		}
		if got := rec.recorded(); len(got) != 0 {
			t.Fatalf("status %d: waits = %v, want none", status, got)
		}
	}
}

// TestMintGitHubAppTokenExhaustionUsesTheListingErrorType pins one error
// type for both calls, so the selector counts them the same way.
func TestMintGitHubAppTokenExhaustionUsesTheListingErrorType(t *testing.T) {
	t.Parallel()

	server, calls := mintTestServer(t, []int{http.StatusBadGateway}, nil)
	rec := newListRetryRecorder()
	_, err := mintGitHubAppToken(context.Background(), mintTestConfig(t, server, rec))
	typed := listRetryAsDiscovery(t, err)
	if typed.Operation != "mint" || typed.Class != "server_error" || typed.Attempts != 4 {
		t.Fatalf("typed = %+v, want mint server_error 4 attempts", typed)
	}
	if got := calls(); got != 4 {
		t.Fatalf("mint calls = %d, want 4", got)
	}
}

// TestMintGitHubAppTokenRateLimitHintsFollowTheListingRules pins that the
// mint reuses the listing's rate-limit rules: a short hint is slept, a long
// one is handed to the cycle.
func TestMintGitHubAppTokenRateLimitHintsFollowTheListingRules(t *testing.T) {
	t.Parallel()

	short, _ := mintTestServer(t, []int{http.StatusTooManyRequests, http.StatusOK}, map[string]string{"Retry-After": "2"})
	rec := newListRetryRecorder()
	if token, err := mintGitHubAppToken(context.Background(), mintTestConfig(t, short, rec)); err != nil || token == "" {
		t.Fatalf("mint = %q, %v; want recovery", token, err)
	}
	if got := rec.recorded(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("waits = %v, want [2s]", got)
	}

	long, calls := mintTestServer(t, []int{http.StatusForbidden}, map[string]string{"Retry-After": "3600"})
	rec = newListRetryRecorder()
	_, err := mintGitHubAppToken(context.Background(), mintTestConfig(t, long, rec))
	typed := listRetryAsDiscovery(t, err)
	if typed.RetryAfter != time.Hour || calls() != 1 || len(rec.recorded()) != 0 {
		t.Fatalf("typed %+v calls %d waits %v, want 1h, one call, no wait", typed, calls(), rec.recorded())
	}
}
