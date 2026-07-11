// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func buildObservedReducerService(
	ctx context.Context,
	db *sql.DB,
	neo4jExecutor sourcecypher.Executor,
	cypherExecutor reducer.CypherExecutor,
	neo4jReader sourcecypher.CypherReader,
	graphReader query.GraphQuery,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	logger *slog.Logger,
) (reducer.Service, error) {
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
	)
	if err != nil {
		return reducer.Service{}, err
	}
	if err := registerReducerObservableGauges(instruments, meter, db, activeWorkers, graphOrphanObserver(serviceRunner), graphReader, getenv); err != nil {
		return reducer.Service{}, err
	}
	serviceRunner.Executor = newActiveWorkerExecutor(serviceRunner.Executor, activeWorkers)
	return serviceRunner, nil
}
