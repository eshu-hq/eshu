// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// endpointPresenceWiring returns an endpoint-presence writer and lookup backed by
// the shared Postgres store, or (nil, nil) when disabled. It is called twice with
// independent enable flags so the two presence concerns never couple:
//   - the uid-exact secrets/IAM projection gate (issue #1380), enabled by the
//     secrets/IAM graph projection flag; and
//   - the property-keyed (repo_id, path) handles_route gate (#2809), enabled by
//     its own kill switch (handlesRouteEndpointPresenceGateEnabled).
//
// Returning nil when a concern is disabled keeps its producers and gate at the
// pre-gate behavior with zero extra write.
func endpointPresenceWiring(enabled bool, db postgres.ExecQueryer) (reducer.EndpointPresenceWriter, reducer.EndpointPresenceLookup) {
	if !enabled {
		return nil, nil
	}
	store := postgres.NewGraphEndpointPresenceStore(db)
	return store, store
}

// handlesRouteEndpointPresenceGateEnabledEnv toggles the handles_route
// endpoint-presence readiness gate (#2809). It defaults ON so
// Function-[:HANDLES_ROUTE]->Endpoint edges are gated on their target endpoint
// committing in EVERY reducer deployment — not only when secrets/IAM graph
// projection happens to be enabled. Set it to a false value to restore the
// pre-#2809 behavior (the edge may then drop on a cold first generation).
const handlesRouteEndpointPresenceGateEnabledEnv = "ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED"

// handlesRouteEndpointPresenceGateEnabled reports whether the handles_route
// endpoint-presence gate should be wired. Default true. Because the handles_route
// presence writer and lookup are wired solely from this flag, setting it false
// always restores the pre-#2809 behavior regardless of the secrets/IAM flag.
func handlesRouteEndpointPresenceGateEnabled(getenv func(string) string) bool {
	return loadBoolOrDefault(getenv, handlesRouteEndpointPresenceGateEnabledEnv, true)
}

// endpointPresenceWirings holds the two independent presence writer/lookup pairs.
// Keeping them in one value documents that they share a Postgres store yet are
// gated separately: the secrets/IAM uid pair (#1380) and the handles_route
// (repo_id, path) pair (#2809) must never be coupled.
type endpointPresenceWirings struct {
	secretsIAMWriter   reducer.EndpointPresenceWriter
	secretsIAMLookup   reducer.EndpointPresenceLookup
	handlesRouteWriter reducer.EndpointPresenceWriter
	handlesRouteLookup reducer.EndpointPresenceLookup
}

// newEndpointPresenceWirings builds both presence pairs from their independent
// enable flags. The secrets/IAM pair is enabled only when secrets/IAM graph
// projection is on; the handles_route pair is enabled solely by its own kill
// switch, so neither flag can disable or widen the other's writes.
func newEndpointPresenceWirings(
	getenv func(string) string,
	secretsIAMEnabled bool,
	db postgres.ExecQueryer,
) endpointPresenceWirings {
	siWriter, siLookup := endpointPresenceWiring(secretsIAMEnabled, db)
	hrWriter, hrLookup := endpointPresenceWiring(handlesRouteEndpointPresenceGateEnabled(getenv), db)
	return endpointPresenceWirings{
		secretsIAMWriter:   siWriter,
		secretsIAMLookup:   siLookup,
		handlesRouteWriter: hrWriter,
		handlesRouteLookup: hrLookup,
	}
}

// newHandlerEdgeWriter constructs the shared canonical edge writer used by the
// reducer handlers, applying the instruments, logger, and grouped-write batch
// tuning in one place.
func newHandlerEdgeWriter(
	neo4jExec sourcecypher.Executor,
	batchSize int,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	inheritanceGroupBatchSize int,
	sqlRelationshipGroupBatchSize int,
) *sourcecypher.EdgeWriter {
	writer := sourcecypher.NewEdgeWriter(neo4jExec, batchSize)
	writer.Instruments = instruments
	writer.Logger = logger
	writer.InheritanceGroupBatchSize = inheritanceGroupBatchSize
	writer.SQLRelationshipGroupBatchSize = sqlRelationshipGroupBatchSize
	return writer
}
