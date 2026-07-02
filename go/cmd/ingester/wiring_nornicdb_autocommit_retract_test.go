// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestNornicDBPhaseGroupExecutorMixedPhaseRunsRetractAutocommitThenGroupsUpsert
// verifies that a mixed phase carrying a Drain-marked relationship retract plus a
// non-drain upsert (the structural_edges shape emitted by the Helm
// template-value edge writer) runs the retract as a single standalone autocommit
// statement and sends only the upsert to the grouped ExecuteWrite path. The
// retract must NOT be part of the grouped transaction (it silently no-ops there,
// #4476), and it must run exactly once (no LIMIT drain loop for the small
// dedicated HELM_VALUE_REFERENCE edge type).
func TestNornicDBPhaseGroupExecutorMixedPhaseRunsRetractAutocommitThenGroupsUpsert(t *testing.T) {
	t.Parallel()

	reader := &drainCountReader{counts: []int64{50}}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `UNWIND $source_uids AS uid
MATCH (u:HelmTemplateValueUsage {uid: uid})-[r:HELM_VALUE_REFERENCE]->(:HelmValueDefinition)
WHERE r.evidence_source = 'projector/canonical'
  AND r.generation_id <> $generation_id
DELETE r`,
			Parameters: map[string]any{"source_uids": []string{"htvu:1"}, "generation_id": "gen-2"},
			Drain:      true,
		},
		{
			Operation:  sourcecypher.OperationCanonicalUpsert,
			Cypher:     "UNWIND $rows AS row MATCH (u:HelmTemplateValueUsage {uid: row.source_uid}) MATCH (d:HelmValueDefinition {uid: row.target_uid}) MERGE (u)-[r:HELM_VALUE_REFERENCE]->(d)",
			Parameters: map[string]any{"rows": []map[string]any{{"source_uid": "htvu:1", "target_uid": "hvd:1"}}},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	// Retract runs as a single autocommit RunWrite (no drain loop).
	if reader.callIdx != 1 {
		t.Fatalf("autocommit retract RunWrite calls = %d, want 1", reader.callIdx)
	}
	// Upsert grouped; retract must not be in the group.
	if inner.callCount == 0 {
		t.Fatal("upsert was not sent to the grouped ExecuteWrite path")
	}
	for _, s := range inner.groupStatements {
		if s.Operation == sourcecypher.OperationCanonicalRetract {
			t.Fatalf("Drain-marked retract must not be part of the grouped transaction; found it in group")
		}
	}
	sawUpsert := false
	for _, s := range inner.groupStatements {
		if s.Operation == sourcecypher.OperationCanonicalUpsert {
			sawUpsert = true
		}
	}
	if !sawUpsert {
		t.Fatalf("grouped statements missing the upsert: %+v", inner.groupStatements)
	}
}

// TestNornicDBPhaseGroupExecutorMixedPhaseNilDrainReaderStillUngroupsRetract
// verifies that even with no drain reader wired, a Drain-marked retract is NOT
// batched into the grouped ExecuteWrite transaction (where it would silently
// no-op, #4476): it runs as its own statement via the inner executor, and only
// the upsert reaches the grouped path.
func TestNornicDBPhaseGroupExecutorMixedPhaseNilDrainReaderStillUngroupsRetract(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 100,
		drainReader:   nil, // no RunWrite-capable executor wired
	}

	stmts := []sourcecypher.Statement{
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     "UNWIND $source_uids AS uid MATCH (u:HelmTemplateValueUsage {uid: uid})-[r:HELM_VALUE_REFERENCE]->(:HelmValueDefinition) WHERE r.generation_id <> $generation_id DELETE r",
			Parameters: map[string]any{"source_uids": []string{"htvu:1"}, "generation_id": "gen-2"},
			Drain:      true,
		},
		{
			Operation:  sourcecypher.OperationCanonicalUpsert,
			Cypher:     "UNWIND $rows AS row MERGE (u)-[r:HELM_VALUE_REFERENCE]->(d)",
			Parameters: map[string]any{"rows": []map[string]any{{"source_uid": "htvu:1"}}},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	// Retract ran ungrouped via inner.Execute, not in the grouped transaction.
	if len(inner.executeStatements) != 1 || inner.executeStatements[0].Operation != sourcecypher.OperationCanonicalRetract {
		t.Fatalf("Drain retract must run via inner.Execute (ungrouped); executeStatements=%+v", inner.executeStatements)
	}
	for _, s := range inner.groupStatements {
		if s.Operation == sourcecypher.OperationCanonicalRetract {
			t.Fatalf("Drain retract must NOT be in the grouped transaction even when drainReader is nil")
		}
	}
}
