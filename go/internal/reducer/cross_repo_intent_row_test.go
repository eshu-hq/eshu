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

// The four tests below drive the REAL resolution pipeline
// (relationships.Resolve -> buildResolvedEdgeIntentRows), constructing
// []relationships.EvidenceFact the way the discovery layer actually shapes
// them, rather than hand-building a ResolvedRelationship literal directly.
//
// This replaces an earlier version of these tests that asserted against a
// hand-built `ResolvedRelationship{Details: {"source_revision": "main"}}`
// literal. That shape does not exist in production:
// relationships.aggregateCandidate (internal/relationships/resolver.go) is
// the sole builder of ResolvedRelationship.Details for every inferred
// relationship, and it sets only "evidence_kinds"/"evidence_preview" —
// never source_revision, destination_namespace, or source_ref directly.
// The hand-built literal validated a code path the real pipeline never
// exercises, which hid a P0: buildResolvedEdgeIntentRow read
// r.Details["source_revision"] and always got "". The fix moved
// source_revision/destination_namespace/first_party_ref_version onto typed
// Candidate/ResolvedRelationship fields populated directly from
// []EvidenceFact in aggregateCandidate, so the compiler — not a
// hand-built test fixture — enforces that this data flows through the real
// pipeline.

// TestBuildResolvedEdgeIntentRowsFromRealPipelineCarriesSourceRevision
// proves the fix end to end: an ArgoCD Application source EvidenceFact
// (matching the shape internal/relationships/structured_family_evidence.go
// discoverStructuredArgoCDEvidence actually produces) carries its declared
// targetRevision through Resolve() -> buildResolvedEdgeIntentRows() into the
// intent payload's source_revision key.
func TestBuildResolvedEdgeIntentRowsFromRealPipelineCarriesSourceRevision(t *testing.T) {
	t.Parallel()

	facts := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindArgoCDAppSource,
			RelationshipType: relationships.RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			Confidence:       0.9,
			Rationale:        "ArgoCD Application source references the target repository",
			Details: map[string]any{
				"argocd_application_name":    "checkout-service",
				"source_revision":            "main",
				"first_party_ref_kind":       "argocd_application_source",
				"first_party_ref_name":       "checkout-service",
				"first_party_ref_version":    "main",
				"first_party_ref_normalized": "https://github.com/example/service-api",
			},
		},
	}

	_, resolved := relationships.Resolve(facts, nil, relationships.DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["source_revision"]), "main"; got != want {
		t.Fatalf("Payload[source_revision] = %q, want %q", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowsFromRealPipelineCarriesFirstPartyRefVersionFromGitHubActions
// proves P1 finding 2: a GitHub Actions reusable-workflow EvidenceFact
// (matching internal/relationships/github_actions_evidence.go) sets
// Details["first_party_ref_version"] DIRECTLY off the `@v1.2.3` pin on a
// `uses:` reference — it never sets source_ref, so a fix that only reads
// source_ref would still lose this pin. RelDeploysFrom is one of the five
// widened edges.
func TestBuildResolvedEdgeIntentRowsFromRealPipelineCarriesFirstPartyRefVersionFromGitHubActions(t *testing.T) {
	t.Parallel()

	facts := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindGitHubActionsReusableWorkflow,
			RelationshipType: relationships.RelDeploysFrom,
			SourceRepoID:     "repo-app",
			TargetRepoID:     "repo-workflows",
			Confidence:       0.9,
			Rationale:        "GitHub Actions reusable workflow references deployment logic in the target repository",
			Details: map[string]any{
				"workflow_ref":               "org/repo-workflows/.github/workflows/deploy.yml@v1.2.3",
				"workflow_repo":              "org/repo-workflows",
				"workflow_path":              ".github/workflows/deploy.yml",
				"workflow_ref_name":          "v1.2.3",
				"first_party_ref_kind":       "github_actions_reusable_workflow",
				"first_party_ref_path":       ".github/workflows/deploy.yml",
				"first_party_ref_version":    "v1.2.3",
				"first_party_ref_normalized": "org/repo-workflows/.github/workflows/deploy.yml",
			},
		},
	}

	_, resolved := relationships.Resolve(facts, nil, relationships.DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["first_party_ref_version"]), "v1.2.3"; got != want {
		t.Fatalf("Payload[first_party_ref_version] = %q, want %q", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowsFromRealPipelineExtractsFirstPartyRefVersionFromTerraformSourceRef
// proves the Terraform-ref-derivation fallback: a Terraform module-source
// EvidenceFact (matching internal/relationships/terraform_evidence.go) sets
// only Details["source_ref"] (the raw pinned "source = " value), no
// first_party_ref_version key, so the pin must be derived via
// relationships.ExtractTerraformRefPin. RelUsesModule is one of the five
// widened edges.
func TestBuildResolvedEdgeIntentRowsFromRealPipelineExtractsFirstPartyRefVersionFromTerraformSourceRef(t *testing.T) {
	t.Parallel()

	facts := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformModuleSource,
			RelationshipType: relationships.RelUsesModule,
			SourceRepoID:     "repo-app",
			TargetRepoID:     "repo-module",
			Confidence:       0.9,
			Rationale:        "Terraform or Terragrunt module source points at the target module repository",
			Details: map[string]any{
				"module_name":                "vpc",
				"source_ref":                 "git::https://example.test/org/mod.git?ref=v2.0.0",
				"first_party_ref_normalized": "https://example.test/org/mod.git",
			},
		},
	}

	_, resolved := relationships.Resolve(facts, nil, relationships.DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["first_party_ref_version"]), "v2.0.0"; got != want {
		t.Fatalf("Payload[first_party_ref_version] = %q, want %q", got, want)
	}
}

// TestBuildResolvedEdgeIntentRowsFromRealPipelineOmitsAbsentEdgeDetailFields
// is the negative counterpart: a real GitHub Actions checkout-repository
// EvidenceFact (matching internal/relationships/github_actions_evidence.go,
// discoverGitHubActionsEvidence's third loop) carries no source_revision,
// no source_ref, and no first_party_ref_version — the pipeline must not
// fabricate a non-empty value for any of the three new payload keys.
func TestBuildResolvedEdgeIntentRowsFromRealPipelineOmitsAbsentEdgeDetailFields(t *testing.T) {
	t.Parallel()

	facts := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindGitHubActionsCheckoutRepository,
			RelationshipType: relationships.RelDiscoversConfigIn,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			Confidence:       0.9,
			Rationale:        "GitHub Actions explicitly checks out config or automation from the target repository",
			Details: map[string]any{
				"checkout_repository": "example/repo-service",
			},
		},
	}

	_, resolved := relationships.Resolve(facts, nil, relationships.DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
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

// TestBuildResolvedEdgeIntentRowsFromRealPipelineDestinationNamespaceMechanism
// proves the destination_namespace plumbing mechanism works when a fact of
// one of the five widened relationship types carries it directly — but see
// the doc comment on evidenceFactDestinationNamespace
// (internal/relationships/evidence_edge_fields.go) for an important caveat:
// as of #5441, NO real evidence producer attaches destination_namespace to
// any RelDeploysFrom/RelDiscoversConfigIn/RelProvisionsDependencyFor/
// RelUsesModule/RelReadsConfigFrom fact. The only producer
// (yaml_iac_evidence.go's appendDestinationPlatformEvidence) attaches it to
// a RelRunsOn fact targeting a Platform entity, which is a different
// Candidate bucket than any of the five edges and is never joined into one
// by this fix. This test therefore intentionally uses a synthetic
// RelDeploysFrom fact to prove the mechanism, not a real-shaped ArgoCD
// destination-platform fact — real ArgoCD corpora will NOT populate
// destination_namespace on a DEPLOYS_FROM edge until a producer or a
// cross-candidate join is added (tracked as follow-up, reported separately
// from this fix).
func TestBuildResolvedEdgeIntentRowsFromRealPipelineDestinationNamespaceMechanism(t *testing.T) {
	t.Parallel()

	facts := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindArgoCDAppSource,
			RelationshipType: relationships.RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			Confidence:       0.9,
			Rationale:        "synthetic: proves the destination_namespace mechanism only",
			Details: map[string]any{
				"destination_namespace": "prod",
			},
		},
	}

	_, resolved := relationships.Resolve(facts, nil, relationships.DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1", time.Now().UTC())
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := stringValue(rows[0].Payload["destination_namespace"]), "prod"; got != want {
		t.Fatalf("Payload[destination_namespace] = %q, want %q", got, want)
	}
}
