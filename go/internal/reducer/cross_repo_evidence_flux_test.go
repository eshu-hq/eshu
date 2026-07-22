// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestResolvedRelationshipEvidenceArtifactsProjectsFluxGitRepositoryIdentity(t *testing.T) {
	t.Parallel()

	artifacts := resolvedRelationshipEvidenceArtifacts(relationships.ResolvedRelationship{
		Details: map[string]any{"evidence_preview": []map[string]any{{
			"kind": "FLUX_GIT_REPOSITORY_SOURCE",
			"details": map[string]any{
				"path":                          "clusters/prod/payments.yaml",
				"flux_git_repository_name":      "app-source",
				"flux_git_repository_namespace": "flux-system",
				"normalized_url":                "https://example.test/app",
			},
		}}},
	})
	if got, want := len(artifacts), 1; got != want {
		t.Fatalf("len(artifacts) = %d, want %d", got, want)
	}
	if got, want := artifacts[0]["matched_alias"], "app-source"; got != want {
		t.Fatalf("matched_alias = %#v, want %#v", got, want)
	}
	if got, want := artifacts[0]["flux_git_repository_name"], "app-source"; got != want {
		t.Fatalf("flux_git_repository_name = %#v, want %#v", got, want)
	}
	if got, want := artifacts[0]["flux_git_repository_namespace"], "flux-system"; got != want {
		t.Fatalf("flux_git_repository_namespace = %#v, want %#v", got, want)
	}
	if got, want := artifacts[0]["matched_value"], "https://example.test/app"; got != want {
		t.Fatalf("matched_value = %#v, want %#v", got, want)
	}
}
