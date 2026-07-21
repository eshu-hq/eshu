// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

// TestGraphDeploymentEvidenceReturnsGHARefFields proves the GRAPH path
// (queryRepoDeploymentEvidence -> copyOptionalDeploymentEvidenceFields) reads
// artifact.ref_value/artifact.ref_pinned off the EvidenceArtifact node and
// returns them on the artifact. This is the graph-side half of the
// both-paths-agree proof for issue #5372's ref_value/ref_pinned signal (the
// Postgres read-model half is
// TestDeploymentEvidenceArtifactFromPreviewGHARefPinned below).
func TestGraphDeploymentEvidenceReturnsGHARefFields(t *testing.T) {
	t.Parallel()

	reader := &stubCommitSHAGraphReader{
		rows: []map[string]any{
			{
				"direction":         "outgoing",
				"artifact_id":       "evidence-artifact:github_actions:1",
				"name":              "GITHUB_ACTIONS_ACTION_REPOSITORY:.github/workflows/deploy.yml",
				"domain":            "deployment",
				"path":              ".github/workflows/deploy.yml",
				"evidence_kind":     "GITHUB_ACTIONS_ACTION_REPOSITORY",
				"artifact_family":   "github_actions",
				"extractor":         "github_actions",
				"relationship_type": "DEPENDS_ON",
				"resolved_id":       "resolved-9",
				"generation_id":     "gen-3",
				"confidence":        float64(0.9),
				"ref_value":         "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				"ref_pinned":        true,
				"evidence_source":   "resolver/cross-repo",
				"source_repo_id":    "repo-service",
				"source_repo_name":  "svc",
				"target_repo_id":    "repo-action",
				"target_repo_name":  "octo-action",
			},
		},
	}

	result, err := queryRepoDeploymentEvidence(context.Background(), reader, nil, map[string]any{"repo_id": "repo-service"})
	if err != nil {
		t.Fatalf("queryRepoDeploymentEvidence() error = %v", err)
	}
	artifacts, _ := result["artifacts"].([]map[string]any)
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1; result = %#v", len(artifacts), result)
	}
	if got := artifacts[0]["ref_value"]; got != "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Errorf("artifact.ref_value = %#v, want the full SHA", got)
	}
	if got, ok := artifacts[0]["ref_pinned"].(bool); !ok || !got {
		t.Errorf("artifact.ref_pinned = %#v, want true", artifacts[0]["ref_pinned"])
	}
}

// TestGraphDeploymentEvidenceOmitsGHARefFieldsWhenAbsent proves the graph
// path never fabricates ref_pinned when the node carries no ref_value (a
// local ./ reusable workflow or a non-GitHub-Actions artifact).
func TestGraphDeploymentEvidenceOmitsGHARefFieldsWhenAbsent(t *testing.T) {
	t.Parallel()

	reader := &stubCommitSHAGraphReader{
		rows: []map[string]any{
			{
				"direction":         "outgoing",
				"artifact_id":       "evidence-artifact:kustomize:1",
				"name":              "KUSTOMIZE_RESOURCE:overlays/prod/kustomization.yaml",
				"domain":            "deployment",
				"path":              "overlays/prod/kustomization.yaml",
				"evidence_kind":     "KUSTOMIZE_RESOURCE",
				"artifact_family":   "kustomize",
				"extractor":         "kustomize",
				"relationship_type": "DEPLOYS_FROM",
				"resolved_id":       "resolved-3",
				"generation_id":     "gen-2",
				"confidence":        float64(0.78),
				"evidence_source":   "resolver/cross-repo",
				"source_repo_id":    "repo-platform",
				"source_repo_name":  "platform-k8s",
				"target_repo_id":    "repo-app",
				"target_repo_name":  "my-app",
				// No ref_value/ref_pinned on the node.
			},
		},
	}

	result, err := queryRepoDeploymentEvidence(context.Background(), reader, nil, map[string]any{"repo_id": "repo-platform"})
	if err != nil {
		t.Fatalf("queryRepoDeploymentEvidence() error = %v", err)
	}
	artifacts, _ := result["artifacts"].([]map[string]any)
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1; result = %#v", len(artifacts), result)
	}
	if v, ok := artifacts[0]["ref_value"]; ok {
		t.Errorf("artifact.ref_value = %#v, want absent (no fabrication)", v)
	}
	if v, ok := artifacts[0]["ref_pinned"]; ok {
		t.Errorf("artifact.ref_pinned = %#v, want absent (no fabrication)", v)
	}
}

// TestDeploymentEvidenceArtifactFromPreviewGHARefPinned proves the READ-MODEL
// path (deploymentEvidenceArtifactFromPreview) projects ref_value/ref_pinned
// from evidence preview details for a GitHub Actions action ref pinned to a
// full commit SHA.
func TestDeploymentEvidenceArtifactFromPreviewGHARefPinned(t *testing.T) {
	t.Parallel()

	preview := map[string]any{
		"kind":       "GITHUB_ACTIONS_ACTION_REPOSITORY",
		"confidence": float64(0.9),
		"details": map[string]any{
			"path":                    ".github/workflows/deploy.yml",
			"matched_alias":           "octo-action",
			"matched_value":           "octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			"extractor":               "github_actions",
			"action_ref_name":         "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			"first_party_ref_version": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		},
	}

	artifact := deploymentEvidenceArtifactFromPreview(
		preview, "outgoing", "resolved-9", "gen-3",
		"repo-service", "svc",
		"repo-action", "octo-action",
		"DEPENDS_ON", 0.9,
	)
	if artifact == nil {
		t.Fatal("deploymentEvidenceArtifactFromPreview() = nil, want non-nil")
	}
	if got := artifact["ref_value"]; got != "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Errorf("artifact.ref_value = %#v, want the full SHA", got)
	}
	if got, ok := artifact["ref_pinned"].(bool); !ok || !got {
		t.Errorf("artifact.ref_pinned = %#v, want true", artifact["ref_pinned"])
	}
}

// TestDeploymentEvidenceArtifactFromPreviewGHARefMutable proves a mutable
// branch/tag ref is projected with ref_pinned:false, never fabricated as
// safe.
func TestDeploymentEvidenceArtifactFromPreviewGHARefMutable(t *testing.T) {
	t.Parallel()

	preview := map[string]any{
		"kind":       "GITHUB_ACTIONS_REUSABLE_WORKFLOW",
		"confidence": float64(0.85),
		"details": map[string]any{
			"path":                    ".github/workflows/deploy.yml",
			"matched_alias":           "deployment-helm",
			"matched_value":           "myorg/deployment-helm/.github/workflows/deploy.yaml@main",
			"extractor":               "github_actions",
			"workflow_ref_name":       "main",
			"first_party_ref_version": "main",
		},
	}

	artifact := deploymentEvidenceArtifactFromPreview(
		preview, "outgoing", "resolved-10", "gen-3",
		"repo-service", "svc",
		"repo-deploy", "deployment-helm",
		"DEPLOYS_FROM", 0.85,
	)
	if artifact == nil {
		t.Fatal("deploymentEvidenceArtifactFromPreview() = nil, want non-nil")
	}
	if got := artifact["ref_value"]; got != "main" {
		t.Errorf("artifact.ref_value = %#v, want main", got)
	}
	if got, ok := artifact["ref_pinned"].(bool); !ok || got {
		t.Errorf("artifact.ref_pinned = %#v, want false (mutable ref must never be fabricated as pinned)", artifact["ref_pinned"])
	}
}

// TestDeploymentEvidenceArtifactFromPreviewGHARefAbsentWhenMissing proves a
// local ./ reusable-workflow evidence item (no @ref) omits both fields.
func TestDeploymentEvidenceArtifactFromPreviewGHARefAbsentWhenMissing(t *testing.T) {
	t.Parallel()

	preview := map[string]any{
		"kind":       "GITHUB_ACTIONS_LOCAL_REUSABLE_WORKFLOW",
		"confidence": float64(0.85),
		"details": map[string]any{
			"path":          ".github/workflows/deploy.yml",
			"matched_value": ".github/workflows/deploy.yml",
			"extractor":     "github_actions",
			// No ref field at all -- a local workflow has no @ref.
		},
	}

	artifact := deploymentEvidenceArtifactFromPreview(
		preview, "outgoing", "resolved-11", "gen-3",
		"repo-service", "svc",
		"repo-service", "svc",
		"DEPLOYS_FROM", 0.85,
	)
	if artifact == nil {
		t.Fatal("deploymentEvidenceArtifactFromPreview() = nil, want non-nil")
	}
	if v, ok := artifact["ref_value"]; ok {
		t.Errorf("artifact.ref_value = %#v, want absent (no fabrication)", v)
	}
	if v, ok := artifact["ref_pinned"]; ok {
		t.Errorf("artifact.ref_pinned = %#v, want absent (no fabrication)", v)
	}
}

// TestDeploymentEvidenceArtifactFromPreviewScopesGHARefToGitHubActions
// proves ref_value/ref_pinned are never projected for a non-GitHub-Actions
// evidence kind, even though first_party_ref_version is also populated by
// unrelated evidence families (Terraform module versions, and so on).
func TestDeploymentEvidenceArtifactFromPreviewScopesGHARefToGitHubActions(t *testing.T) {
	t.Parallel()

	preview := map[string]any{
		"kind":       "TERRAFORM_APP_REPO",
		"confidence": float64(0.99),
		"details": map[string]any{
			"path":                    "main.tf",
			"matched_value":           "payments-service",
			"matched_alias":           "payments-service",
			"extractor":               "terraform",
			"first_party_ref_version": "v2.0.0",
		},
	}

	artifact := deploymentEvidenceArtifactFromPreview(
		preview, "outgoing", "resolved-12", "gen-3",
		"repo-infra", "infra",
		"repo-payments", "payments-service",
		"USES_MODULE", 0.99,
	)
	if artifact == nil {
		t.Fatal("deploymentEvidenceArtifactFromPreview() = nil, want non-nil")
	}
	if v, ok := artifact["ref_value"]; ok {
		t.Errorf("artifact.ref_value = %#v, want absent for a non-GitHub-Actions evidence kind", v)
	}
	if v, ok := artifact["ref_pinned"]; ok {
		t.Errorf("artifact.ref_pinned = %#v, want absent for a non-GitHub-Actions evidence kind", v)
	}
}

// TestContentReaderDeploymentEvidenceHydratesGHARef proves the end-to-end
// read-model SQL -> scan -> artifact path (repositoryDeploymentEvidence)
// carries ref_value/ref_pinned, mirroring
// TestContentReaderDeploymentEvidenceHydratesCommitSHA's proof for commit_sha.
func TestContentReaderDeploymentEvidenceHydratesGHARef(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderDeploymentEvidenceColumns(),
			rows: [][]driver.Value{
				{
					"outgoing", "resolved-9", "gen-3", "repo-service", "svc",
					"", "", "repo-action", "octo-action", "", "", "DEPENDS_ON", float64(0.9),
					[]byte(`{"evidence_preview":[{"kind":"GITHUB_ACTIONS_ACTION_REPOSITORY","confidence":0.9,"details":{"path":".github/workflows/deploy.yml","extractor":"github_actions","matched_alias":"octo-action","matched_value":"octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","action_ref_name":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","first_party_ref_version":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"}}]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.repositoryDeploymentEvidence(t.Context(), "repo-service")
	if err != nil {
		t.Fatalf("repositoryDeploymentEvidence() error = %v", err)
	}
	if !got.Available || len(got.Rows) == 0 {
		t.Fatal("no rows returned")
	}
	row := got.Rows[0]
	if v := row["ref_value"]; v != "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Errorf("ref_value = %#v, want the full SHA", v)
	}
	if v, ok := row["ref_pinned"].(bool); !ok || !v {
		t.Errorf("ref_pinned = %#v, want true", row["ref_pinned"])
	}
}
