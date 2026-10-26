// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// BenchmarkBoundedPATFetch measures the same complete one-run snapshot on
// both revisions, from FetchRuns entry through decoding all three responses.
func BenchmarkBoundedPATFetch(b *testing.B) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/jobs"):
			_, _ = fmt.Fprint(w, `{"total_count":1,"jobs":[{"id":2001,"name":"build"}]}`)
		case strings.HasSuffix(request.URL.Path, "/artifacts"):
			_, _ = fmt.Fprint(w, `{"total_count":1,"artifacts":[{"id":3001,"name":"image"}]}`)
		case request.URL.Path == "/repos/example/repository/actions/runs":
			_, _ = fmt.Fprint(w, `{"total_count":1,"workflow_runs":[{"id":1001,"workflow_id":42,"run_attempt":1,"head_sha":"0123456789012345678901234567890123456789"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	b.Cleanup(server.Close)
	client := GitHubClient{HTTPClient: server.Client()}
	target := TargetConfig{
		ScopeID: "ci:example/repository", Repository: "example/repository",
		Token: "fixture-token", AllowedRepositories: []string{"example/repository"},
		APIBaseURL: server.URL, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
	}
	// Warm the TLS connection outside the measured region.
	if _, err := client.FetchRuns(b.Context(), target); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		page, err := client.FetchRuns(b.Context(), target)
		if err != nil || len(page.Snapshots) != 1 || len(page.Snapshots[0].Jobs) != 1 || len(page.Snapshots[0].Artifacts) != 1 {
			b.Fatalf("bounded snapshot changed: %v", err)
		}
	}
}

// BenchmarkAppCredentialMint measures the added per-claim exchange separately
// from the unchanged bounded PAT provider fetch above.
func BenchmarkAppCredentialMint(b *testing.B) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/app/installations/123/access_tokens" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"token":"fixture-installation-token"}`)
	}))
	b.Cleanup(server.Close)
	target := TargetConfig{
		AuthMode: AuthModeGitHubApp, GitHubAppID: "456",
		GitHubAppInstallationID: "123", GitHubAppPrivateKey: string(keyPEM),
		APIBaseURL: server.URL, AppHTTPClient: server.Client(),
	}
	if _, err := resolveClaimCredential(b.Context(), target); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		resolved, err := resolveClaimCredential(b.Context(), target)
		if err != nil || resolved.Token != "fixture-installation-token" || target.Token != "" {
			b.Fatal("per-claim credential exchange failed or mutated configured credentials")
		}
	}
	b.StopTimer()
	if requests != b.N+1 {
		b.Fatalf("mint request count=%d, want %d", requests, b.N+1)
	}
}
