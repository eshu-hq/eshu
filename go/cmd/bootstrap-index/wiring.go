// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const bootstrapIndexConnectionTimeout = 10 * time.Second

func buildBootstrapCollector(
	ctx context.Context,
	database bootstrapDB,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collectorDeps, error) {
	instrumentedDB := &postgres.InstrumentedDB{
		Inner:       database,
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "bootstrap-index",
	}

	config, err := collector.LoadRepoSyncConfig("bootstrap-index", getenv)
	if err != nil {
		return collectorDeps{}, err
	}
	discoveryOptions, err := collector.LoadDiscoveryOptionsFromEnv(getenv)
	if err != nil {
		return collectorDeps{}, err
	}

	source := &collector.GitSource{
		Component: "bootstrap-index",
		Selector:  collector.NativeRepositorySelector{Config: config},
		Snapshotter: collector.NativeRepositorySnapshotter{
			SCIP:             collector.LoadSnapshotSCIPConfig(getenv),
			ParseWorkers:     config.ParseWorkers,
			DiscoveryOptions: discoveryOptions,
			Tracer:           tracer,
			Instruments:      instruments,
			Logger:           logger,
		},
		SnapshotWorkers:        config.SnapshotWorkers,
		LargeRepoThreshold:     config.LargeRepoThreshold,
		LargeRepoMaxConcurrent: config.LargeRepoMaxConcurrent,
		StreamBuffer:           config.StreamBuffer,
		Tracer:                 tracer,
		Instruments:            instruments,
		Logger:                 logger,
	}

	committer := postgres.NewIngestionStore(instrumentedDB)
	committer.Logger = logger
	committer.Instruments = instruments
	// bootstrap-index runs the corpus-wide deferred relationship backfill as a
	// dedicated phase, so per-commit backfill would be duplicate work
	// (#4451, § T8).
	committer.SkipRelationshipBackfill = true

	return collectorDeps{
		source:      source,
		committer:   committer,
		commitLanes: commitLaneCount(getenv),
	}, nil
}

func buildBootstrapProjector(
	ctx context.Context,
	database bootstrapDB,
	canonicalWriter projector.CanonicalWriter,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projectorDeps, error) {
	instrumentedDB := &postgres.InstrumentedDB{
		Inner:       database,
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "bootstrap-index",
	}

	projectorQueue := postgres.NewProjectorQueue(instrumentedDB, "bootstrap-index", time.Minute).
		WithClaimSourceSystem(string(scope.CollectorGit))
	// Exponential backoff + jitter (#4450): the one-shot bootstrap-index
	// projector must use the same PROJECTOR retry policy as the ingester so
	// same-instant retry failures don't reconverge on one visible_at and
	// self-reinforce into a retry storm. Without this the direct-constructed
	// queue keeps JitterFraction at its zero value (no jitter).
	projectorRetryPolicy, err := runtimecfg.LoadRetryPolicyConfig(getenv, "PROJECTOR")
	if err != nil {
		return projectorDeps{}, err
	}
	projectorQueue.RetryDelay, projectorQueue.MaxAttempts = projectorRetryPolicy.RetryDelay, projectorRetryPolicy.MaxAttempts
	projectorQueue.MaxRetryDelay = projectorRetryPolicy.MaxRetryDelay
	projectorQueue.JitterFraction = projectorRetryPolicy.JitterFraction
	reducerQueue := postgres.NewReducerQueue(instrumentedDB, "bootstrap-index", time.Minute)
	contentConfig, err := content.LoadWriterConfig(getenv)
	if err != nil {
		return projectorDeps{}, err
	}
	runtime := projector.Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter: postgres.NewContentWriter(instrumentedDB).
			WithLogger(logger).
			WithEntityBatchSize(contentConfig.EntityBatchSize),
		IntentWriter:                  reducerQueue,
		PhasePublisher:                postgres.NewGraphProjectionPhaseStateStore(instrumentedDB),
		RepairQueue:                   postgres.NewGraphProjectionPhaseRepairQueueStore(instrumentedDB),
		PackageRegistryIdentityLocker: postgres.PackageRegistryIdentityLocker{DB: instrumentedDB},
		Tracer:                        tracer,
		Instruments:                   instruments,
		Logger:                        logger,
	}

	return projectorDeps{
		workSource:        projectorQueue,
		factStore:         postgres.NewFactStore(instrumentedDB),
		runner:            runtime,
		workSink:          projectorQueue,
		heartbeater:       projectorQueue,
		heartbeatInterval: bootstrapProjectorHeartbeatInterval(projectorQueue.LeaseDuration),
	}, nil
}

// bootstrapProjectorHeartbeatInterval renews leases well before expiry while
// avoiding excessive wakeups for long lease durations.
func bootstrapProjectorHeartbeatInterval(leaseDuration time.Duration) time.Duration {
	if leaseDuration <= 0 {
		return time.Minute
	}
	interval := leaseDuration / 3
	if interval <= 0 {
		return time.Second
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}

// neo4jBatchSize reads ESHU_NEO4J_BATCH_SIZE from the environment.
// Returns 0 (use default) if unset or invalid.
func neo4jBatchSize(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv("ESHU_NEO4J_BATCH_SIZE"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// openBootstrapCanonicalWriter opens the canonical graph writer for the
// configured backend. database is the already-open Postgres handle
// bootstrap-index's main() owns; it is threaded in, not opened here, so the
// #5443 MATCHES_STATE ownership resolver reuses the process's single
// Postgres connection instead of duplicating a lifecycle -- the same pattern
// cmd/projector's openProjectorCanonicalWriter and cmd/ingester's
// openIngesterCanonicalWriter follow.
func openBootstrapCanonicalWriter(
	parent context.Context,
	database bootstrapDB,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) (projector.CanonicalWriter, io.Closer, error) {
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return nil, nil, err
	}
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(parent, getenv)
	if err != nil {
		return nil, nil, err
	}

	profileGroupStatements, err := bootstrapNeo4jProfileGroupStatements(getenv)
	if err != nil {
		_ = closeBootstrapNeo4jDriver(driver)
		return nil, nil, err
	}
	rawExecutor := bootstrapNeo4jExecutor{
		Driver:                 driver,
		DatabaseName:           cfg.DatabaseName,
		TxTimeout:              bootstrapCanonicalTransactionTimeout(graphBackend, getenv),
		ProfileGroupStatements: profileGroupStatements,
		Instruments:            instruments,
	}

	// The shared in-flight canonical permit pool (issue #4515, Lane B) is
	// constructed exactly ONCE here, per bootstrap-index run, and threaded
	// into bootstrapCanonicalExecutorForGraphBackend so every concurrent
	// canonical write this run performs — across every projector.Service
	// worker and every concurrent chunk in the entity-phase fan-out
	// (executeEntityPhaseGroupConcurrently) — draws from the same budget. See
	// newBootstrapCanonicalGate and bootstrapCanonicalExecutorForGraphBackend
	// for why the gate wraps the inner GroupExecutor layer rather than the
	// outer per-phase-group call. Default unset ceiling (both
	// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT and
	// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT) yields a nil gate, so behavior
	// is unchanged until an operator opts in.
	canonicalGate := newBootstrapCanonicalGate(getenv, instruments)

	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		rawExecutor,
		graphBackend,
		getenv,
		tracer,
		instruments,
		canonicalGate,
	)
	if err != nil {
		_ = closeBootstrapNeo4jDriver(driver)
		return nil, nil, err
	}

	writer := sourcecypher.NewCanonicalNodeWriter(
		executor,
		neo4jBatchSize(getenv),
		instruments,
	).WithTracer(tracer).WithTerraformStateOwnershipResolver(
		bootstrapTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(postgres.PostgresTerraformBackendQuery{DB: database})},
	).WithTerraformStateConfigMatchResolver(
		bootstrapTerraformStateConfigMatchResolver{driver: driver, databaseName: cfg.DatabaseName},
	).WithKustomizeOverlayResolver(
		bootstrapKustomizeOverlayResolver{driver: driver, databaseName: cfg.DatabaseName},
	)
	labelBatchSizes := map[string]int(nil)
	orderedLabels := []string(nil)
	fileBatchSize := 0
	entityBatchSize := 0
	batchedEntityContainment := false
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		fileBatchSize, err = nornicDBPositiveIntEnv(getenv, nornicDBFileBatchSizeEnv, defaultNornicDBFileBatchSize)
		if err != nil {
			_ = closeBootstrapNeo4jDriver(driver)
			return nil, nil, err
		}
		entityBatchSize, err = nornicDBPositiveIntEnv(getenv, nornicDBEntityBatchSizeEnv, defaultNornicDBEntityBatchSize)
		if err != nil {
			_ = closeBootstrapNeo4jDriver(driver)
			return nil, nil, err
		}
		labelBatchSizes, err = nornicDBEntityLabelBatchSizes(getenv, entityBatchSize)
		if err != nil {
			_ = closeBootstrapNeo4jDriver(driver)
			return nil, nil, err
		}
		batchedEntityContainment, err = nornicDBBatchedEntityContainmentEnabled(getenv)
		if err != nil {
			_ = closeBootstrapNeo4jDriver(driver)
			return nil, nil, err
		}
		orderedLabels = orderedBootstrapEntityBatchLabels(labelBatchSizes)
	}
	writer = configureBootstrapCanonicalWriter(writer, bootstrapCanonicalWriterConfig{
		GraphBackend:                      graphBackend,
		FileBatchSize:                     fileBatchSize,
		EntityBatchSize:                   entityBatchSize,
		EntityLabelBatchSizes:             labelBatchSizes,
		DisableNornicDBBatchedContainment: !batchedEntityContainment,
		OrderedEntityLabelBatchSizeLabels: orderedLabels,
	})

	return writer, bootstrapNeo4jDriverCloser{Driver: driver}, nil
}

type bootstrapNeo4jExecutor struct {
	Driver                 neo4jdriver.DriverWithContext
	DatabaseName           string
	TxTimeout              time.Duration
	ProfileGroupStatements bool
	Instruments            *telemetry.Instruments
}

func (e bootstrapNeo4jExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if e.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	if len(stmts) == 0 {
		return nil
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	rawCounts, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		counts := make([]sourcecypher.StatementRetractionCounts, 0, len(stmts))
		err := sourcecypher.ExecuteProfiledStatementGroup(ctx, stmts, func(ctx context.Context, stmt sourcecypher.Statement) error {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return runErr
			}
			summary, consumeErr := result.Consume(ctx)
			if consumeErr != nil {
				return consumeErr
			}
			counts = append(counts, bootstrapStatementRetractionCounts(stmt, summary))
			return nil
		}, e.ProfileGroupStatements, nil)
		if err != nil {
			return nil, err
		}
		return counts, nil
	}, e.transactionConfigurers()...)
	if err != nil {
		return err
	}
	if counts, ok := rawCounts.([]sourcecypher.StatementRetractionCounts); ok {
		sourcecypher.RecordReconciliationDriftRetractionCounts(ctx, e.Instruments, counts)
	}
	return nil
}

func (e bootstrapNeo4jExecutor) Execute(ctx context.Context, statement sourcecypher.Statement) error {
	if e.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, statement.Cypher, statement.Parameters, e.transactionConfigurers()...)
	if err != nil {
		return err
	}
	summary, err := result.Consume(ctx)
	if err == nil {
		sourcecypher.RecordReconciliationDriftRetractions(
			ctx,
			e.Instruments,
			statement,
			int64(summary.Counters().NodesDeleted()),
			int64(summary.Counters().RelationshipsDeleted()),
		)
	}
	return err
}

func bootstrapStatementRetractionCounts(
	statement sourcecypher.Statement,
	summary neo4jdriver.ResultSummary,
) sourcecypher.StatementRetractionCounts {
	counters := summary.Counters()
	return sourcecypher.StatementRetractionCounts{
		Statement:            statement,
		NodesDeleted:         int64(counters.NodesDeleted()),
		RelationshipsDeleted: int64(counters.RelationshipsDeleted()),
	}
}

func (e bootstrapNeo4jExecutor) transactionConfigurers() []func(*neo4jdriver.TransactionConfig) {
	if e.TxTimeout <= 0 {
		return nil
	}
	return []func(*neo4jdriver.TransactionConfig){neo4jdriver.WithTxTimeout(e.TxTimeout)}
}

func bootstrapCanonicalTransactionTimeout(graphBackend runtimecfg.GraphBackend, getenv func(string) string) time.Duration {
	if graphBackend != runtimecfg.GraphBackendNornicDB {
		return 0
	}
	return nornicDBCanonicalWriteTimeout(getenv)
}

func bootstrapNeo4jProfileGroupStatements(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv("ESHU_NEO4J_PROFILE_GROUP_STATEMENTS"))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse ESHU_NEO4J_PROFILE_GROUP_STATEMENTS=%q: %w", raw, err)
	}
	return enabled, nil
}

type bootstrapNeo4jDriverCloser struct {
	Driver neo4jdriver.DriverWithContext
}

func (c bootstrapNeo4jDriverCloser) Close() error {
	return closeBootstrapNeo4jDriver(c.Driver)
}

func closeBootstrapNeo4jDriver(driver neo4jdriver.DriverWithContext) error {
	if driver == nil {
		return nil
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), bootstrapIndexConnectionTimeout)
	defer cancel()
	return driver.Close(closeCtx)
}
