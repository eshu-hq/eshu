// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestResourceInvestigationSelectorFanoutNeverReadsOnCanceledContext is the
// #8881 regression. A context canceled before the fanout starts has both
// select cases ready in every goroutine (a free semaphore slot and a closed
// Done channel), and Go picks one of two ready cases at random. Without a
// ctx.Err() check ahead of the select, each of the 11 default-label goroutines
// started its graph read about half the time, so the resolver reported a
// client cancel only when at least one goroutine happened to pick Done. A
// canceled request must always return its cancel error and never issue a
// graph read. Not parallel-sensitive: the graph and context are per iteration.
func TestResourceInvestigationSelectorFanoutNeverReadsOnCanceledContext(t *testing.T) {
	t.Parallel()

	queries := resourceInvestigationSelectorCyphers(
		ResourceInvestigationRequest{Query: "orders", Limit: 1},
		querycontract.RepositoryAccessFilter{AllScopes: true},
		ResourceInvestigationExactSelectorPredicates,
	)
	if len(queries) != len(resourceInvestigationDefaultLabels) {
		t.Fatalf("queries = %d, want %d", len(queries), len(resourceInvestigationDefaultLabels))
	}
	for i := range 300 {
		graph := &resourceInvestigationSelectorGraph{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := runResourceInvestigationSelectorFanout(ctx, graph, queries, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d: err = %v, want context.Canceled", i, err)
		}
		graph.mu.Lock()
		reads := len(graph.calls)
		graph.mu.Unlock()
		if reads != 0 {
			t.Fatalf("iteration %d: %d graph reads on a canceled context, want 0", i, reads)
		}
	}
}

// errAfterFirstContext reports no error on its first Err call and
// context.Canceled on every later call. Its Done channel is nil, so it is never
// ready: a fanout goroutine can only stop on the Err recheck, never on a
// select case. That isolates the recheck that runs after a goroutine takes a
// semaphore slot.
type errAfterFirstContext struct {
	context.Context
	errCalls atomic.Int32
}

func (c *errAfterFirstContext) Done() <-chan struct{} { return nil }

func (c *errAfterFirstContext) Err() error {
	if c.errCalls.Add(1) == 1 {
		return nil
	}
	return context.Canceled
}

// TestResourceInvestigationSelectorFanoutRechecksContextAfterTakingSlot pins
// the #8881 recheck that follows the semaphore receive. The context passes
// the check ahead of the fanout (its first Err call) and reports a cancel on
// every later call, with a Done channel that never fires. Every goroutine
// takes a slot, so only the post-slot ctx.Err() check keeps it from issuing a
// graph read. It is deterministic: no timing and no random select outcome.
func TestResourceInvestigationSelectorFanoutRechecksContextAfterTakingSlot(t *testing.T) {
	t.Parallel()

	queries := resourceInvestigationSelectorCyphers(
		ResourceInvestigationRequest{Query: "orders", Limit: 1},
		querycontract.RepositoryAccessFilter{AllScopes: true},
		ResourceInvestigationExactSelectorPredicates,
	)
	graph := &resourceInvestigationSelectorGraph{}
	ctx := &errAfterFirstContext{Context: context.Background()}
	_, err := runResourceInvestigationSelectorFanout(ctx, graph, queries, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	graph.mu.Lock()
	reads := len(graph.calls)
	graph.mu.Unlock()
	if reads != 0 {
		t.Fatalf("%d graph reads after the context reported a cancel, want 0", reads)
	}
	if got, want := ctx.errCalls.Load(), int32(1+len(queries)); got != want {
		t.Fatalf("Err calls = %d, want %d (one before the fanout, one per goroutine)", got, want)
	}
}
