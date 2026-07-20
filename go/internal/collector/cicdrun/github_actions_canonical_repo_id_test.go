// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestGitHubActionsFixtureEmitsCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/github_actions_success.json")
	observedAt := time.Date(2026, 5, 16, 3, 30, 0, 0, time.UTC)
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123456789:2",
		CollectorInstanceID: "fixture-gh-actions",
		FencingToken:        77,
		ObservedAt:          observedAt,
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/actions/runs/123456789",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)

	// Expected canonical id: NormalizeRemoteURL("https://github.com/eshu-hq/eshu") → SHA1[:8]
	expectedCanonicalID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/eshu-hq/eshu", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	if !strings.HasPrefix(expectedCanonicalID, "repository:r_") {
		t.Fatalf("expected canonical id prefix repository:r_, got %q", expectedCanonicalID)
	}

	// ci.run: repository_id must be canonical; provider_repository_id must be raw
	run := byKind[facts.CICDRunFactKind][0]
	assertPayload(t, run.Payload, "repository_id", expectedCanonicalID)
	assertPayload(t, run.Payload, "provider_repository_id", "github.com/eshu-hq/eshu")

	// ci.pipeline_definition: same treatment
	pipeline := byKind[facts.CICDPipelineDefinitionFactKind][0]
	assertPayload(t, pipeline.Payload, "repository_id", expectedCanonicalID)
	assertPayload(t, pipeline.Payload, "provider_repository_id", "github.com/eshu-hq/eshu")

	// correlation_anchors must include canonical repository_id
	anchors, ok := run.Payload["correlation_anchors"].([]string)
	if !ok {
		t.Fatalf("correlation_anchors = %T, want []string", run.Payload["correlation_anchors"])
	}
	foundRepoAnchor := false
	for _, anchor := range anchors {
		if anchor == expectedCanonicalID {
			foundRepoAnchor = true
			break
		}
	}
	if !foundRepoAnchor {
		t.Fatalf("correlation_anchors does not contain canonical repository_id %q: %#v", expectedCanonicalID, anchors)
	}
}

func TestGitHubActionsFixtureCanonicalRepositoryIDMatchesGitCollector(t *testing.T) {
	t.Parallel()

	// The git collector computes canonical IDs via repositoryidentity.CanonicalRepositoryID
	// from the normalized remote URL. Prove the cicdrun collector's canonical ID for the
	// same repo matches what the git collector would compute.
	raw := readFixture(t, "testdata/github_actions_success.json")
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123456789:2",
		CollectorInstanceID: "fixture-gh-actions",
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/actions/runs/123456789",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	run := byKind[facts.CICDRunFactKind][0]
	gotRepoID, ok := run.Payload["repository_id"].(string)
	if !ok {
		t.Fatalf("repository_id = %T, want string", run.Payload["repository_id"])
	}

	// SSH form of same remote must produce identical canonical ID
	sshCanonicalID, err := repositoryidentity.CanonicalRepositoryID("git@github.com:eshu-hq/eshu.git", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID(SSH) error = %v", err)
	}
	if gotRepoID != sshCanonicalID {
		t.Fatalf("ci.run repository_id %q != SSH canonical id %q", gotRepoID, sshCanonicalID)
	}
}

func TestGitHubActionsFixtureCanonicalIDHandlesGHESHost(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"run": {
			"id": 123,
			"run_attempt": 1,
			"repository": {
				"full_name": "eshu-hq/eshu",
				"html_url": "https://github.example.com/eshu-hq/eshu"
			},
			"head_sha": "0123456789abcdef0123456789abcdef01234567"
		}
	}`)
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://github.example.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123:1",
		CollectorInstanceID: "fixture-gh-actions",
		SourceURI:           "https://api.github.example.com/repos/eshu-hq/eshu/actions/runs/123",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	run := byKind[facts.CICDRunFactKind][0]

	// GHES host yields a different canonical id (different host in hash)
	expectedID, err := repositoryidentity.CanonicalRepositoryID("https://github.example.com/eshu-hq/eshu", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID(GHES) error = %v", err)
	}
	assertPayload(t, run.Payload, "repository_id", expectedID)
	// provider_repository_id preserves the raw host-based string
	assertPayload(t, run.Payload, "provider_repository_id", "github.example.com/eshu-hq/eshu")
}

// TestGitHubActionsFixtureCanonicalIDFallsBackWhenNoHTMLURL verifies that
// when the repository has no html_url, the canonical repository_id is
// derived from the host+FullName pattern (NOT from the per-run SourceURI
// verbatim, which would embed the run ID and mint a different id per run).
//
// tier: strengthened from the original false-green prefix check to exact-equality.
func TestGitHubActionsFixtureCanonicalIDFallsBackWhenNoHTMLURL(t *testing.T) {
	t.Parallel()

	// No html_url on repository; SourceURI is a per-run API URL.
	raw := []byte(`{
		"run": {
			"id": 123,
			"run_attempt": 1,
			"repository": {"full_name": "eshu-hq/eshu"},
			"head_sha": "0123456789abcdef0123456789abcdef01234567"
		}
	}`)
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123:1",
		CollectorInstanceID: "fixture-gh-actions",
		// Per-run API URL: must NOT be hashed verbatim (would embed run ID).
		SourceURI: "https://api.github.com/repos/eshu-hq/eshu/actions/runs/123",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	run := byKind[facts.CICDRunFactKind][0]

	// The canonical id must be derived from https://github.com/eshu-hq/eshu
	// (api.github.com mapped to github.com, run-id-bearing path discarded),
	// NOT from the verbatim SourceURI.
	expectedCanonicalID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/eshu-hq/eshu", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	gotRepoID, ok := run.Payload["repository_id"].(string)
	if !ok || gotRepoID != expectedCanonicalID {
		t.Fatalf("repository_id = %q, want exact canonical %q (must not hash per-run SourceURI)", gotRepoID, expectedCanonicalID)
	}
}

// TestGitHubActionsFixtureCanonicalIDStableAcrossRunURLs proves that two
// different per-run SourceURIs of the same repo produce the SAME canonical
// repository_id — the backbone join depends on this.
func TestGitHubActionsFixtureCanonicalIDStableAcrossRunURLs(t *testing.T) {
	t.Parallel()

	noHTMLURLFixture := []byte(`{
		"run": {
			"id": 100,
			"run_attempt": 1,
			"repository": {"full_name": "eshu-hq/eshu"},
			"head_sha": "0123456789abcdef0123456789abcdef01234567"
		}
	}`)

	ctx1 := FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "100:1",
		CollectorInstanceID: "fixture-gh-actions",
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/actions/runs/100",
	}
	ctx2 := FixtureContext{
		ScopeID:             ctx1.ScopeID,
		GenerationID:        "200:1",
		CollectorInstanceID: ctx1.CollectorInstanceID,
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/actions/runs/200",
	}

	run1, err := GitHubActionsFixtureEnvelopes(noHTMLURLFixture, ctx1)
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	run2, err := GitHubActionsFixtureEnvelopes(noHTMLURLFixture, ctx2)
	if err != nil {
		t.Fatalf("run2: %v", err)
	}

	id1, _ := envelopesByKind(run1)[facts.CICDRunFactKind][0].Payload["repository_id"].(string)
	id2, _ := envelopesByKind(run2)[facts.CICDRunFactKind][0].Payload["repository_id"].(string)

	if id1 != id2 || id1 == "" {
		t.Fatalf("run 100 id = %q, run 200 id = %q; want identical non-empty canonical id", id1, id2)
	}
}

// TestGitHubActionsFixtureCanonicalIDHandlesGHESAPIPath proves the GHES
// case: when SourceURI is https://ghes.example.com/api/v3/repos/org/repo/...,
// the canonical id must derive from https://ghes.example.com/org/repo (the
// GHES host itself, not api.ghes.example.com).
func TestGitHubActionsFixtureCanonicalIDHandlesGHESAPIPath(t *testing.T) {
	t.Parallel()

	// GHES with no html_url, SourceURI containing /api/v3 path
	raw := []byte(`{
		"run": {
			"id": 123,
			"run_attempt": 1,
			"repository": {"full_name": "eshu-hq/eshu"},
			"head_sha": "0123456789abcdef0123456789abcdef01234567"
		}
	}`)
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://ghes.example.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123:1",
		CollectorInstanceID: "fixture-gh-actions",
		SourceURI:           "https://ghes.example.com/api/v3/repos/eshu-hq/eshu/actions/runs/123",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	run := byKind[facts.CICDRunFactKind][0]

	// The canonical id must derive from https://ghes.example.com/eshu-hq/eshu
	expectedID, err := repositoryidentity.CanonicalRepositoryID("https://ghes.example.com/eshu-hq/eshu", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	assertPayload(t, run.Payload, "repository_id", expectedID)
}

// TestCanonicalGitHubHostMapsOnlyKnownAPIPatterns proves the host-mapping
// contract: api.github.com → github.com and api.<tenant>.ghe.com →
// <tenant>.ghe.com (GitHub Enterprise Cloud data residency). All other hosts
// — including legitimate non-GitHub api.* hosts — must pass through unchanged,
// or the git collector and CI collector will hash different strings and
// silently miss the join.
func TestCanonicalGitHubHostMapsOnlyKnownAPIPatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input  string
		want   string
		reason string
	}{
		{"api.github.com", "github.com", "public github.com"},
		{"API.GITHUB.COM", "github.com", "case-insensitive public github.com"},
		{"Api.GitHub.Com", "github.com", "mixed-case github.com"},
		{"api.acme.ghe.com", "acme.ghe.com", "GHEC data residency tenant"},
		{"API.Acme.GHE.COM", "acme.ghe.com", "mixed-case GHEC tenant — mapped host is lowercased for consistency"},
		{"api.corp.example.com", "api.corp.example.com", "non-GitHub host — must not strip api prefix"},
		{"ghes.example.com", "ghes.example.com", "GHES self-hosted — no api prefix to strip"},
		{"github.com", "github.com", "already canonical — no change"},
		{"api.github.com:8443", "github.com", "port-bearing — strip api, drop port"},
		{"api.acme.ghe.com:8443", "acme.ghe.com", "port-bearing GHEC — strip api prefix and port"},
		{"acme.ghe.com", "acme.ghe.com", "GHEC without api prefix — unchanged"},
		{"", "", "empty host — unchanged"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := canonicalGitHubHost(tc.input)
			if got != tc.want {
				t.Fatalf("canonicalGitHubHost(%q) = %q, want %q (%s)", tc.input, got, tc.want, tc.reason)
			}
		})
	}
}

// BenchmarkRepositoryID measures the canonical repositoryID path used at
// CI fact emission. One NormalizeRemoteURL + SHA1 per envelope.
func BenchmarkRepositoryID(b *testing.B) {
	repo := githubRepository{
		FullName: "eshu-hq/eshu",
		HTMLURL:  "https://github.com/eshu-hq/eshu",
	}
	ctx := FixtureContext{
		SourceURI: "https://api.github.com/repos/eshu-hq/eshu/actions/runs/123456789",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = repositoryID(repo, ctx)
	}
}

// BenchmarkGitHubActionsEnvelopesEndToEnd measures the full envelope-build
// path for a realistic success fixture to capture the one-off canonicalization
// cost per run.
func BenchmarkGitHubActionsEnvelopesEndToEnd(b *testing.B) {
	raw := []byte(`{
		"workflow": {"id": 314159, "name": "CI", "path": ".github/workflows/ci.yml", "state": "active", "trigger": "push"},
		"run": {
			"id": 123456789, "run_attempt": 2, "run_number": 42,
			"name": "CI", "event": "push", "status": "completed", "conclusion": "success",
			"head_branch": "main", "head_sha": "0123456789abcdef0123456789abcdef01234567",
			"run_started_at": "2026-05-16T03:00:00Z", "updated_at": "2026-05-16T03:20:00Z",
			"html_url": "https://github.com/eshu-hq/eshu/actions/runs/123456789",
			"repository": {"full_name": "eshu-hq/eshu", "html_url": "https://github.com/eshu-hq/eshu"},
			"actor": {"login": "linuxdynasty"}
		},
		"jobs": [{"id": 9001, "name": "build", "status": "completed", "conclusion": "success", "started_at": "2026-05-16T03:01:00Z", "completed_at": "2026-05-16T03:10:00Z", "labels": ["ubuntu-latest"], "steps": [{"name": "Run actions/checkout@v4", "number": 1, "status": "completed", "conclusion": "success", "started_at": "2026-05-16T03:01:01Z", "completed_at": "2026-05-16T03:01:04Z"}]}],
		"artifacts": [{"id": 7001, "name": "image-digest", "size_in_bytes": 128, "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "artifact_type": "container_image", "archive_download_url": "https://api.github.com/repos/eshu-hq/eshu/actions/artifacts/7001/zip?token=secret", "expired": false, "created_at": "2026-05-16T03:09:00Z", "expires_at": "2026-06-16T03:09:00Z", "workflow_run": {"id": 123456789, "head_sha": "0123456789abcdef0123456789abcdef01234567"}}],
		"triggers": [{"trigger_kind": "workflow_call", "source_run_id": "555", "source_provider": "github_actions"}]
	}`)
	ctx := FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123456789:2",
		CollectorInstanceID: "fixture-gh-actions",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		envelopes, err := GitHubActionsFixtureEnvelopes(raw, ctx)
		if err != nil {
			b.Fatalf("GitHubActionsFixtureEnvelopes error: %v", err)
		}
		_ = envelopes
	}
}

// TestRepositoryCanonicalURLRejectsHostlessHTMLURL proves finding 3: tier-1
// HTMLURL acceptance requires a real host, not just any parseable string.
func TestRepositoryCanonicalURLRejectsHostlessHTMLURL(t *testing.T) {
	t.Parallel()

	// "notaurl" parses without error in url.Parse but has no Host.
	// To test hostless rejection: HTMLURL has no host, FullName is empty.
	repoNoHost := githubRepository{HTMLURL: "notaurl"}
	ctx := FixtureContext{}

	url2 := repositoryCanonicalURL(repoNoHost, ctx)
	if url2 != "" {
		t.Fatalf("repositoryCanonicalURL(hostless HTMLURL, no FullName) = %q, want \"\"", url2)
	}

	// With a valid HTMLURL, it must still work.
	repo3 := githubRepository{
		FullName: "eshu-hq/eshu",
		HTMLURL:  "https://github.com/eshu-hq/eshu",
	}
	url3 := repositoryCanonicalURL(repo3, ctx)
	if url3 != "https://github.com/eshu-hq/eshu" {
		t.Fatalf("repositoryCanonicalURL(valid HTMLURL) = %q, want https://github.com/eshu-hq/eshu", url3)
	}

	// Verify the full path: hostless HTMLURL + empty FullName → empty repository_id.
	id := repositoryID(repoNoHost, ctx)
	if id != "" {
		t.Fatalf("repositoryID(hostless HTMLURL, no FullName) = %q, want \"\"", id)
	}
}
