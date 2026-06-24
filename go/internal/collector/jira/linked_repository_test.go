// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestExternalLinkPersistsCanonicalRepositoryIDForGitHubPullRequest(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		ID:          "30001",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "GitHub", Type: "com.github"},
		Object: LinkObject{
			URL:   "https://github.com/example/app/pull/42?access_token=secret",
			Title: "PR 42",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}

	wantID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/example/app", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v, want nil", err)
	}
	got, ok := env.Payload["linked_repository_id"].(string)
	if !ok || got == "" {
		t.Fatalf("linked_repository_id = %v, want canonical repo id", env.Payload["linked_repository_id"])
	}
	if got != wantID {
		t.Fatalf("linked_repository_id = %q, want %q", got, wantID)
	}
	assertNoRawURLLeak(t, env.Payload, "github.com/example/app", "access_token")
}

func TestExternalLinkPersistsCanonicalRepositoryIDForGitLabMergeRequest(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		ID:          "30002",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "GitLab", Type: "com.gitlab"},
		Object: LinkObject{
			URL:   "https://gitlab.com/group/subgroup/app/-/merge_requests/7?private_token=secret",
			Title: "MR 7",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}

	wantID, err := repositoryidentity.CanonicalRepositoryID("https://gitlab.com/group/subgroup/app", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v, want nil", err)
	}
	got, ok := env.Payload["linked_repository_id"].(string)
	if !ok || got != wantID {
		t.Fatalf("linked_repository_id = %v, want %q", env.Payload["linked_repository_id"], wantID)
	}
	assertNoRawURLLeak(t, env.Payload, "gitlab.com/group/subgroup/app", "private_token")
}

func TestExternalLinkOmitsRepositoryIDForUnsupportedProvider(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		ID:          "30003",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "Unknown Tracker", Type: "unknown"},
		Object: LinkObject{
			URL:   "https://private.example.invalid/deploy/123?token=secret",
			Title: "Private deploy",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	if _, present := env.Payload["linked_repository_id"]; present {
		t.Fatalf("linked_repository_id present = %v, want omitted for unsupported provider", env.Payload["linked_repository_id"])
	}
}

func TestExternalLinkOmitsRepositoryIDForUncanonicalizableGitHubURL(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	// GitHub-typed anchor but the path has no owner/repo before /pull/ — an
	// unexpected shape. Persist no guessed id.
	link := ExternalLink{
		ID:          "30004",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "GitHub", Type: "com.github"},
		Object: LinkObject{
			URL:   "https://github.com/app/pull/9",
			Title: "Malformed PR link",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	// Sanity: still classified as a PR anchor so this exercises the omit-on-
	// ambiguous-shape path, not the wrong-provider path.
	if got := env.Payload["correlation_anchor_class"]; got != "github_pull_request" {
		t.Fatalf("correlation_anchor_class = %v, want github_pull_request", got)
	}
	if _, present := env.Payload["linked_repository_id"]; present {
		t.Fatalf("linked_repository_id present = %v, want omitted for un-canonicalizable URL", env.Payload["linked_repository_id"])
	}
}

func TestExternalLinkOmitsRepositoryIDForAmbiguousGitLabRootProject(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	// GitLab MR anchor whose project path has no group prefix (a single path
	// segment before the MR marker). Conservatively omit rather than guess.
	link := ExternalLink{
		ID:          "30007",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "GitLab", Type: "com.gitlab"},
		Object: LinkObject{
			URL:   "https://gitlab.com/app/-/merge_requests/7",
			Title: "MR 7",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	if got := env.Payload["correlation_anchor_class"]; got != "gitlab_merge_request" {
		t.Fatalf("correlation_anchor_class = %v, want gitlab_merge_request", got)
	}
	if _, present := env.Payload["linked_repository_id"]; present {
		t.Fatalf("linked_repository_id present = %v, want omitted for ambiguous GitLab path", env.Payload["linked_repository_id"])
	}
}

func TestExternalLinkOmitsRepositoryIDWhenURLAbsent(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		ID:          "30005",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		GlobalID:    "github:pr:42",
		Application: LinkApplication{Name: "GitHub", Type: "com.github"},
		URLRedacted: true,
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	if _, present := env.Payload["linked_repository_id"]; present {
		t.Fatalf("linked_repository_id present = %v, want omitted when URL absent", env.Payload["linked_repository_id"])
	}
}

func TestExternalLinkKeepsURLRedactedWhenRepositoryResolved(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		ID:          "30006",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "GitHub", Type: "com.github"},
		Object: LinkObject{
			URL:   "https://github.com/example/app/pull/42?access_token=secret",
			Title: "PR 42",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}

	// Redaction contract preserved: raw URL dropped, fingerprint retained,
	// redaction flagged — even though we resolved a durable id from it.
	if got := env.Payload["url"]; got != "" {
		t.Fatalf("url = %q, want raw URL still redacted", got)
	}
	if got, _ := env.Payload["url_fingerprint"].(string); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("url_fingerprint = %v, want preserved sha256 fingerprint", env.Payload["url_fingerprint"])
	}
	if got := env.Payload["url_redacted"]; got != true {
		t.Fatalf("url_redacted = %v, want true", got)
	}
	if got := env.Payload["url_present"]; got != true {
		t.Fatalf("url_present = %v, want true", got)
	}
}

// assertNoRawURLLeak fails if any string payload value contains the raw repo
// path or a sensitive token marker, proving only the opaque canonical id and
// fingerprint survive — no raw URL, query params, or secrets.
func assertNoRawURLLeak(t *testing.T, payload map[string]any, rawPathMarker, secretMarker string) {
	t.Helper()
	for key, value := range payload {
		s, ok := value.(string)
		if !ok {
			continue
		}
		if strings.Contains(s, rawPathMarker) {
			t.Fatalf("payload[%q] = %q leaks raw repo URL path %q", key, s, rawPathMarker)
		}
		if strings.Contains(strings.ToLower(s), secretMarker) {
			t.Fatalf("payload[%q] = %q leaks secret marker %q", key, s, secretMarker)
		}
	}
}
