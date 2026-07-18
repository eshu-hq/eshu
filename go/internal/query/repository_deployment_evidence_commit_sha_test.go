// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestGraphDeploymentEvidenceReturnsCommitSHA(t *testing.T) {
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
				"start_line":        int64(3),
				"end_line":          int64(5),
				"commit_sha":        "deadbeef1234",
				"evidence_source":   "resolver/cross-repo",
				"source_repo_id":    "repo-platform",
				"source_repo_name":  "platform-k8s",
				"target_repo_id":    "repo-app",
				"target_repo_name":  "my-app",
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
	if got := artifacts[0]["commit_sha"]; got != "deadbeef1234" {
		t.Errorf("artifact.commit_sha = %#v, want deadbeef1234", got)
	}
}

// TestDeploymentEvidenceArtifactFromPreviewCommitSHA proves that the
// read-model path (deploymentEvidenceArtifactFromPreview) copies commit_sha
// from the preview details into the returned artifact map.
func TestDeploymentEvidenceArtifactFromPreviewCommitSHA(t *testing.T) {
	t.Parallel()

	preview := map[string]any{
		"kind":       "KUSTOMIZE_RESOURCE",
		"confidence": float64(0.78),
		"details": map[string]any{
			"path":          "overlays/prod/kustomization.yaml",
			"matched_alias": "my-app",
			"matched_value": "my-app",
			"extractor":     "kustomize",
			"commit_sha":    "deadbeef1234",
		},
	}

	artifact := deploymentEvidenceArtifactFromPreview(
		preview, "outgoing", "resolved-3", "gen-2",
		"repo-platform", "platform-k8s",
		"repo-app", "my-app",
		"DEPLOYS_FROM", 0.78,
	)
	if artifact == nil {
		t.Fatal("deploymentEvidenceArtifactFromPreview() = nil, want non-nil")
	}
	if got := artifact["commit_sha"]; got != "deadbeef1234" {
		t.Errorf("artifact.commit_sha = %#v, want deadbeef1234", got)
	}
}

// TestDeploymentEvidenceArtifactFromPreviewNoCommitSHADegradesSafely proves
// that when commit_sha is absent from details the field is simply omitted —
// no fabrication.
func TestDeploymentEvidenceArtifactFromPreviewNoCommitSHADegradesSafely(t *testing.T) {
	t.Parallel()

	preview := map[string]any{
		"kind":       "KUSTOMIZE_RESOURCE",
		"confidence": float64(0.78),
		"details": map[string]any{
			"path":      "overlays/prod/kustomization.yaml",
			"extractor": "kustomize",
			// no commit_sha
		},
	}

	artifact := deploymentEvidenceArtifactFromPreview(
		preview, "outgoing", "resolved-3", "gen-2",
		"repo-platform", "platform-k8s",
		"repo-app", "my-app",
		"DEPLOYS_FROM", 0.78,
	)
	if artifact == nil {
		t.Fatal("deploymentEvidenceArtifactFromPreview() = nil, want non-nil")
	}
	if got, ok := artifact["commit_sha"]; ok {
		t.Errorf("artifact.commit_sha = %#v, want absent (no fabrication)", got)
	}
}

// TestContentReaderDeploymentEvidenceHydratesCommitSHA proves the end-to-end
// read-model SQL → scan → artifact path carries commit_sha.
func TestContentReaderDeploymentEvidenceHydratesCommitSHA(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderDeploymentEvidenceColumns(),
			rows: [][]driver.Value{
				{
					"outgoing", "resolved-3", "gen-2", "repo-platform", "platform-k8s",
					"", "", "repo-app", "my-app", "", "", "DEPLOYS_FROM", float64(0.78),
					[]byte(`{"evidence_preview":[{"kind":"KUSTOMIZE_RESOURCE","confidence":0.78,"details":{"path":"overlays/prod/kustomization.yaml","extractor":"kustomize","matched_alias":"my-app","matched_value":"my-app","commit_sha":"deadbeef1234"}}]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.repositoryDeploymentEvidence(t.Context(), "repo-app")
	if err != nil {
		t.Fatalf("repositoryDeploymentEvidence() error = %v", err)
	}
	if !got.Available || len(got.Rows) == 0 {
		t.Fatal("no rows returned")
	}
	row := got.Rows[0]
	if sha := row["commit_sha"]; sha != "deadbeef1234" {
		t.Errorf("commit_sha = %#v, want deadbeef1234", sha)
	}
}

// stubCommitSHAGraphReader returns fixed rows only for the outgoing Cypher
// query fired by queryRepoDeploymentEvidence, identified by the
// HAS_DEPLOYMENT_EVIDENCE pattern; the incoming query returns nothing.
type stubCommitSHAGraphReader struct {
	rows []map[string]any
}

func (s *stubCommitSHAGraphReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	// Only respond to the outgoing query; the incoming query uses "source:Repository"
	// (no repo_id bind on the HAS_DEPLOYMENT_EVIDENCE match side).
	if strings.Contains(cypher, "source_rel:HAS_DEPLOYMENT_EVIDENCE") {
		return s.rows, nil
	}
	return nil, nil
}

func (s *stubCommitSHAGraphReader) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

type recordingDeploymentEvidenceGraphReader struct {
	cypherCalls []string
	params      []map[string]any
}

func (r *recordingDeploymentEvidenceGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.cypherCalls = append(r.cypherCalls, cypher)
	r.params = append(r.params, params)
	return nil, nil
}

func (r *recordingDeploymentEvidenceGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}
