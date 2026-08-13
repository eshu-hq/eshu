// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"reflect"
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
			stmt, ok := BuildCandidateOrphanNodesQuery(label, 25, nil)
			if !ok {
				t.Fatalf("BuildCandidateOrphanNodesQuery(%s) ok = false, want true", label)
			}
			if stmt.Operation != OperationCanonicalRetract {
				t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
			}
			wants := []string{
				fmt.Sprintf("MATCH (n:%s)", label),
				"n.evidence_source IS NOT NULL",
				"LIMIT $limit",
			}
			// Module's identity is (name, lang), so it projects the key values
			// through a WITH and returns them as key_0/key_1; every other label
			// keeps the single `key` column and its byte-identical Cypher.
			if properties, _ := orphanSweepIdentityProperties(label); len(properties) > 1 {
				wants = append(wants,
					"WITH n.name AS key_0",
					"n.eshu_orphan_observed_at_unix AS observed_at",
					"RETURN key_0, key_1, observed_at")
			} else {
				wants = append(wants,
					"RETURN n.",
					"AS key, n.eshu_orphan_observed_at_unix AS observed_at")
			}
			for _, want := range wants {
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
		want  string
	}{
		{OrphanSweepLabelRepository, "RETURN n.id AS key"},
		{OrphanSweepLabelPlatform, "RETURN n.id AS key"},
		{OrphanSweepLabelEvidenceArtifact, "RETURN n.id AS key"},
		{OrphanSweepLabelFile, "RETURN n.path AS key"},
		{OrphanSweepLabelDirectory, "RETURN n.path AS key"},
		// Module's identity is (name, lang), so it projects both, aliased for
		// the composite cursor rather than a single `key` column.
		{OrphanSweepLabelModule, "WITH n.name AS key_0, coalesce(n.lang, '<absent>') AS key_1"},
	} {
		stmt, ok := BuildCandidateOrphanNodesQuery(tc.label, 10, nil)
		if !ok {
			t.Fatalf("BuildCandidateOrphanNodesQuery(%s) ok = false", tc.label)
		}
		want := tc.want
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("%s Cypher missing identity key %q:\n%s", tc.label, want, stmt.Cypher)
		}
	}
}

func TestRepositoryCandidateQueryExcludesSourceLocalCanonicalRepositories(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildCandidateOrphanNodesQuery(OrphanSweepLabelRepository, 10, nil)
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
		stmt, ok := BuildCandidateOrphanNodesQuery(label, 10, nil)
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
			stmt, ok := BuildConnectedKeysQuery(label, singleOrphanKeys([]string{"a", "b"}))
			if !ok {
				t.Fatalf("BuildConnectedKeysQuery(%s) ok = false, want true", label)
			}
			anchor := ": candidate_key})-[r]-(m)"
			if properties, _ := orphanSweepIdentityProperties(label); len(properties) > 1 {
				// A composite identity binds its anchor property from the
				// UNWIND row's first column and the rest in the WHERE clause.
				anchor = ": candidate_key.key_0})-[r]-(m)"
			}
			for _, want := range []string{
				"UNWIND $keys AS candidate_key",
				fmt.Sprintf("MATCH (n:%s {", label),
				anchor,
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
			// Compared structurally, not through a formatted string: a
			// composite label's rows are maps, and Go randomizes map
			// iteration order, so a "%v" rendering of map[key_0:a key_1:]
			// is not a stable value to assert against.
			var wantKeys any = []string{"a", "b"}
			if properties, _ := orphanSweepIdentityProperties(label); len(properties) > 1 {
				wantKeys = []map[string]any{
					{"key_0": "a", "key_1": ""},
					{"key_0": "b", "key_1": ""},
				}
			}
			if got := stmt.Parameters["keys"]; !reflect.DeepEqual(got, wantKeys) {
				t.Fatalf("keys parameter = %#v, want %#v", got, wantKeys)
			}
		})
	}
}

func TestBuildClearMarkSweepStatementsAreKeyAnchoredNoRelationshipPredicate(t *testing.T) {
	t.Parallel()

	keys := singleOrphanKeys([]string{"k1", "k2"})
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

			sweepStmt, ok := BuildSweepOrphanNodesStatement(label, keys, 0)
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
	if _, ok := BuildCandidateOrphanNodesQuery(unknown, 1, nil); ok {
		t.Fatal("BuildCandidateOrphanNodesQuery() ok = true, want false for unknown label")
	}
	if _, ok := BuildConnectedKeysQuery(unknown, singleOrphanKeys([]string{"a"})); ok {
		t.Fatal("BuildConnectedKeysQuery() ok = true, want false for unknown label")
	}
	if _, ok := BuildClearOrphanMarkerStatement(unknown, singleOrphanKeys([]string{"a"})); ok {
		t.Fatal("BuildClearOrphanMarkerStatement() ok = true, want false for unknown label")
	}
	if _, ok := BuildMarkOrphanNodesStatement(unknown, singleOrphanKeys([]string{"a"}), 1); ok {
		t.Fatal("BuildMarkOrphanNodesStatement() ok = true, want false for unknown label")
	}
	if _, ok := BuildSweepOrphanNodesStatement(unknown, singleOrphanKeys([]string{"a"}), 0); ok {
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
