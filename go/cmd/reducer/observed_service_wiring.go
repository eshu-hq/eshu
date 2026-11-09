// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	stagedaudit "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/staged/audit"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func buildObservedReducerService(
	ctx context.Context,
	db *sql.DB,
	neo4jExecutor sourcecypher.Executor,
	cypherExecutor reducer.CypherExecutor,
	neo4jReader node.CypherReader,
	graphReader querycontract.GraphQuery,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	logger *slog.Logger,
) (reducer.Service, *snapshot.Refresher, *snapshot.Refresher, func(), error) {
	activeWorkers := new(atomic.Int64)
	instrumentedDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "reducer",
	}
	instrumentedNeo4j := &sourcecypher.InstrumentedExecutor{
		Inner:       neo4jExecutor,
		Tracer:      tracer,
		Instruments: instruments,
	}
	intentStore := postgres.NewSharedIntentStore(instrumentedDB)
	identityCache, err := postgres.NewIdentityEpochCache(
		instruments,
		identityCacheMaxBytes(getenv),
		postgres.WithHeartbeatInterval(reducerHeartbeatInterval),
	)
	if err != nil {
		return reducer.Service{}, nil, nil, nil, fmt.Errorf("identity epoch cache: %w", err)
	}
	// The winners-audit split scan (#10416 slice 1) opens a stale reader
	// access only when routing admits stale audits. Default off returns nil
	// without touching the database, so the Auditor stays writer-backed
	// exactly as before.
	staleAccess, closeStaleAccess, err := openWinnersStaleAccessIfEnabled(ctx, getenv, meter, tracer, logger)
	if err != nil {
		return reducer.Service{}, nil, nil, nil, err
	}
	serviceRunner, err := buildReducerService(
		ctx,
		instrumentedDB,
		instrumentedNeo4j,
		cypherExecutor,
		intentStore,
		neo4jReader,
		graphReader,
		getenv,
		tracer,
		instruments,
		logger,
		identityCache,
	)
	if err != nil {
		closeStaleAccess()
		return reducer.Service{}, nil, nil, nil, err
	}
	if staleAccess != nil {
		// Thread the stale reader to the Auditor only: verify, repair
		// queueing, and commit stay on the writer transaction either way.
		// The maintainer field is a shared pointer, so this survives the
		// service copies below.
		if maintainer := serviceRunner.SupplyChainImpactWinnersMaintainer; maintainer != nil {
			if winnersStagedAuditRouting(getenv) {
				// staged-audits runs the staged standby scan: per-scope
				// batches on one pinned member, compacted client-side.
				maintainer.Auditor = stagedaudit.StagedAuditor{
					Fleet:  pgaccess.NewStagedAuditAccessFleet(staleAccess),
					Store:  postgres.NewSupplyChainImpactWinnersStore(instrumentedDB),
					Logger: logger,
				}
			} else {
				maintainer.Auditor = postgres.NewSupplyChainImpactWinnersStore(instrumentedDB).
					WithStaleCandidates(staleAccess.StaleReader())
			}
		}
	}
	serviceRunner, err = withStatusSummaryWriter(serviceRunner, getenv, instrumentedDB, tracer, instruments, logger)
	if err != nil {
		closeStaleAccess()
		return reducer.Service{}, nil, nil, nil, err
	}
	graphRefresher, postgresRefresher, err := registerReducerObservableGauges(instruments, meter, db, activeWorkers, graphOrphanObserver(serviceRunner), graphReader, getenv, logger)
	if err != nil {
		closeStaleAccess()
		return reducer.Service{}, nil, nil, nil, err
	}
	serviceRunner.Executor = newActiveWorkerExecutor(serviceRunner.Executor, activeWorkers)
	return serviceRunner, graphRefresher, postgresRefresher, closeStaleAccess, nil
}
