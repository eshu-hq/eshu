// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// ghaEvidenceArtifact builds a minimal ResolvedRelationship carrying one
// GitHub Actions evidence preview item so each test below can vary the
// preview details independently.
func ghaEvidenceArtifact(details map[string]any) relationships.ResolvedRelationship {
	return relationships.ResolvedRelationship{
		SourceRepoID: "repo-service",
		TargetRepoID: "repo-action",
		Details: map[string]any{
			"evidence_preview": []map[string]any{
				{
					"kind":       "GITHUB_ACTIONS_ACTION_REPOSITORY",
					"confidence": 0.9,
					"details":    details,
				},
			},
		},
	}
}

// TestResolvedRelationshipEvidenceArtifactsProjectsPinnedRefValue proves that
// a GitHub Actions action ref pinned to a full-length 40-hex commit SHA is
// projected as ref_value + ref_pinned:true onto the graph artifact.
//
// This is the TDD anchor for the reducer side of issue #5372. It MUST FAIL
// before ref_value/ref_pinned projection is added to
// resolvedRelationshipEvidenceArtifacts.
func TestResolvedRelationshipEvidenceArtifactsProjectsPinnedRefValue(t *testing.T) {
	t.Parallel()

	r := ghaEvidenceArtifact(map[string]any{
		"path":                    ".github/workflows/deploy.yml",
		"matched_value":           "octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		"matched_alias":           "octo-action",
		"extractor":               "github_actions",
		"action_repo":             "octo-org/octo-action",
		"action_ref_name":         "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		"first_party_ref_kind":    "github_actions_action",
		"first_party_ref_version": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
	})

	artifacts := resolvedRelationshipEvidenceArtifacts(r)
	if len(artifacts) != 1 {
		t.Fatalf("resolvedRelationshipEvidenceArtifacts() len = %d, want 1", len(artifacts))
	}
	art := artifacts[0]

	if got, _ := art["ref_value"].(string); got != "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Errorf("artifact[ref_value] = %v, want the full SHA", art["ref_value"])
	}
	if got, ok := art["ref_pinned"].(bool); !ok || !got {
		t.Errorf("artifact[ref_pinned] = %v (%T), want true", art["ref_pinned"], art["ref_pinned"])
	}
}

// TestResolvedRelationshipEvidenceArtifactsProjectsMutableRefValue proves
// that a GitHub Actions ref pinned to a mutable branch/tag (not a full commit
// SHA) is projected with ref_pinned:false -- never fabricated as safe.
func TestResolvedRelationshipEvidenceArtifactsProjectsMutableRefValue(t *testing.T) {
	t.Parallel()

	r := ghaEvidenceArtifact(map[string]any{
		"path":                    ".github/workflows/deploy.yml",
		"matched_value":           "octo-org/mutable-action@main",
		"matched_alias":           "mutable-action",
		"extractor":               "github_actions",
		"action_repo":             "octo-org/mutable-action",
		"action_ref_name":         "main",
		"first_party_ref_kind":    "github_actions_action",
		"first_party_ref_version": "main",
	})

	artifacts := resolvedRelationshipEvidenceArtifacts(r)
	if len(artifacts) != 1 {
		t.Fatalf("resolvedRelationshipEvidenceArtifacts() len = %d, want 1", len(artifacts))
	}
	art := artifacts[0]

	if got, _ := art["ref_value"].(string); got != "main" {
		t.Errorf("artifact[ref_value] = %v, want main", art["ref_value"])
	}
	if got, ok := art["ref_pinned"].(bool); !ok || got {
		t.Errorf("artifact[ref_pinned] = %v (%T), want false (mutable ref must never be fabricated as pinned)", art["ref_pinned"], art["ref_pinned"])
	}
}

// TestResolvedRelationshipEvidenceArtifactsOmitsRefFieldsWhenAbsent proves
// that a local `./` reusable-workflow evidence item (no @ref at all) omits
// both ref_value and ref_pinned entirely -- honest absence, not a fabricated
// ref_pinned:true for a workflow that runs at the calling commit.
func TestResolvedRelationshipEvidenceArtifactsOmitsRefFieldsWhenAbsent(t *testing.T) {
	t.Parallel()

	r := ghaEvidenceArtifact(map[string]any{
		"path":                       ".github/workflows/deploy.yml",
		"matched_value":              ".github/workflows/deploy.yml",
		"extractor":                  "github_actions",
		"local_workflow_path":        ".github/workflows/deploy.yml",
		"first_party_ref_kind":       "github_actions_local_workflow",
		"first_party_ref_path":       ".github/workflows/deploy.yml",
		"first_party_ref_normalized": ".github/workflows/deploy.yml",
		// No ref/version field at all -- a local workflow has no @ref.
	})

	artifacts := resolvedRelationshipEvidenceArtifacts(r)
	if len(artifacts) != 1 {
		t.Fatalf("resolvedRelationshipEvidenceArtifacts() len = %d, want 1", len(artifacts))
	}
	art := artifacts[0]

	if v, ok := art["ref_value"]; ok {
		t.Errorf("artifact[ref_value] = %v, want absent (honest absence, no fabrication)", v)
	}
	if v, ok := art["ref_pinned"]; ok {
		t.Errorf("artifact[ref_pinned] = %v, want absent (must not fabricate ref_pinned for a workflow with no ref)", v)
	}
}

// TestResolvedRelationshipEvidenceArtifactsScopesRefFieldsToGitHubActions
// proves that ref_value/ref_pinned are projected ONLY for GITHUB_ACTIONS_*
// evidence kinds. first_party_ref_version is also populated by unrelated
// evidence families (Terraform module versions, Ansible role refs, Chef
// cookbook versions, ...) via the shared withFirstPartyRefDetails helper;
// attaching a GitHub Actions pin-safety label to one of those would be
// exactly the kind of fabrication issue #5372 was designed to avoid.
func TestResolvedRelationshipEvidenceArtifactsScopesRefFieldsToGitHubActions(t *testing.T) {
	t.Parallel()

	r := relationships.ResolvedRelationship{
		SourceRepoID: "repo-infra",
		TargetRepoID: "repo-module",
		Details: map[string]any{
			"evidence_preview": []map[string]any{
				{
					"kind":       "TERRAFORM_APP_REPO",
					"confidence": 0.9,
					"details": map[string]any{
						"path":                    "main.tf",
						"matched_value":           "terraform-module",
						"matched_alias":           "terraform-module",
						"extractor":               "terraform",
						"first_party_ref_kind":    "terraform_module_source",
						"first_party_ref_version": "v2.0.0",
					},
				},
			},
		},
	}

	artifacts := resolvedRelationshipEvidenceArtifacts(r)
	if len(artifacts) != 1 {
		t.Fatalf("resolvedRelationshipEvidenceArtifacts() len = %d, want 1", len(artifacts))
	}
	art := artifacts[0]

	if v, ok := art["ref_value"]; ok {
		t.Errorf("artifact[ref_value] = %v, want absent for a non-GitHub-Actions evidence kind", v)
	}
	if v, ok := art["ref_pinned"]; ok {
		t.Errorf("artifact[ref_pinned] = %v, want absent for a non-GitHub-Actions evidence kind", v)
	}
}
