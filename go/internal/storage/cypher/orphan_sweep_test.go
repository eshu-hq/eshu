// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
	"testing"
)

// forbiddenOrphanSweepPatterns are the relationship-existence predicate
// shapes proven mis-evaluated on both pinned NornicDB backends (#5147). No
// orphan-sweep statement may contain any of them.
var forbiddenOrphanSweepPatterns = []string{
	")--(",
	"NOT (",
	"COUNT {",
	"EXISTS {",
}

func assertNoForbiddenPatterns(t *testing.T, name, cypher string) {
	t.Helper()
	for _, forbidden := range forbiddenOrphanSweepPatterns {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("%s Cypher must not contain forbidden relationship-existence pattern %q:\n%s", name, forbidden, cypher)
		}
	}
}

func TestDefaultOrphanSweepLabelsIncludesCodeStructureLabels(t *testing.T) {
	t.Parallel()

	got := make(map[string]bool)
	for _, label := range DefaultOrphanSweepLabels() {
		got[string(label)] = true
	}

	for _, want := range []string{
		"Repository",
		"Platform",
		"EvidenceArtifact",
		"File",
		"Directory",
		"Module",
	} {
		if !got[want] {
			t.Fatalf("DefaultOrphanSweepLabels() missing %s in %#v", want, got)
		}
	}
}

func TestBuildCandidateOrphanNodesQueryUsesStaticLabelNoRelationshipPredicate(t *testing.T) {
	t.Parallel()

	for _, label := range DefaultOrphanSweepLabels() {
		t.Run(string(label), func(t *testing.T) {
			t.Parallel()
			stmt, ok := BuildCandidateOrphanNodesQuery(label, 25)
			if !ok {
				t.Fatalf("BuildCandidateOrphanNodesQuery(%s) ok = false, want true", label)
			}
			if stmt.Operation != OperationCanonicalRetract {
				t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
			}
			for _, want := range []string{
				fmt.Sprintf("MATCH (n:%s)", label),
				"n.evidence_source IS NOT NULL",
				"RETURN n.",
				"AS key, n.eshu_orphan_observed_at_unix AS observed_at",
				"LIMIT $limit",
			} {
				if !strings.Contains(stmt.Cypher, want) {
					t.Fatalf("candidate Cypher missing %q:\n%s", want, stmt.Cypher)
				}
			}
			assertNoForbiddenPatterns(t, "candidate", stmt.Cypher)
			if got := stmt.Parameters["limit"]; got != 25 {
				t.Fatalf("limit = %#v, want 25", got)
			}
		})
	}
}

func TestBuildCandidateOrphanNodesQueryUsesPerLabelIdentityKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		label OrphanSweepLabel
		key   string
	}{
		{OrphanSweepLabelRepository, "id"},
		{OrphanSweepLabelPlatform, "id"},
		{OrphanSweepLabelEvidenceArtifact, "id"},
		{OrphanSweepLabelFile, "path"},
		{OrphanSweepLabelDirectory, "path"},
		{OrphanSweepLabelModule, "name"},
	} {
		stmt, ok := BuildCandidateOrphanNodesQuery(tc.label, 10)
		if !ok {
			t.Fatalf("BuildCandidateOrphanNodesQuery(%s) ok = false", tc.label)
		}
		want := fmt.Sprintf("RETURN n.%s AS key", tc.key)
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("%s Cypher missing identity key %q:\n%s", tc.label, want, stmt.Cypher)
		}
	}
}

func TestRepositoryCandidateQueryExcludesSourceLocalCanonicalRepositories(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildCandidateOrphanNodesQuery(OrphanSweepLabelRepository, 10)
	if !ok {
		t.Fatal("BuildCandidateOrphanNodesQuery() ok = false, want true")
	}
	if !strings.Contains(stmt.Cypher, "n.evidence_source <> 'projector/canonical'") {
		t.Fatalf("repository candidate Cypher must exclude source-local canonical repositories:\n%s", stmt.Cypher)
	}

	// Every other label must NOT carry the repository-only exclusion.
	for _, label := range []OrphanSweepLabel{
		OrphanSweepLabelPlatform,
		OrphanSweepLabelEvidenceArtifact,
		OrphanSweepLabelFile,
		OrphanSweepLabelDirectory,
		OrphanSweepLabelModule,
	} {
		stmt, ok := BuildCandidateOrphanNodesQuery(label, 10)
		if !ok {
			t.Fatalf("BuildCandidateOrphanNodesQuery(%s) ok = false", label)
		}
		if strings.Contains(stmt.Cypher, "projector/canonical") {
			t.Fatalf("%s candidate Cypher must not carry the repository-only exclusion:\n%s", label, stmt.Cypher)
		}
	}
}

func TestBuildConnectedKeysQueryUsesConcreteRelationshipVariable(t *testing.T) {
	t.Parallel()

	for _, label := range DefaultOrphanSweepLabels() {
		t.Run(string(label), func(t *testing.T) {
			t.Parallel()
			stmt, ok := BuildConnectedKeysQuery(label, []string{"a", "b"})
			if !ok {
				t.Fatalf("BuildConnectedKeysQuery(%s) ok = false, want true", label)
			}
			for _, want := range []string{
				"UNWIND $keys AS candidate_key",
				fmt.Sprintf("MATCH (n:%s {", label),
				": candidate_key})-[r]-(m)",
				"RETURN DISTINCT n.",
			} {
				if !strings.Contains(stmt.Cypher, want) {
					t.Fatalf("connected-keys Cypher missing %q:\n%s", want, stmt.Cypher)
				}
			}
			assertNoForbiddenPatterns(t, "connected-keys", stmt.Cypher)
			// The UNWIND binding variable must differ from the RETURN alias:
			// reusing "key" for both silently returns zero rows on the pinned
			// NornicDB backends instead of erroring.
			if strings.Contains(stmt.Cypher, "UNWIND $keys AS key\n") {
				t.Fatalf("connected-keys Cypher must not reuse the RETURN alias as the UNWIND variable:\n%s", stmt.Cypher)
			}
			if got := stmt.Parameters["keys"]; fmt.Sprintf("%v", got) != "[a b]" {
				t.Fatalf("keys parameter = %#v, want [a b]", got)
			}
		})
	}
}

func TestBuildClearMarkSweepStatementsAreKeyAnchoredNoRelationshipPredicate(t *testing.T) {
	t.Parallel()

	keys := []string{"k1", "k2"}
	for _, label := range DefaultOrphanSweepLabels() {
		t.Run(string(label), func(t *testing.T) {
			t.Parallel()

			clearStmt, ok := BuildClearOrphanMarkerStatement(label, keys)
			if !ok {
				t.Fatalf("BuildClearOrphanMarkerStatement(%s) ok = false", label)
			}
			for _, want := range []string{
				"UNWIND $keys AS candidate_key",
				fmt.Sprintf("MATCH (n:%s {", label),
				"REMOVE n.eshu_orphan_observed_at_unix",
			} {
				if !strings.Contains(clearStmt.Cypher, want) {
					t.Fatalf("clear Cypher missing %q:\n%s", want, clearStmt.Cypher)
				}
			}
			assertNoForbiddenPatterns(t, "clear", clearStmt.Cypher)

			markStmt, ok := BuildMarkOrphanNodesStatement(label, keys, 1_786_000_000)
			if !ok {
				t.Fatalf("BuildMarkOrphanNodesStatement(%s) ok = false", label)
			}
			for _, want := range []string{
				"UNWIND $keys AS candidate_key",
				fmt.Sprintf("MATCH (n:%s {", label),
				"SET n.eshu_orphan_observed_at_unix = $observed_at_unix",
			} {
				if !strings.Contains(markStmt.Cypher, want) {
					t.Fatalf("mark Cypher missing %q:\n%s", want, markStmt.Cypher)
				}
			}
			assertNoForbiddenPatterns(t, "mark", markStmt.Cypher)
			if got := markStmt.Parameters["observed_at_unix"]; got != int64(1_786_000_000) {
				t.Fatalf("observed_at_unix = %#v, want int64 timestamp", got)
			}

			sweepStmt, ok := BuildSweepOrphanNodesStatement(label, keys)
			if !ok {
				t.Fatalf("BuildSweepOrphanNodesStatement(%s) ok = false", label)
			}
			for _, want := range []string{
				"UNWIND $keys AS candidate_key",
				fmt.Sprintf("MATCH (n:%s {", label),
				"DELETE n",
			} {
				if !strings.Contains(sweepStmt.Cypher, want) {
					t.Fatalf("sweep Cypher missing %q:\n%s", want, sweepStmt.Cypher)
				}
			}
			assertNoForbiddenPatterns(t, "sweep", sweepStmt.Cypher)
			if strings.Contains(sweepStmt.Cypher, "DETACH DELETE") {
				t.Fatalf("sweep Cypher must not detach-delete:\n%s", sweepStmt.Cypher)
			}
		})
	}
}

func TestBuildOrphanSweepStatementsRejectUnknownLabels(t *testing.T) {
	t.Parallel()

	unknown := OrphanSweepLabel("DynamicLabel")
	if _, ok := BuildCandidateOrphanNodesQuery(unknown, 1); ok {
		t.Fatal("BuildCandidateOrphanNodesQuery() ok = true, want false for unknown label")
	}
	if _, ok := BuildConnectedKeysQuery(unknown, []string{"a"}); ok {
		t.Fatal("BuildConnectedKeysQuery() ok = true, want false for unknown label")
	}
	if _, ok := BuildClearOrphanMarkerStatement(unknown, []string{"a"}); ok {
		t.Fatal("BuildClearOrphanMarkerStatement() ok = true, want false for unknown label")
	}
	if _, ok := BuildMarkOrphanNodesStatement(unknown, []string{"a"}, 1); ok {
		t.Fatal("BuildMarkOrphanNodesStatement() ok = true, want false for unknown label")
	}
	if _, ok := BuildSweepOrphanNodesStatement(unknown, []string{"a"}); ok {
		t.Fatal("BuildSweepOrphanNodesStatement() ok = true, want false for unknown label")
	}
}

func TestRepoRelationshipUpsertStampsTargetRepositoryForFutureSweeps(t *testing.T) {
	t.Parallel()

	for _, cypher := range []string{
		canonicalDeploysFromRepoRelationshipUpsertCypher,
		canonicalRepoDependencyUpsertCypher,
		batchCanonicalRepoDependencyUpsertCypher,
	} {
		for _, want := range []string{
			"ON CREATE SET source_repo.evidence_source",
			"source_repo.generation_id",
			"ON CREATE SET target_repo.evidence_source",
			"target_repo.generation_id",
		} {
			if !strings.Contains(cypher, want) {
				t.Fatalf("repo relationship Cypher missing sweep metadata %q:\n%s", want, cypher)
			}
		}
	}
}
