// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/legacy/retire"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	auditstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ExecuteCypherCountingNodeDeletes runs one statement exactly as ExecuteCypher
// does, through the persistent retrying executor, and returns the nodes its
// committed attempts deleted, read from the Bolt write summary the runner
// reports through sourcecypher.ReportWriteCounts. It satisfies retire.Writer
// (internal/reducer/workload/legacy/retire) so the legacy Workload retirement
// can report what it removed. Every counter entry is forwarded to a collector
// already on ctx, so differential capture still sees the statement.
func (e reducerCypherExecutor) ExecuteCypherCountingNodeDeletes(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (int64, error) {
	outer := sourcecypher.WriteCountsCollectorFromContext(ctx)
	collector := sourcecypher.NewWriteCountsCollector()
	err := e.ExecuteCypher(sourcecypher.WithWriteCountsCollector(ctx, collector), cypher, params)
	var deleted int64
	for _, entry := range collector.Entries() {
		deleted += entry.Counters.NodesDeleted
		outer.Add(entry)
	}
	return deleted, err
}

// legacyWorkloadRetirementHandler builds the POST /admin/retire-legacy-workloads
// handler for the reducer admin listener (#8795).
//
// Reads go through the raw session runner when it can serve rows: like the
// id-anchor census this is an operator probe, and a recorded copy of it would
// appear as a graph-specific statement in a differential capture. Execute is
// enabled only on Neo4j, the backend whose clause evaluation the retirement
// statement was proven on; NornicDB mis-evaluates those clauses, so there the
// route serves dry runs and refuses execute.
func legacyWorkloadRetirementHandler(
	cypherExec reducer.CypherExecutor,
	rawReader node.CypherReader,
	decorated query.GraphQuery,
	backend runtimecfg.GraphBackend,
	db *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*runtimecfg.LegacyWorkloadRetirementHandler, error) {
	reader, _ := rawReader.(legacyworkload.Reader)
	if reader == nil && decorated != nil {
		reader = decorated
	}
	writer, _ := cypherExec.(retire.Writer)
	var audit retire.Auditor
	if db != nil {
		audit = auditstore.NewGovernanceAuditStore(postgres.SQLDB{DB: db})
	}
	runner := &retire.Runner{
		Reader:           reader,
		Writer:           writer,
		ExecuteSupported: backend == runtimecfg.GraphBackendNeo4j,
		Audit:            audit,
		Instruments:      instruments,
		Logger:           logger,
	}
	return runtimecfg.NewLegacyWorkloadRetirementHandler(runner)
}
