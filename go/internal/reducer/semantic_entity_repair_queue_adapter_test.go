// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
)

// TestSemanticEntityRepairQueueAdapterEnqueueTranslatesAllFields drives a
// fully populated semanticentity.GraphProjectionPhaseRepair row through
// semanticEntityRepairQueueAdapter.Enqueue and asserts every field of the
// recorded root-typed row equals the input.
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

	in := semanticentity.GraphProjectionPhaseRepair{
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

	if err := adapter.Enqueue(context.Background(), []semanticentity.GraphProjectionPhaseRepair{in}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}

	if len(rootQueue.calls) != 1 || len(rootQueue.calls[0]) != 1 {
		t.Fatalf("recorded calls = %#v, want exactly one call carrying one repair", rootQueue.calls)
	}
	got := rootQueue.calls[0][0]

	want := GraphProjectionPhaseRepair{
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
// independently declared GraphProjectionPhaseRepair struct copies --
// graph_projection_phase_repair.go (root) and semanticentity/graph_ports.go
// -- field-for-field. semanticentity cannot import the root's struct (issue
// #6061: a family subpackage never imports the reducer root), so the two
// declarations are hand-copied and nothing but code review previously
// caught them drifting apart. PR #6536 review, second half.
func TestGraphProjectionPhaseRepairStructsStayFieldForFieldParity(t *testing.T) {
	t.Parallel()

	rootType := reflect.TypeOf(GraphProjectionPhaseRepair{})
	semanticType := reflect.TypeOf(semanticentity.GraphProjectionPhaseRepair{})

	if rootType.NumField() != semanticType.NumField() {
		t.Fatalf("field count = %d (root) vs %d (semanticentity), want equal", rootType.NumField(), semanticType.NumField())
	}
	for i := 0; i < rootType.NumField(); i++ {
		rootField := rootType.Field(i)
		semanticField := semanticType.Field(i)
		if rootField.Name != semanticField.Name {
			t.Fatalf("field %d name = %q (root) vs %q (semanticentity), want equal", i, rootField.Name, semanticField.Name)
		}
		if rootField.Type != semanticField.Type {
			t.Fatalf("field %q type = %s (root) vs %s (semanticentity), want equal", rootField.Name, rootField.Type, semanticField.Type)
		}
	}
}
