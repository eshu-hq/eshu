// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// TestSemanticEntityRepairQueueAdapterEnqueueTranslatesAllFields drives a
// fully populated semantic.GraphProjectionPhaseRepair row through
// semanticEntityRepairQueueAdapter.Enqueue and asserts every field of the
// recorded gpphase-typed row equals the input.
//
// PR #6536 review: the existing wiring tests
// (defaults_domain_catalog_semantic_repair_queue_test.go) only assert the
// adapter's concrete type and that it wraps the right queue instance --
// nothing in the suite would fail if Enqueue's field-by-field translation in
// semantic_entity_repair_queue_adapter.go mapped a field to the wrong
// destination or dropped one. This test gives every field a distinct value
// so a swapped or dropped mapping is guaranteed to produce a mismatch.
func TestSemanticEntityRepairQueueAdapterEnqueueTranslatesAllFields(t *testing.T) {
	t.Parallel()

	in := semantic.GraphProjectionPhaseRepair{
		Key: gpphase.PhaseKey{
			ScopeID:          "scope-1",
			AcceptanceUnitID: "unit-1",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
		},
		Phase:         gpphase.PhaseSemanticNodesCommitted,
		CommittedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		EnqueuedAt:    time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC),
		NextAttemptAt: time.Date(2026, 1, 2, 3, 6, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 1, 2, 3, 7, 0, 0, time.UTC),
		Attempts:      2,
		LastError:     "boom",
	}

	rootQueue := &recordingSemanticEntityRepairQueue{}
	adapter := semanticEntityRepairQueueAdapter{queue: rootQueue}

	if err := adapter.Enqueue(context.Background(), []semantic.GraphProjectionPhaseRepair{in}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}

	if len(rootQueue.calls) != 1 || len(rootQueue.calls[0]) != 1 {
		t.Fatalf("recorded calls = %#v, want exactly one call carrying one repair", rootQueue.calls)
	}
	got := rootQueue.calls[0][0]

	want := gpphase.PhaseRepair{
		Key:           in.Key,
		Phase:         in.Phase,
		CommittedAt:   in.CommittedAt,
		EnqueuedAt:    in.EnqueuedAt,
		NextAttemptAt: in.NextAttemptAt,
		UpdatedAt:     in.UpdatedAt,
		Attempts:      in.Attempts,
		LastError:     in.LastError,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded repair = %#v, want %#v (every field of the adapted row must equal the input)", got, want)
	}
}

// TestGraphProjectionPhaseRepairStructsStayFieldForFieldParity pins the two
// independently declared repair struct copies -- gpphase/repair.go and
// code/semantic/graph_ports.go -- field-for-field. code/semantic keeps its
// own copy (issue #6061: the port stays narrow and local to the family), so
// the two declarations are hand-copied and nothing but code review previously
// caught them drifting apart. PR #6536 review, second half.
func TestGraphProjectionPhaseRepairStructsStayFieldForFieldParity(t *testing.T) {
	t.Parallel()

	leafType := reflect.TypeOf(gpphase.PhaseRepair{})
	semanticType := reflect.TypeOf(semantic.GraphProjectionPhaseRepair{})

	if leafType.NumField() != semanticType.NumField() {
		t.Fatalf("field count = %d (gpphase) vs %d (code/semantic), want equal", leafType.NumField(), semanticType.NumField())
	}
	for i := 0; i < leafType.NumField(); i++ {
		leafField := leafType.Field(i)
		semanticField := semanticType.Field(i)
		if leafField.Name != semanticField.Name {
			t.Fatalf("field %d name = %q (gpphase) vs %q (code/semantic), want equal", i, leafField.Name, semanticField.Name)
		}
		if leafField.Type != semanticField.Type {
			t.Fatalf("field %q type = %s (gpphase) vs %s (code/semantic), want equal", leafField.Name, leafField.Type, semanticField.Type)
		}
	}
}
