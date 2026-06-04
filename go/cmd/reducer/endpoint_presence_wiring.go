package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// endpointPresenceWiring returns the uid-exact endpoint-presence writer and
// lookup that back the cross-scope secrets/IAM projection gate (issue #1380),
// or (nil, nil) when the projection feature is off. Both are the same Postgres
// store; returning nil when disabled keeps the CloudResource/KubernetesWorkload
// materializers and the projection handler at their current behavior with zero
// extra write.
func endpointPresenceWiring(enabled bool, db postgres.ExecQueryer) (reducer.EndpointPresenceWriter, reducer.EndpointPresenceLookup) {
	if !enabled {
		return nil, nil
	}
	store := postgres.NewGraphEndpointPresenceStore(db)
	return store, store
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
