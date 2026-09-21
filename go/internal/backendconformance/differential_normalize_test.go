// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const testGenerationMiddle = "8abd9abff4e39ae9289735a54e40437d3c023cf85334fb3c29c0cdcc0a45ce59"

const otherGenerationMiddle = "962ed3558b58d6b8e03c7a95e778431c9539c9b29a0ca0d22556e9394e2f1a96"

// TestNormalizeComparisonStripsResolvedIDGenerationMiddle pins that a
// resolved_id embedding the run's generation stamp pairs across runs: the
// deployable-unit-correlation shape carries the generation as its middle
// segment, so only that segment normalizes.
func TestNormalizeComparisonStripsResolvedIDGenerationMiddle(t *testing.T) {
	t.Parallel()
	fp := func(middle string) DifferentialFingerprint {
		out, err := FingerprintStatement(
			"UNWIND $rows AS row MERGE (n {id: row.repo_id})",
			map[string]any{"rows": []any{
				map[string]any{"repo_id": "repository:r_1f68383d", "resolved_id": "deployable-unit-correlation:" + middle + ":repository:r_1f68383d:deployable-source"},
			}},
		)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	a, b := fp(testGenerationMiddle), fp(otherGenerationMiddle)
	if a != b {
		t.Fatalf("fingerprints differ across generation stamps:\n%+v\n%+v", a, b)
	}
	if strings.Contains(a.Parameters, testGenerationMiddle) {
		t.Fatalf("fingerprint leaks the generation stamp: %s", a.Parameters)
	}
}

// TestNormalizeComparisonLeavesDeterministicResolvedIDs pins the narrow
// scope: resolved_id shapes without a generation middle (code imports,
// package consumption) compare exactly, so distinct correlations never pair.
func TestNormalizeComparisonLeavesDeterministicResolvedIDs(t *testing.T) {
	t.Parallel()
	fp := func(resolved string) DifferentialFingerprint {
		out, err := FingerprintStatement(
			"UNWIND $rows AS row MERGE (n {id: row.repo_id})",
			map[string]any{"rows": []any{
				map[string]any{"repo_id": "repository:r_ea78e8bb", "resolved_id": resolved},
			}},
		)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	a := fp("code-imports:repository:r_ea78e8bb->repository:r_3eddcea1")
	b := fp("code-imports:repository:r_ea78e8bb->repository:r_98a4c8a8")
	if a == b {
		t.Fatal("fingerprints pair across distinct deterministic resolved_ids")
	}
}

// TestNormalizeComparisonLeavesContentHashes pins that bare content hashes
// (uids, digests) never normalize: only the resolved_id middle segment is
// generation-scoped.
func TestNormalizeComparisonLeavesContentHashes(t *testing.T) {
	t.Parallel()
	fp := func(uid string) DifferentialFingerprint {
		out, err := FingerprintStatement(
			"MERGE (n:Function {uid: $uid})",
			map[string]any{"uid": uid},
		)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	if fp("content-entity:e_8adc1231903a") == fp("content-entity:e_59c56c38911d") {
		t.Fatal("fingerprints pair across distinct content hashes")
	}
}

// TestDigestRowsCanonicalizesGraphNodes pins that backend-assigned node
// identity never enters the digest: two drivers' Nodes for the same
// labels-plus-properties digest identically.
func TestDigestRowsCanonicalizesGraphNodes(t *testing.T) {
	t.Parallel()
	rows := func(id int64, element string) []map[string]any {
		return []map[string]any{{
			"source": neo4jdriver.Node{
				Id:        id,
				ElementId: element,
				Labels:    []string{"Repository"},
				Props:     map[string]any{"id": "repository:r_ea78e8bb", "name": "demo"},
			},
		}}
	}
	a, err := DigestRows(rows(1, "4:aaa:1"), false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	b, err := DigestRows(rows(987654321, "4:bbb:999"), false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	if a != b {
		t.Fatalf("digests differ across backend node identity:\n%s\n%s", a, b)
	}
}

// TestDigestRowsCanonicalizesRelationshipShapes pins the NornicDB reshape:
// relationships(path) arrives as a driver Relationship on Neo4j and as a
// {type, properties} map on NornicDB, and both digest identically.
func TestDigestRowsCanonicalizesRelationshipShapes(t *testing.T) {
	t.Parallel()
	props := map[string]any{"confidence": 0.96, "evidence_type": "argocd"}
	neo := []map[string]any{{"rel": neo4jdriver.Relationship{
		Id: 12, ElementId: "5:aaa:12", StartId: 1, EndId: 2,
		Type: "DEPENDS_ON", Props: props,
	}}}
	nornic := []map[string]any{{"rel": map[string]any{
		"type": "DEPENDS_ON", "properties": props,
	}}}
	a, err := DigestRows(neo, false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	b, err := DigestRows(nornic, false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	if a != b {
		t.Fatalf("digests differ across backend relationship shapes:\n%s\n%s", a, b)
	}
}

// TestDigestRowsSortsNestedLists pins that unordered list projections
// (labels(), collect() without an ordering guarantee) digest identically
// regardless of the backend's return order.
func TestDigestRowsSortsNestedLists(t *testing.T) {
	t.Parallel()
	a, err := DigestRows([]map[string]any{{
		"labels":       []any{"Workload", "Repository"},
		"environments": []any{"prod", "stage"},
	}}, false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	b, err := DigestRows([]map[string]any{{
		"labels":       []any{"Repository", "Workload"},
		"environments": []any{"stage", "prod"},
	}}, false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	if a != b {
		t.Fatalf("digests differ across list order:\n%s\n%s", a, b)
	}
}

// TestDigestRowsNormalizesClockKeys pins that wall-clock observation columns
// (unverifiable across runs by construction) never enter the digest, while
// ordinary columns still distinguish rows.
func TestDigestRowsNormalizesClockKeys(t *testing.T) {
	t.Parallel()
	rows := func(observed float64) []map[string]any {
		return []map[string]any{{
			"key":         "evidence-artifact:deadbeef",
			"observed_at": observed,
		}}
	}
	a, err := DigestRows(rows(1758393600), false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	b, err := DigestRows(rows(1758397200), false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	if a != b {
		t.Fatalf("digests differ across wall-clock observation times:\n%s\n%s", a, b)
	}
	c, err := DigestRows([]map[string]any{{"key": "other", "observed_at": 1758393600}}, false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	if a == c {
		t.Fatal("digests pair across distinct non-clock columns")
	}
}

// TestDigestRowsNormalizesLineageDigestValues pins rows-only blindness for
// full run-scoped digests: cross-generation resolved_ edge ids cannot invert
// to content, so their cells normalize while every other cell still verifies.
func TestDigestRowsNormalizesLineageDigestValues(t *testing.T) {
	t.Parallel()
	rows := func(resolved string) []map[string]any {
		return []map[string]any{{
			"type":        "DEPENDS_ON",
			"resolved_id": resolved,
		}}
	}
	a, err := DigestRows(rows("resolved_8165734d70de32e7"), false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	b, err := DigestRows(rows("resolved_a88bb3682d8051ff"), false)
	if err != nil {
		t.Fatalf("DigestRows() error = %v", err)
	}
	if a != b {
		t.Fatalf("digests differ across run-scoped lineage ids:\n%s\n%s", a, b)
	}
}

// TestNormalizeComparisonStripsArtifactIDStamp pins key-scoped pairing for
// derivation-stamped artifact ids: an artifact_id cell is sha1 over the
// run's resolved id plus the derivation inputs, so two batches carrying the
// same logical writes under different run stamps pair. Bare sweep keys (no
// artifact_id key) never normalize.
func TestNormalizeComparisonStripsArtifactIDStamp(t *testing.T) {
	t.Parallel()
	fp := func(artifact string) DifferentialFingerprint {
		out, err := FingerprintStatement(
			"UNWIND $rows AS row MATCH (n {id: row.repo_id}) MERGE (m {artifact: row.artifact_id})",
			map[string]any{"rows": []any{
				map[string]any{"repo_id": "repository:r_1f68383d", "artifact_id": artifact},
			}},
		)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	if fp("evidence-artifact:1d10851ea4a4b015") != fp("evidence-artifact:e8b7a630c7b515cd") {
		t.Fatal("fingerprints differ across run-stamped artifact ids for the same logical write")
	}
}

// TestFingerprintsKeepLineageDigestValues pins the rows-only scope: full
// run-scoped digests in PARAMETERS still distinguish executions, so
// generation-derived sweep keys never pair across runs.
func TestFingerprintsKeepLineageDigestValues(t *testing.T) {
	t.Parallel()
	fp := func(key string) DifferentialFingerprint {
		out, err := FingerprintStatement(
			"UNWIND $keys AS candidate_key MATCH (n:EvidenceArtifact {id: candidate_key}) RETURN n",
			map[string]any{"keys": []any{key}},
		)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	if fp("evidence-artifact:1b74581f091865f7") == fp("evidence-artifact:0d1952e2977f603d") {
		t.Fatal("fingerprints pair across distinct run-scoped sweep keys")
	}
}
