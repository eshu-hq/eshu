// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reducerGraphWriteChain keeps fault injection on the shared retry-capable
// base, canonical and materializer writes under the same permit pools, and
// the tagged lease-expiry pause outside the canonical permit. The raw base
// remains available for the separately bounded semantic path.
type reducerGraphWriteChain struct {
	raw       sourcecypher.Executor
	canonical sourcecypher.Executor
	cypher    reducer.CypherExecutor
	gate      reducerGraphWriteGate
	barrier   *ifaLeaseExpiryBarrier
}

// buildReducerGraphWriteChain preserves the graph wrapper order. A real graph
// call returns through the permit and retry layers before the optional barrier
// waits in the still-active GCP reducer handler.
func buildReducerGraphWriteChain(
	graph sourcecypher.Executor,
	cypher reducer.CypherExecutor,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (reducerGraphWriteChain, error) {
	// The tagged fault decorator must wrap the persistent RetryingExecutor
	// before either canonical or semantic graph paths are constructed.
	wrapped, err := wrapIfaFaultExecutor(graph, getenv, logger)
	if err != nil {
		return reducerGraphWriteChain{}, fmt.Errorf("wrap ifa graph executor: %w", err)
	}
	gate := newReducerGraphWriteGate(getenv, instruments)
	barrier, err := newIfaLeaseExpiryBarrier(getenv)
	if err != nil {
		return reducerGraphWriteChain{}, err
	}
	return reducerGraphWriteChain{
		raw:       wrapped,
		canonical: barrier.wrapGraph(gate.boundExecutor(wrapped)),
		cypher:    gate.boundCypherExecutor(cypher),
		gate:      gate,
		barrier:   barrier,
	}, nil
}
