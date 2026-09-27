// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type plainMaterializerExecutor struct{ calls int }

func (p *plainMaterializerExecutor) ExecuteCypher(context.Context, string, map[string]any) error {
	p.calls++
	return nil
}

type countingMaterializerExecutor struct {
	plainMaterializerExecutor
}

func (c *countingMaterializerExecutor) ExecuteCypherCountingRelationshipDeletes(
	ctx context.Context, cypherText string, params map[string]any,
) (int64, error) {
	_ = c.ExecuteCypher(ctx, cypherText, params)
	return 5, nil
}

// TestCypherExecutorGateForwardsRelationshipDeleteCounts pins that the gate
// keeps the #7285 retract's delete counts when the inner chain can count, and
// still executes the write (reporting it uncounted) when it cannot.
func TestCypherExecutorGateForwardsRelationshipDeleteCounts(t *testing.T) {
	t.Parallel()

	gate := cypher.NewBackpressureGate(2, nil)
	counting := &countingMaterializerExecutor{}
	wrapped, ok := WrapCypherExecutorWithGate(counting, gate).(retract.CountingExecutor)
	if !ok {
		t.Fatal("gated executor lost the relationship delete counting capability")
	}
	if deleted, err := wrapped.ExecuteCypherCountingRelationshipDeletes(context.Background(), "MATCH ()-[r]->() DELETE r", nil); err != nil || deleted != 5 || counting.calls != 1 {
		t.Fatalf("counting inner: deleted=%d err=%v calls=%d, want 5, nil, 1", deleted, err, counting.calls)
	}

	plain := &plainMaterializerExecutor{}
	wrapped = WrapCypherExecutorWithGate(plain, gate).(retract.CountingExecutor)
	deleted, err := wrapped.ExecuteCypherCountingRelationshipDeletes(context.Background(), "MATCH ()-[r]->() DELETE r", nil)
	if !errors.Is(err, retract.ErrUncounted) || deleted != 0 || plain.calls != 1 {
		t.Fatalf("plain inner: deleted=%d err=%v calls=%d, want the write executed and reported uncounted", deleted, err, plain.calls)
	}
}
