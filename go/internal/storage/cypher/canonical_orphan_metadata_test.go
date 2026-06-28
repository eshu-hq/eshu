// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"
)

func TestBuildCanonicalRelationshipTargetsStampOrphanSweepMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		stmt           Statement
		wantGeneration string
		wantFragments  []string
	}{
		{
			name: "runtime platform",
			stmt: BuildCanonicalRuntimePlatformUpsert(CanonicalRuntimePlatformParams{
				InstanceID:       "instance-1",
				PlatformID:       "platform:eks:aws:my-cluster:production:us-east-1",
				PlatformName:     "my-cluster",
				PlatformKind:     "eks",
				PlatformProvider: "aws",
				Environment:      "production",
				PlatformRegion:   "us-east-1",
				PlatformLocator:  "arn:aws:eks:us-east-1:123:cluster/my-cluster",
				GenerationID:     "gen-runtime-platform",
			}, "finalization/workloads"),
			wantGeneration: "gen-runtime-platform",
			wantFragments: []string{
				"p.evidence_source = $evidence_source",
				"p.generation_id = $generation_id",
			},
		},
		{
			name: "repo dependency",
			stmt: BuildCanonicalRepoDependencyUpsert(CanonicalRepoDependencyParams{
				RepoID:       "repo-a",
				TargetRepoID: "repo-b",
				EvidenceType: "docker_compose_depends_on",
				GenerationID: "gen-repo-dependency",
			}, "finalization/workloads"),
			wantGeneration: "gen-repo-dependency",
			wantFragments: []string{
				"source_repo.evidence_source = $evidence_source",
				"source_repo.generation_id = $generation_id",
				"target_repo.evidence_source = $evidence_source",
				"target_repo.generation_id = $generation_id",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, fragment := range tc.wantFragments {
				if !strings.Contains(tc.stmt.Cypher, fragment) {
					t.Fatalf("Cypher missing metadata fragment %q: %s", fragment, tc.stmt.Cypher)
				}
			}
			if got := tc.stmt.Parameters["generation_id"]; got != tc.wantGeneration {
				t.Fatalf("generation_id = %v, want %s", got, tc.wantGeneration)
			}
			if got := tc.stmt.Parameters["evidence_source"]; got != "finalization/workloads" {
				t.Fatalf("evidence_source = %v, want finalization/workloads", got)
			}
		})
	}
}

func TestCanonicalCodeStructureNodesStampOrphanSweepMetadata(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		cypher    string
		fragments []string
	}{
		{
			name:   "directory node",
			cypher: canonicalNodeDirectoryNodeCypher,
			fragments: []string{
				"d.evidence_source = 'projector/canonical'",
			},
		},
		{
			name:   "root directory edge",
			cypher: canonicalNodeDirectoryDepth0EdgeCypher,
			fragments: []string{
				"rel.evidence_source = 'projector/canonical'",
			},
		},
		{
			name:   "nested directory edge",
			cypher: canonicalNodeDirectoryDepthNEdgeCypher,
			fragments: []string{
				"rel.evidence_source = 'projector/canonical'",
			},
		},
		{
			name:   "imported module",
			cypher: canonicalNodeModuleUpsertCypher,
			fragments: []string{
				"m.evidence_source = 'projector/canonical'",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, fragment := range tc.fragments {
				if !strings.Contains(tc.cypher, fragment) {
					t.Fatalf("Cypher missing metadata fragment %q: %s", fragment, tc.cypher)
				}
			}
		})
	}
}
