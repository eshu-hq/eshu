// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDeriveRelationshipReferenceCandidateKeyAnsiblePrefix proves the
// artifact_type_norm column keeps the ansible_ prefix the family predicate
// matches with LIKE 'ansible_%' (#10875).
func TestDeriveRelationshipReferenceCandidateKeyAnsiblePrefix(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-ansible",
		ScopeID:      "git-repository-scope:github.com/acme/app",
		GenerationID: "gen-1",
		FactKind:     "content",
		Payload: map[string]any{
			"repo_id":       "github.com/acme/app",
			"artifact_type": "Ansible_Playbook",
			"content":       "hosts: all",
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if row.ArtifactTypeNorm != "ansible_playbook" {
		t.Fatalf("ArtifactTypeNorm = %q, want %q", row.ArtifactTypeNorm, "ansible_playbook")
	}
	if row.KeysVersion != relationshipReferenceCandidateKeysVersion {
		t.Fatalf("KeysVersion = %d, want %d", row.KeysVersion, relationshipReferenceCandidateKeysVersion)
	}
}

// TestDeriveRelationshipReferenceCandidateKeyYamlSuffixGate proves the
// path_norm column keeps the yaml suffix the family predicate gates the
// ArgoCD and Salt content markers on (#10875).
func TestDeriveRelationshipReferenceCandidateKeyYamlSuffixGate(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-yaml",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "content",
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"artifact_type": "argocd",
			"content_path":  "deploy/App.YAML",
			"content":       "kind: Application",
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if row.PathNorm != "deploy/app.yaml" {
		t.Fatalf("PathNorm = %q, want %q", row.PathNorm, "deploy/app.yaml")
	}
	if !row.ContentArgoCDKind {
		t.Error("ContentArgoCDKind = false, want true for 'kind: Application'")
	}
}

// TestDeriveRelationshipReferenceCandidateKeyApplicationSet proves an
// applicationset kind sets the ArgoCD marker through the application
// substring, exactly as the SQL LIKE '%kind: application%' does (#10875).
func TestDeriveRelationshipReferenceCandidateKeyApplicationSet(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-appset",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "content",
		Payload: map[string]any{
			"repo_id": "repo-a",
			"content": "KIND: ApplicationSet",
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if !row.ContentArgoCDKind {
		t.Error("ContentArgoCDKind = false, want true for 'KIND: ApplicationSet'")
	}
}

// TestDeriveRelationshipReferenceCandidateKeySaltSpaceBeforeColon proves the
// Salt conjunction is false when LIKE passes but the line regex fails:
// 'gitfs_remotes :' has a space before the colon, so the SQL conjunction
// (LIKE '%gitfs_remotes:%' AND ~ line) is false and Go must agree (#10875).
func TestDeriveRelationshipReferenceCandidateKeySaltSpaceBeforeColon(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-salt",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "content",
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "salt.sls",
			"content":       "gitfs_remotes :\n  - https://example.com/repo.git",
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if row.ContentSaltGitfs {
		t.Error("ContentSaltGitfs = true, want false when a space precedes the colon")
	}
}

// TestDeriveRelationshipReferenceCandidateKeySaltPositive pins the true arm
// of the Salt conjunction: a line-anchored 'gitfs_remotes:' entry (#10875).
func TestDeriveRelationshipReferenceCandidateKeySaltPositive(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-salt-pos",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "content",
		Payload: map[string]any{
			"repo_id": "repo-a",
			"content": "files:\ngitfs_remotes:\n  - https://example.com/repo.git",
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if !row.ContentSaltGitfs {
		t.Error("ContentSaltGitfs = false, want true for a line-anchored gitfs_remotes entry")
	}
}

// TestDeriveRelationshipReferenceCandidateKeyFluxEmptyArray proves an empty
// flux_git_repositories array does not set the Flux marker: the parser
// serializes an empty array into every YAML file (#10875).
func TestDeriveRelationshipReferenceCandidateKeyFluxEmptyArray(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-flux-empty",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "file",
		Payload: map[string]any{
			"repo_id":          "repo-a",
			"parsed_file_data": map[string]any{"flux_git_repositories": []any{}},
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if row.FluxGitNonEmpty {
		t.Error("FluxGitNonEmpty = true, want false for an empty array")
	}
	if row.UMFluxGitRepositories {
		t.Error("UMFluxGitRepositories = true, want false for an empty array")
	}
}

// TestDeriveRelationshipReferenceCandidateKeyUnconditionalMarkers proves the
// nested and case-variant unconditional markers land in the um_* columns
// (#10875).
func TestDeriveRelationshipReferenceCandidateKeyUnconditionalMarkers(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-markers",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "file",
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"Artifact_Type": "ArgoCD",
			"nested": map[string]any{
				"ArgoCD_Applications": []any{map[string]any{"name": "x"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true")
	}
	if !row.UMArtifactArgoCD {
		t.Error("UMArtifactArgoCD = false, want true for case-variant artifact_type key")
	}
	if !row.UMArgoCDApplications {
		t.Error("UMArgoCDApplications = false, want true for nested non-empty array")
	}
	if row.UMArgoCDApplicationsets {
		t.Error("UMArgoCDApplicationsets = true, want false (absent)")
	}
	if row.UMFluxGitRepositories {
		t.Error("UMFluxGitRepositories = true, want false (absent)")
	}
}

// TestDeriveRelationshipReferenceCandidateKeyEmptyStreamStillWrites proves a
// version-1 row is derived even when the token stream is empty: the reader
// trusts per-row keys_version, and an absent row means legacy fallback.
// Every non-tombstone eligible fact gets a row (#10875).
func TestDeriveRelationshipReferenceCandidateKeyEmptyStreamStillWrites(t *testing.T) {
	t.Parallel()

	row, ok, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-empty",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "content",
		Payload:      map[string]any{},
	})
	if err != nil {
		t.Fatalf("deriveRelationshipReferenceCandidateKeyRow() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() ok = false, want true for empty stream")
	}
	if row.ReferenceKey != "" {
		t.Fatalf("ReferenceKey = %q, want empty for a payload with no tokens", row.ReferenceKey)
	}
}

// TestDeriveRelationshipReferenceCandidateKeyMarshalErrorPropagates proves a
// payload the fact insert itself could not marshal is an error here, never a
// silent skip that would leave the row missing while the fact exists
// (#10875, #10861 D2).
func TestDeriveRelationshipReferenceCandidateKeyMarshalErrorPropagates(t *testing.T) {
	t.Parallel()

	_, _, err := deriveRelationshipReferenceCandidateKeyRow(facts.Envelope{
		FactID:       "fact-bad",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "content",
		Payload:      map[string]any{"repo_id": "repo-a", "bad": func() {}},
	})
	if err == nil {
		t.Fatal("deriveRelationshipReferenceCandidateKeyRow() error = nil, want marshal failure")
	}
}

// TestDeriveRelationshipReferenceCandidateKeySkipsTombstonesAndOtherKinds
// proves tombstones and ineligible kinds still yield no row (#10875).
func TestDeriveRelationshipReferenceCandidateKeySkipsTombstonesAndOtherKinds(t *testing.T) {
	t.Parallel()

	for name, envelope := range map[string]facts.Envelope{
		"tombstone": {
			FactID: "tombstone", ScopeID: "scope", GenerationID: "gen",
			FactKind: "content", IsTombstone: true,
			Payload: map[string]any{"repo_id": "repo-a"},
		},
		"repository": {
			FactID: "repository", ScopeID: "scope", GenerationID: "gen",
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-a"},
		},
	} {
		_, ok, err := deriveRelationshipReferenceCandidateKeyRow(envelope)
		if err != nil {
			t.Fatalf("%s: error = %v, want nil", name, err)
		}
		if ok {
			t.Fatalf("%s: ok = true, want false", name)
		}
	}
}
