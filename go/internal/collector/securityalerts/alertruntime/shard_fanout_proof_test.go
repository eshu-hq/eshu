// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
)

// shardReplayServer is a synthetic, GitHub-shaped Dependabot server generated
// in code (a 250-repository cassette would be unreadable). Every org and
// repository page carries a cursor Link to a next page, so every read the real
// GitHubDependabotClient makes spends exactly max_pages requests: the worst
// case the #9019 request formula bounds.
type shardReplayServer struct {
	mu        sync.Mutex
	orgReads  int
	repoReads map[string]int
	requests  int
}

func (s *shardReplayServer) handler(t *testing.T, base func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests++
		cursor := s.requests
		s.mu.Unlock()
		var alert map[string]any
		switch {
		case r.URL.Path == "/orgs/example-org/dependabot/alerts":
			if r.URL.Query().Get("after") == "" {
				s.mu.Lock()
				s.orgReads++
				s.mu.Unlock()
			}
			alert = replayAlert(1, "example-org/not-allowlisted")
		case strings.HasPrefix(r.URL.Path, "/repos/example-org/") && strings.HasSuffix(r.URL.Path, "/dependabot/alerts"):
			repository := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/dependabot/alerts")
			if r.URL.Query().Get("after") == "" {
				s.mu.Lock()
				s.repoReads[repository]++
				s.mu.Unlock()
			}
			alert = replayAlert(7, "")
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		next := fmt.Sprintf("<%s%s?after=c%d&per_page=100&state=open>; rel=\"next\"", base(), r.URL.Path, cursor)
		w.Header().Set("Link", next)
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4000")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{alert})
	})
}

func replayAlert(number int, repository string) map[string]any {
	alert := map[string]any{
		"number": number, "state": "open",
		"dependency":        map[string]any{"package": map[string]any{"ecosystem": "npm", "name": "left-pad"}, "manifest_path": "package-lock.json"},
		"security_advisory": map[string]any{"ghsa_id": "GHSA-0007-abcd-efgh", "severity": "high"},
		"security_vulnerability": map[string]any{
			"package": map[string]any{"ecosystem": "npm", "name": "left-pad"}, "vulnerable_version_range": "< 1.0.1",
		},
		"created_at": "2026-10-20T10:00:00Z", "updated_at": "2026-10-21T10:00:00Z",
	}
	if repository != "" {
		owner, name, _ := strings.Cut(repository, "/")
		alert["repository"] = map[string]any{"full_name": repository, "name": name, "owner": map[string]any{"login": owner}}
	}
	return alert
}

// TestShardedOrgTargetReadsEveryRepositoryOncePerScan is the #9019 fan-out
// proof over HTTP with the real provider client: one scan interval of a
// 250-repository org target (one claim per shard) reads every allowlisted
// repository exactly once, and sends exactly max_pages x (K + N) provider
// requests, K shard org reads plus N repository reads, where one unsharded
// claim used to read at most 100 repositories.
func TestShardedOrgTargetReadsEveryRepositoryOncePerScan(t *testing.T) {
	t.Parallel()

	for _, maxPages := range []int{1, 2} {
		t.Run(fmt.Sprintf("max_pages=%d", maxPages), func(t *testing.T) {
			t.Parallel()
			allowed := make([]string, 0, 251)
			for i := range 250 {
				allowed = append(allowed, fmt.Sprintf("example-org/svc-%03d", i))
			}
			allowed = append(allowed, "other-org/never-read")
			replay := &shardReplayServer{repoReads: map[string]int{}}
			var server *httptest.Server
			server = httptest.NewServer(replay.handler(t, func() string { return server.URL }))
			t.Cleanup(server.Close)
			source, err := NewClaimedSource(SourceConfig{
				CollectorInstanceID: "security-alert-primary",
				Targets: []TargetConfig{{
					Provider: ProviderGitHubDependabot, Scope: TargetScopeOrganization, ScopeID: fanoutTestOrgScopeID,
					Organization: "example-org", Token: "github-token", APIBaseURL: server.URL, MaxPages: maxPages,
					AllowedRepositories: allowed,
				}},
				Now: func() time.Time { return time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatalf("NewClaimedSource() error = %v", err)
			}
			leaves := shard.Partition(fanoutTestOrgScopeID, "example-org", allowed)
			factRepositories := map[string]int{}
			for _, leaf := range leaves {
				collected, ok, err := source.NextClaimed(context.Background(), shardWorkItem(leaf.ScopeID, leaf.ScopeID))
				if err != nil || !ok {
					t.Fatalf("claim %s: ok=%v err=%v", leaf.ScopeID, ok, err)
				}
				for _, env := range drainFacts(collected.Facts) {
					factRepositories[payloadString(env.Payload, "repository_id")]++
				}
			}
			k, n := len(leaves), 250
			if replay.orgReads != k {
				t.Fatalf("org reads = %d, want one per shard (%d)", replay.orgReads, k)
			}
			if len(replay.repoReads) != n {
				t.Fatalf("repositories read = %d, want all %d", len(replay.repoReads), n)
			}
			for repository, reads := range replay.repoReads {
				if reads != 1 {
					t.Fatalf("repository %s read %d times in one scan, want exactly once", repository, reads)
				}
			}
			if want := maxPages * (k + n); replay.requests != want {
				t.Fatalf("provider requests = %d, want max_pages x (K + N) = %d x (%d + %d) = %d", replay.requests, maxPages, k, n, want)
			}
			if len(factRepositories) != n {
				t.Fatalf("facts cover %d repositories, want %d", len(factRepositories), n)
			}
			t.Logf("PERF #9019 fan-out: N=%d K=%d max_pages=%d requests=%d (formula %d); unsharded claim read at most 100 repositories",
				n, k, maxPages, replay.requests, maxPages*(k+n))
		})
	}
}
