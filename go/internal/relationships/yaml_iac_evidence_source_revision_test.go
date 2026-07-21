// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDiscoverArgoCDDocumentEvidenceCarriesSourceRevision is the #5441
// second-P0 regression guard, found while proving rc-156 against the live
// golden-corpus gate: a plain ArgoCD Application YAML manifest (the shape
// tests/fixtures/ecosystems/helm_argocd_platform/application.yaml and
// deployable-config/application.yaml both use, and the shape
// discoverArgoCDDocumentEvidence -- NOT discoverStructuredArgoCDEvidence --
// actually processes for a bare top-level Application object) declares
// spec.source.targetRevision, and the resulting EvidenceKindArgoCDAppSource
// fact must carry it as Details["source_revision"].
//
// Before this fix, discoverArgoCDDocumentEvidence's matchCatalog call passed
// nil for extraDetails, so the fact's Details never had a "source_revision"
// key at all -- the #5441 P0 fix (evidenceFactSourceRevision reading
// fact.Details["source_revision"] in aggregateCandidate) was correct but had
// no data to read for any evidence produced by this path, which is the
// dominant production path for a bare ArgoCD Application manifest (the
// separate discoverStructuredArgoCDEvidence path requires a parser to have
// already populated parsedFileData["argocd_applications"], which a bare
// top-level Application YAML file does not trigger in this corpus). The live
// golden-corpus gate caught this: rc-156_edge_prop_source_revision failed
// with "2/2 matching edges offending" even after the P0 fix landed, because
// both corpus edges came through this document-level path.
func TestDiscoverArgoCDDocumentEvidenceCarriesSourceRevision(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/payments.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: 'https://github.com/myorg/payments-service.git'
    targetRevision: v1.4.0
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindArgoCDAppSource {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindArgoCDAppSource)
	}
	if got, want := stringValue(evidence[0].Details["source_revision"]), "v1.4.0"; got != want {
		t.Fatalf("Details[source_revision] = %q, want %q; Details=%#v", got, want, evidence[0].Details)
	}
}

// TestDiscoverArgoCDDocumentEvidenceOmitsSourceRevisionWhenAbsent is the
// negative counterpart: an ArgoCD Application with no targetRevision must
// not fabricate a non-empty source_revision.
func TestDiscoverArgoCDDocumentEvidenceOmitsSourceRevisionWhenAbsent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/payments.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: 'https://github.com/myorg/payments-service.git'
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if got := stringValue(evidence[0].Details["source_revision"]); got != "" {
		t.Fatalf("Details[source_revision] = %q, want empty when targetRevision is absent", got)
	}
}

// TestDiscoverArgoCDDocumentEvidenceCarriesSourceRevisionFromMultiSource
// covers the spec.sources[] (multi-source Application) shape: the first
// source carrying a non-empty targetRevision wins.
func TestDiscoverArgoCDDocumentEvidenceCarriesSourceRevisionFromMultiSource(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/payments.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  sources:
    - repoURL: 'https://github.com/myorg/payments-service.git'
      targetRevision: release-2.0
    - repoURL: 'https://github.com/myorg/config-repo.git'
      ref: values
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if got, want := stringValue(evidence[0].Details["source_revision"]), "release-2.0"; got != want {
		t.Fatalf("Details[source_revision] = %q, want %q", got, want)
	}
}
