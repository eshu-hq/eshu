// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"testing"

	leaf "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/crossplane"
)

// goldenCrossplaneSatisfiedByEdgeUpsertCypher pins the exact pre-move (#6694)
// upsert template byte-for-byte. The leaf move must not alter a single byte:
// this test compares the root writer's emitted Cypher AND the leaf constant
// against this literal with ==, so any drift fails loudly.
const goldenCrossplaneSatisfiedByEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (claim:K8sResource {uid: row.claim_uid})
MATCH (xrd:CrossplaneXRD {uid: row.xrd_uid})
MERGE (claim)-[rel:SATISFIED_BY]->(xrd)
SET rel.resolution_mode = row.resolution_mode,
    rel.claim_group = row.claim_group,
    rel.claim_kind = row.claim_kind,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// goldenRetractCrossplaneSatisfiedByEdgesCypher pins the exact pre-move
// (#6694) retract template byte-for-byte, same contract as the upsert golden
// above.
const goldenRetractCrossplaneSatisfiedByEdgesCypher = `MATCH (:K8sResource)-[rel:SATISFIED_BY]->(:CrossplaneXRD)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

func TestCrossplaneSatisfiedByEdgeLeafParityUpsertByteIdentical(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 2)
	rows := crossplaneSatisfiedByEdgeRows(5)
	if err := writer.WriteCrossplaneSatisfiedByEdges(
		context.Background(), rows, "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource,
	); err != nil {
		t.Fatalf("WriteCrossplaneSatisfiedByEdges returned error: %v", err)
	}

	parts := leaf.BuildUpsertParts(rows, "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource, 2)
	if len(executor.calls) != len(parts) {
		t.Fatalf("root statements = %d, leaf parts = %d", len(executor.calls), len(parts))
	}
	for i, stmt := range executor.calls {
		if stmt.Operation != OperationCanonicalUpsert {
			t.Fatalf("statement[%d] Operation = %v, want OperationCanonicalUpsert", i, stmt.Operation)
		}
		if stmt.Drain {
			t.Fatalf("statement[%d] Drain = true, want false (upserts stay grouped)", i)
		}
		if stmt.Cypher != parts[i].Cypher {
			t.Fatalf("statement[%d] Cypher differs from leaf part:\nroot:\n%s\nleaf:\n%s", i, stmt.Cypher, parts[i].Cypher)
		}
		if !reflect.DeepEqual(stmt.Parameters, parts[i].Parameters) {
			t.Fatalf("statement[%d] parameters differ from leaf part:\nroot: %#v\nleaf: %#v", i, stmt.Parameters, parts[i].Parameters)
		}
		if stmt.Cypher != goldenCrossplaneSatisfiedByEdgeUpsertCypher {
			t.Fatalf("statement[%d] Cypher drifted from the pre-move golden template:\n%s", i, stmt.Cypher)
		}
	}
	if leaf.SatisfiedByEdgeUpsertCypher != goldenCrossplaneSatisfiedByEdgeUpsertCypher {
		t.Fatalf("leaf upsert template drifted from the pre-move golden template:\n%s", leaf.SatisfiedByEdgeUpsertCypher)
	}
}

func TestCrossplaneSatisfiedByEdgeLeafParityRetractByteIdentical(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 0)
	if err := writer.RetractCrossplaneSatisfiedByEdges(
		context.Background(),
		[]string{"scope-1", "scope-2"},
		"gen-1",
		crossplaneSatisfiedByEvidenceSource,
	); err != nil {
		t.Fatalf("RetractCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}

	part := leaf.BuildRetractPart([]string{"scope-1", "scope-2"}, "gen-1", crossplaneSatisfiedByEvidenceSource)
	stmt := executor.calls[0]
	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("retract Operation = %v, want OperationCanonicalRetract", stmt.Operation)
	}
	if stmt.Cypher != part.Cypher {
		t.Fatalf("retract Cypher differs from leaf part:\nroot:\n%s\nleaf:\n%s", stmt.Cypher, part.Cypher)
	}
	if !reflect.DeepEqual(stmt.Parameters, part.Parameters) {
		t.Fatalf("retract parameters differ from leaf part:\nroot: %#v\nleaf: %#v", stmt.Parameters, part.Parameters)
	}
	if stmt.Cypher != goldenRetractCrossplaneSatisfiedByEdgesCypher {
		t.Fatalf("retract Cypher drifted from the pre-move golden template:\n%s", stmt.Cypher)
	}
	if leaf.RetractSatisfiedByEdgesCypher != goldenRetractCrossplaneSatisfiedByEdgesCypher {
		t.Fatalf("leaf retract template drifted from the pre-move golden template:\n%s", leaf.RetractSatisfiedByEdgesCypher)
	}
	if !part.IsRetract {
		t.Fatal("leaf retract part must set IsRetract so the root adapter maps it to OperationCanonicalRetract")
	}
}

func TestCrossplaneLeafMetadataKeysMatchParent(t *testing.T) {
	t.Parallel()

	// The leaf's unexported metadata keys must stay identical to the parent
	// cypher package's StatementMetadata*Key constants: the root adapter
	// copies Parameters through untouched, so a drift would silently fork the
	// phase/entity-label/summary dimensions. Parameters are looked up by the
	// PARENT keys — a leaf-side rename makes these lookups miss and fails.
	rows := crossplaneSatisfiedByEdgeRows(3)
	upserts := leaf.BuildUpsertParts(rows, "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource, 2)
	if len(upserts) == 0 {
		t.Fatal("no upsert parts built")
	}
	for i, p := range upserts {
		if got := p.Parameters[StatementMetadataPhaseKey]; got != leaf.Phase {
			t.Fatalf("upsert part[%d] Parameters[%q] = %#v, want leaf phase %q", i, StatementMetadataPhaseKey, got, leaf.Phase)
		}
		if got := p.Parameters[StatementMetadataEntityLabelKey]; got != leaf.EdgeLabel {
			t.Fatalf("upsert part[%d] Parameters[%q] = %#v, want leaf edge label %q", i, StatementMetadataEntityLabelKey, got, leaf.EdgeLabel)
		}
		if _, ok := p.Parameters[StatementMetadataSummaryKey]; !ok {
			t.Fatalf("upsert part[%d] missing Parameters[%q]", i, StatementMetadataSummaryKey)
		}
	}
	retract := leaf.BuildRetractPart([]string{"scope-1"}, "gen-1", crossplaneSatisfiedByEvidenceSource)
	if got := retract.Parameters[StatementMetadataPhaseKey]; got != leaf.Phase {
		t.Fatalf("retract Parameters[%q] = %#v, want leaf phase %q", StatementMetadataPhaseKey, got, leaf.Phase)
	}
	if got := retract.Parameters[StatementMetadataEntityLabelKey]; got != leaf.EdgeLabel {
		t.Fatalf("retract Parameters[%q] = %#v, want leaf edge label %q", StatementMetadataEntityLabelKey, got, leaf.EdgeLabel)
	}
	if _, ok := retract.Parameters[StatementMetadataSummaryKey]; !ok {
		t.Fatalf("retract missing Parameters[%q]", StatementMetadataSummaryKey)
	}
}
