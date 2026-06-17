package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// endpointPresenceWiring returns the uid-exact endpoint-presence writer and
// lookup backing two readiness gates: the cross-scope secrets/IAM projection
// gate (issue #1380) and the handles_route endpoint-presence gate (#2809). Both
// are the same Postgres store. Returning nil when disabled keeps the
// CloudResource/KubernetesWorkload materializers, the secrets/IAM handler, and
// the handles_route gate at their pre-gate behavior with zero extra write.
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
// committing in EVERY reducer deployment — not only when secrets-IAM graph
// projection happens to be enabled. Set it to a false value to restore the
// pre-#2809 behavior (the edge may then drop on a cold first generation).
const handlesRouteEndpointPresenceGateEnabledEnv = "ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED"

// handlesRouteEndpointPresenceGateEnabled reports whether the handles_route
// endpoint-presence gate should be wired. Default true.
func handlesRouteEndpointPresenceGateEnabled(getenv func(string) string) bool {
	return loadBoolOrDefault(getenv, handlesRouteEndpointPresenceGateEnabledEnv, true)
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
