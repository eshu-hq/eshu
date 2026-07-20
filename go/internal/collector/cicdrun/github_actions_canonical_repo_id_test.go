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

func TestGitHubActionsFixtureCanonicalIDFallsBackWhenNoHTMLURL(t *testing.T) {
	t.Parallel()

	// No html_url on repository, but SourceURI provides the host for fallback.
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
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/actions/runs/123",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	run := byKind[facts.CICDRunFactKind][0]

	// repository_id must be canonical (not empty, not raw)
	gotRepoID, ok := run.Payload["repository_id"].(string)
	if !ok || gotRepoID == "" {
		t.Fatalf("repository_id = %q, want non-empty canonical", gotRepoID)
	}
	if !strings.HasPrefix(gotRepoID, "repository:r_") {
		t.Fatalf("repository_id %q does not start with repository:r_", gotRepoID)
	}
	// provider_repository_id preserves the raw host/fullName.
	// Since html_url is missing, repositoryHost falls back to SourceURI,
	// whose host is "api.github.com".
	providerID, ok := run.Payload["provider_repository_id"].(string)
	if !ok || providerID != "api.github.com/eshu-hq/eshu" {
		t.Fatalf("provider_repository_id = %q, want api.github.com/eshu-hq/eshu", providerID)
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
