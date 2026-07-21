// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// discoverArgoCDMultiSourceIntentRows drives the REAL pipeline end to end
// for one ArgoCD Application YAML document: DiscoverEvidence (YAML ->
// EvidenceFact), relationships.Resolve (candidate aggregation ->
// ResolvedRelationship), then buildResolvedEdgeIntentRows (the #5441
// chokepoint). Returns rows keyed by target repo ID so a test can assert
// each DEPLOYS_FROM edge's own source_revision independently of row order.
func discoverArgoCDMultiSourceIntentRows(t *testing.T, content string, catalog []relationships.CatalogEntry) map[string]SharedProjectionIntentRow {
	t.Helper()

	envelopes := []facts.Envelope{{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"artifact_type": "argocd",
			"relative_path": "apps/payments.yaml",
			"content":       content,
		},
	}}

	evidence := relationships.DiscoverEvidence(envelopes, catalog)
	_, resolved := relationships.Resolve(evidence, nil, relationships.DefaultConfidenceThreshold)

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())

	byTarget := make(map[string]SharedProjectionIntentRow, len(rows))
	for _, row := range rows {
		targetRepoID := stringValue(row.Payload["target_repo_id"])
		byTarget[targetRepoID] = row
	}
	return byTarget
}

// TestBuildResolvedEdgeIntentRowsPerSourceRevisionForMultiSourceApplication
// is the #5441 review round 8, P1-b regression guard. Before this fix,
// discoverArgoCDDocumentEvidence (yaml_iac_evidence.go) computed ONE
// document-wide sourceRevisionDetails (the first non-empty targetRevision
// found anywhere in spec.sources[]) and stamped it onto every resulting
// DEPLOYS_FROM edge, regardless of which source produced that edge. A
// multi-source Application with two DIFFERENT repos declaring DIFFERENT
// targetRevisions would fabricate the first repo's revision onto the second
// repo's edge -- wrong deployment truth, the exact question #5441 exists to
// answer correctly. Each edge must carry its OWN source's revision.
func TestBuildResolvedEdgeIntentRowsPerSourceRevisionForMultiSourceApplication(t *testing.T) {
	t.Parallel()

	content := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  sources:
    - repoURL: 'https://github.com/myorg/service-a.git'
      targetRevision: v1.0.0
    - repoURL: 'https://github.com/myorg/service-b.git'
      targetRevision: v2.5.0
`
	catalog := []relationships.CatalogEntry{
		{RepoID: "repo-service-a", Aliases: []string{"service-a"}},
		{RepoID: "repo-service-b", Aliases: []string{"service-b"}},
	}

	rows := discoverArgoCDMultiSourceIntentRows(t, content, catalog)

	rowA, ok := rows["repo-service-a"]
	if !ok {
		t.Fatalf("no DEPLOYS_FROM edge to repo-service-a; rows = %#v", rows)
	}
	if got, want := stringValue(rowA.Payload["source_revision"]), "v1.0.0"; got != want {
		t.Fatalf("repo-service-a source_revision = %q, want %q (its own source's revision, not repo-service-b's)", got, want)
	}

	rowB, ok := rows["repo-service-b"]
	if !ok {
		t.Fatalf("no DEPLOYS_FROM edge to repo-service-b; rows = %#v", rows)
	}
	if got, want := stringValue(rowB.Payload["source_revision"]), "v2.5.0"; got != want {
		t.Fatalf("repo-service-b source_revision = %q, want %q (its own source's revision, not repo-service-a's)", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowsMultiSourceEmptyRevisionDoesNotInheritSibling
// covers the coordinator's second required case: one source has no
// targetRevision at all while its sibling does. The revision-less source's
// edge must stay empty, not borrow its sibling's value -- proving the fix
// pairs revision to source, not "first non-empty value found in the
// document" (the old, buggy behavior).
func TestBuildResolvedEdgeIntentRowsMultiSourceEmptyRevisionDoesNotInheritSibling(t *testing.T) {
	t.Parallel()

	content := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  sources:
    - repoURL: 'https://github.com/myorg/service-a.git'
      targetRevision: v1.0.0
    - repoURL: 'https://github.com/myorg/service-b.git'
`
	catalog := []relationships.CatalogEntry{
		{RepoID: "repo-service-a", Aliases: []string{"service-a"}},
		{RepoID: "repo-service-b", Aliases: []string{"service-b"}},
	}

	rows := discoverArgoCDMultiSourceIntentRows(t, content, catalog)

	rowA, ok := rows["repo-service-a"]
	if !ok {
		t.Fatalf("no DEPLOYS_FROM edge to repo-service-a; rows = %#v", rows)
	}
	if got, want := stringValue(rowA.Payload["source_revision"]), "v1.0.0"; got != want {
		t.Fatalf("repo-service-a source_revision = %q, want %q", got, want)
	}

	rowB, ok := rows["repo-service-b"]
	if !ok {
		t.Fatalf("no DEPLOYS_FROM edge to repo-service-b; rows = %#v", rows)
	}
	if got := stringValue(rowB.Payload["source_revision"]); got != "" {
		t.Fatalf("repo-service-b source_revision = %q, want empty (must not inherit repo-service-a's v1.0.0)", got)
	}
}

// TestBuildResolvedEdgeIntentRowsSingleSourceApplicationUnchanged proves the
// dominant, single-source spec.source shape is unaffected by the
// per-source restructuring: one source, one edge, its own revision.
func TestBuildResolvedEdgeIntentRowsSingleSourceApplicationUnchanged(t *testing.T) {
	t.Parallel()

	content := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: 'https://github.com/myorg/payments-service.git'
    targetRevision: v1.4.0
`
	catalog := []relationships.CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	rows := discoverArgoCDMultiSourceIntentRows(t, content, catalog)

	row, ok := rows["repo-payments"]
	if !ok {
		t.Fatalf("no DEPLOYS_FROM edge to repo-payments; rows = %#v", rows)
	}
	if got, want := stringValue(row.Payload["source_revision"]), "v1.4.0"; got != want {
		t.Fatalf("repo-payments source_revision = %q, want %q", got, want)
	}
}
