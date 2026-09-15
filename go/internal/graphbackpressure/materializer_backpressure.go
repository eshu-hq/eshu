// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// cypherExecutorGate bounds a reducer.CypherExecutor (the workload and
// infrastructure-platform materializer write path) on a shared backpressure
// gate. The materializer writes via ExecuteCypher, a separate adapter from the
// canonical Executor path, so without this wrapper materializer writes bypass
// the ESHU_GRAPH_WRITE_MAX_IN_FLIGHT pool entirely and a slow backend still
// receives unbounded concurrent materializer writes (#3652 P2).
//
// The wrapper acquires a permit, then delegates to inner, so it is the outermost
// layer of the materializer write path and one permit covers the whole inner
// write.
type cypherExecutorGate struct {
	inner reducer.CypherExecutor
	gate  *cypher.BackpressureGate
}

// ExecuteCypher acquires one shared permit before delegating to the wrapped
// materializer executor, so materializer writes draw from the same in-flight
// pool as canonical and semantic graph writes.
func (e cypherExecutorGate) ExecuteCypher(ctx context.Context, query string, params map[string]any) error {
	release, err := e.gate.Acquire(ctx, "materialize_cypher")
	if err != nil {
		return err
	}
	defer release()
	return e.inner.ExecuteCypher(ctx, query, params)
}

// ExecuteCypherGroup holds one shared permit for the complete atomic group.
func (e cypherExecutorGate) ExecuteCypherGroup(
	ctx context.Context,
	statements []reducer.CypherGroupStatement,
) error {
	grouped, ok := e.inner.(reducer.CypherGroupExecutor)
	if !ok {
		return fmt.Errorf("materializer executor %T does not support atomic groups", e.inner)
	}
	release, err := e.gate.Acquire(ctx, "materialize_cypher_group")
	if err != nil {
		return err
	}
	defer release()
	return grouped.ExecuteCypherGroup(ctx, statements)
}

// WrapCypherExecutorWithGate bounds the materializer CypherExecutor path on the
// shared gate so workload and infrastructure-platform materializer writes draw
// from the same permit pool as every other reducer graph write (#3652 P2). A nil
// gate returns inner unchanged (passthrough) so a disabled bound adds no wrapper.
func WrapCypherExecutorWithGate(inner reducer.CypherExecutor, gate *cypher.BackpressureGate) reducer.CypherExecutor {
	if gate == nil {
		return inner
	}
	return cypherExecutorGate{inner: inner, gate: gate}
}
