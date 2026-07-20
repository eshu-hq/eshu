// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestBuildResolvedEdgeIntentRowsCarriesEvidenceArtifactsForGraphStory and
// the tests below in this file exercise buildResolvedEdgeIntentRow directly
// (the #5441 chokepoint for widening resolved-relationship Details onto the
// shared projection intent payload), split out of
// cross_repo_resolution_test.go to keep both files under the repo's 500-line
// cap.

func TestBuildResolvedEdgeIntentRowsCarriesEvidenceArtifactsForGraphStory(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			EvidenceCount:    2,
			Rationale:        "deployment config references service repository",
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details: map[string]any{
				"evidence_kinds": []string{
					string(relationships.EvidenceKindHelmValues),
					string(relationships.EvidenceKindKustomizeResource),
				},
				"evidence_preview": []map[string]any{
					{
						"kind":       string(relationships.EvidenceKindHelmValues),
						"confidence": 0.84,
						"details": map[string]any{
							"path":          "argocd/service-api/overlays/prod/values.yaml",
							"extractor":     "helm",
							"matched_alias": "service-api",
							"matched_value": "registry.example.test/service-api",
						},
					},
				},
			},
		},
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	artifacts := mapSliceValueForTest(rows[0].Payload["evidence_artifacts"])
	if got, want := len(artifacts), 1; got != want {
		t.Fatalf("len(evidence_artifacts) = %d, want %d: %#v", got, want, rows[0].Payload)
	}
	artifact := artifacts[0]
	for key, want := range map[string]string{
		"evidence_kind":   string(relationships.EvidenceKindHelmValues),
		"artifact_family": "helm",
		"path":            "argocd/service-api/overlays/prod/values.yaml",
		"extractor":       "helm",
		"environment":     "prod",
		"matched_alias":   "service-api",
		"matched_value":   "registry.example.test/service-api",
	} {
		if got := stringValue(artifact[key]); got != want {
			t.Fatalf("artifact[%s] = %q, want %q; artifact=%#v", key, got, want, artifact)
		}
	}
	if got := floatValueForTest(artifact["confidence"]); got != 0.84 {
		t.Fatalf("artifact confidence = %v, want 0.84", got)
	}
}

// TestBuildResolvedEdgeIntentRowCarriesSourceRevision proves the #5441
// chokepoint widening: an ArgoCD-sourced DEPLOYS_FROM edge carries its
// declared targetRevision through Details["source_revision"] into the
// intent payload's source_revision key, so the edge can answer "which git
// revision is declared for this deployment" without a Postgres lookup.
func TestBuildResolvedEdgeIntentRowCarriesSourceRevision(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details: map[string]any{
				"source_revision": "main",
			},
		},
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["source_revision"]), "main"; got != want {
		t.Fatalf("Payload[source_revision] = %q, want %q", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowCarriesDestinationNamespace proves the
// #5441 chokepoint widening for the ArgoCD destination-namespace evidence
// key (yaml_iac_evidence.go), so DEPLOYS_FROM-family edges can answer
// "which namespace is env Y declared into" directly.
func TestBuildResolvedEdgeIntentRowCarriesDestinationNamespace(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details: map[string]any{
				"destination_namespace": "prod",
			},
		},
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["destination_namespace"]), "prod"; got != want {
		t.Fatalf("Payload[destination_namespace] = %q, want %q", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowExtractsFirstPartyRefVersion proves the
// #5441 chokepoint widening for the Terraform module pin: the raw pinned
// module source (Details["source_ref"], previously thrown away past the
// reducer boundary) is reduced to its ref= pin value and carried onto the
// intent payload as first_party_ref_version.
func TestBuildResolvedEdgeIntentRowExtractsFirstPartyRefVersion(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-app",
			TargetRepoID:     "repo-module",
			RelationshipType: relationships.RelUsesModule,
			Confidence:       0.9,
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details: map[string]any{
				"source_ref": "git::https://example.test/org/mod.git?ref=v1.2.3",
			},
		},
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["first_party_ref_version"]), "v1.2.3"; got != want {
		t.Fatalf("Payload[first_party_ref_version] = %q, want %q", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowOmitsAbsentEdgeDetailFields is the negative
// counterpart: a resolved relationship with no source_revision,
// destination_namespace, or source_ref in Details must not fabricate any of
// the three new payload keys with a non-empty value.
func TestBuildResolvedEdgeIntentRowOmitsAbsentEdgeDetailFields(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details:          map[string]any{},
		},
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	for _, key := range []string{"source_revision", "destination_namespace", "first_party_ref_version"} {
		if got := stringValue(rows[0].Payload[key]); got != "" {
			t.Fatalf("Payload[%s] = %q, want empty for absent Details", key, got)
		}
	}
}
