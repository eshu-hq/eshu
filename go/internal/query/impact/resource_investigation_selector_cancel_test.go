// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
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
