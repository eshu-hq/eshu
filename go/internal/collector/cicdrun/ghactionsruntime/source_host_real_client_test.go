// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestHostTokenRealClientClaim proves that the per-claim bearer can cross the
// real client's validation boundary and reach all bounded provider reads.
func TestHostTokenRealClientClaim(t *testing.T) {
	t.Parallel()
	for _, deploymentStatus := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(deploymentStatus), func(t *testing.T) {
			t.Parallel()
			tokenPath := filepath.Join(t.TempDir(), "host-token.json")
			atomicRewriteHostTokenFile(t, tokenPath, "host-token-a", hostTokenTestScopeID)
			var mu sync.Mutex
			var seen []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				mu.Lock()
				seen = append(seen, auth+" "+r.URL.Path)
				mu.Unlock()
				if auth != "Bearer host-token-a" && auth != "Bearer host-token-b" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case "/repos/example/repo/actions/runs":
					_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1001,"workflow_id":42,"run_attempt":1,"run_number":88,"name":"Publish","event":"push","status":"completed","conclusion":"success","head_branch":"main","head_sha":"0123456789abcdef0123456789abcdef01234567","run_started_at":"2026-06-07T14:59:00Z","updated_at":"2026-06-07T15:00:00Z","html_url":"https://github.com/example/repo/actions/runs/1001","repository":{"full_name":"example/repo","html_url":"https://github.com/example/repo"}}]}`))
				case "/repos/example/repo/actions/runs/1001/jobs":
					_, _ = w.Write([]byte(`{"total_count":1,"jobs":[{"id":2001,"name":"build","status":"completed","conclusion":"success"}]}`))
				case "/repos/example/repo/actions/runs/1001/artifacts":
					_, _ = w.Write([]byte(`{"total_count":1,"artifacts":[{"id":3001,"name":"image","size_in_bytes":10,"archive_download_url":"https://github.com/example/repo/artifacts/3001?token=secret"}]}`))
				case "/repos/example/repo/deployments":
					if deploymentStatus != http.StatusOK {
						w.WriteHeader(deploymentStatus)
					} else {
						_, _ = w.Write([]byte(`[]`))
					}
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			target := hostTokenTestTarget(tokenPath)
			target.APIBaseURL = server.URL
			source, err := NewClaimedSource(SourceConfig{CollectorInstanceID: "ci-cd-primary", Client: GitHubClient{HTTPClient: server.Client()}, Targets: []TargetConfig{target}})
			if err != nil {
				t.Fatalf("NewClaimedSource: %v", err)
			}
			for index, token := range []string{"host-token-a", "host-token-b"} {
				if index == 1 {
					atomicRewriteHostTokenFile(t, tokenPath, token, hostTokenTestScopeID)
				}
				collected, ok, err := source.NextClaimed(t.Context(), hostTokenTestWorkItem("generation-"+token))
				if err != nil || !ok {
					t.Fatalf("NextClaimed(%d) = ok=%v err=%v, want facts", index, ok, err)
				}
				envelopes := drainFacts(t, collected.Facts)
				for _, kind := range []string{facts.CICDRunFactKind, facts.CICDJobFactKind, facts.CICDArtifactFactKind} {
					requireFactKind(t, envelopes, kind)
				}
				if deploymentStatus != http.StatusOK {
					found := false
					for _, warning := range filterFactKind(envelopes, facts.CICDWarningFactKind) {
						found = found || warning.Payload["reason"] == "deployment_permission_denied"
					}
					if !found {
						t.Fatal("missing optional deployment permission warning")
					}
				}
			}
			mu.Lock()
			requests := append([]string(nil), seen...)
			mu.Unlock()
			if len(requests) != 8 {
				t.Fatalf("provider requests = %d, want four for each claim", len(requests))
			}
			for index, request := range requests {
				want := "Bearer host-token-a"
				if index >= 4 {
					want = "Bearer host-token-b"
				}
				if !strings.HasPrefix(request, want+" ") {
					t.Fatalf("request %d authorization = %q, want %s", index, request, want)
				}
			}
			configured := source.targets[hostTokenTestScopeID]
			if configured.Token != "" || configured.AuthMode != AuthModeHostMinted || configured.HostTokenFile != tokenPath {
				t.Fatal("long-lived source cached the claim bearer")
			}
		})
	}
}

// TestHostTokenRealClient401IsTerminal verifies the original host mode is
// retained for failure classification after a claim-local bearer resolves.
func TestHostTokenRealClient401IsTerminal(t *testing.T) {
	t.Parallel()
	tokenPath := writeHostTokenFile(t, "refused-host-token", hostTokenTestScopeID)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	target := hostTokenTestTarget(tokenPath)
	target.APIBaseURL = server.URL
	source, err := NewClaimedSource(SourceConfig{CollectorInstanceID: "ci-cd-primary", Client: GitHubClient{HTTPClient: server.Client()}, Targets: []TargetConfig{target}})
	if err != nil {
		t.Fatalf("NewClaimedSource: %v", err)
	}
	_, _, err = source.NextClaimed(t.Context(), hostTokenTestWorkItem("generation-refused"))
	assertAuthDeniedTerminal(t, err, "refused-host-token")
}
