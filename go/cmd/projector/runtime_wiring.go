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

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	projectorConnectionTimeout           = 10 * time.Second
	defaultNornicDBCanonicalWriteTimeout = storagenornicdb.DefaultCanonicalWriteTimeout
	canonicalWriteTimeoutEnv             = "ESHU_CANONICAL_WRITE_TIMEOUT"
)

// buildCrossplaneRedriveSweeper constructs the Crossplane cross-scope
// SATISFIED_BY re-drive sweeper (issue #5476) shared by two callers: the live
// post-Ack hook wired into projectorQueue.CrossplaneRedrive in
// buildProjectorService, and the periodic catch-up loop
// (runCrossplaneRedriveCatchUpLoop) started from main.go. Both must be wired
// -- the live hook alone cannot recover a sweep that failed partway through
// its fan-out or whose process crashed mid-sweep (see
// postgres.CrossplaneSatisfiedByRedriveSweeper's doc comment).
func buildCrossplaneRedriveSweeper(
	database postgres.SQLDB,
	reducerQueue postgres.ReducerQueue,
	instruments *telemetry.Instruments,
) postgres.CrossplaneSatisfiedByRedriveSweeper {
	return postgres.CrossplaneSatisfiedByRedriveSweeper{
		DB:          postgres.SQLQueryer(database),
		State:       postgres.NewCrossplaneRedriveStateStore(database),
		Replayer:    reducerQueue,
		Owner:       "projector",
		Instruments: instruments,
	}
}

func buildProjectorService(
	database postgres.SQLDB,
	canonicalWriter runtime.CanonicalWriter,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.Service, error) {
	projectorQueue := postgres.NewProjectorQueue(database, "projector", time.Minute)
	reducerQueue := postgres.NewReducerQueue(database, "projector", time.Minute)
	retryInjector, err := loadProjectorRetryInjector(getenv)
	if err != nil {
		return projector.Service{}, err
	}
	retryPolicy, err := loadProjectorRetryPolicy(getenv)
	if err != nil {
		return projector.Service{}, err
	}
	projectorQueue.RetryDelay = retryPolicy.RetryDelay
	projectorQueue.MaxAttempts = retryPolicy.MaxAttempts
	// Exponential backoff + jitter (#4450): without these, same-instant
	// failures reconverge on one visible_at and self-reinforce into a retry
	// storm. See runtime.RetryPolicyConfig's doc comment for the formula.
	projectorQueue.MaxRetryDelay = retryPolicy.MaxRetryDelay
	projectorQueue.JitterFraction = retryPolicy.JitterFraction
	projectorQueue.Instruments = instruments
	// Closes the Crossplane cross-scope SATISFIED_BY XRD-lag false-negative
	// window (issue #5476): after Ack activates a generation, re-drive any
	// OTHER scope's unresolved Claims matching a newly-active XRD. See
	// postgres.CrossplaneSatisfiedByRedriveSweeper's doc comment. This live
	// hook alone cannot recover from a transient error or crash mid-sweep;
	// runCrossplaneRedriveCatchUpLoop (wired in main.go from the SAME
	// buildCrossplaneRedriveSweeper helper) is the required recovery path --
	// see that function's doc comment (issue #5476 P1-a).
	projectorQueue.CrossplaneRedrive = buildCrossplaneRedriveSweeper(database, reducerQueue, instruments)

	runner, err := buildProjectorRuntime(database, canonicalWriter, reducerQueue, retryInjector, getenv, tracer, instruments, logger)
	if err != nil {
		return projector.Service{}, err
	}

	return projector.Service{
		PollInterval:      time.Second,
		WorkSource:        projectorQueue,
		FactStore:         postgres.NewFactStore(database),
		Runner:            runner,
		WorkSink:          projectorQueue,
		Heartbeater:       projectorQueue,
		HeartbeatInterval: projectorHeartbeatInterval(projectorQueue.LeaseDuration),
		Tracer:            tracer,
		Instruments:       instruments,
		Logger:            logger,
		Workers:           projectorWorkerCount(getenv),
	}, nil
}

func projectorHeartbeatInterval(leaseDuration time.Duration) time.Duration {
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

func projectorWorkerCount(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_PROJECTOR_WORKERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := cpubudget.UsableCPUs()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

func buildProjectorRuntime(
	database postgres.SQLDB,
	canonicalWriter runtime.CanonicalWriter,
	intentWriter runtime.ReducerIntentWriter,
	retryInjector failure.RetryInjector,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (runtime.Runtime, error) {
	contentConfig, err := content.LoadWriterConfig(getenv)
	if err != nil {
		return runtime.Runtime{}, err
	}

	return runtime.Runtime{
		CanonicalWriter:               canonicalWriter,
		ContentWriter:                 postgres.NewContentWriter(database).WithInstruments(instruments).WithEntityBatchSize(contentConfig.EntityBatchSize),
		IntentWriter:                  intentWriter,
		PhasePublisher:                postgres.NewGraphProjectionPhaseStateStore(database),
		RepairQueue:                   postgres.NewGraphProjectionPhaseRepairQueueStore(database),
		RetryInjector:                 retryInjector,
		PackageRegistryIdentityLocker: postgres.PackageRegistryIdentityLocker{DB: database},
		Tracer:                        tracer,
		Instruments:                   instruments,
		Logger:                        logger,
	}, nil
}

func openProjectorCanonicalWriter(
	parent context.Context,
	database postgres.SQLDB,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) (runtime.CanonicalWriter, io.Closer, error) {
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return nil, nil, err
	}
	// Differential capture (#6782 slice 3) opens before any datastore so a
	// half-configured capture fails closed with nothing to leak: the flag
	// without a directory errors here instead of running uncaptured.
	captureSession, err := capture.Open(getenv, "projector")
	if err != nil {
		return nil, nil, fmt.Errorf("open differential capture: %w", err)
	}
	nornicDBConfig := projectorNornicDBConfig{}
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		nornicDBConfig, err = loadProjectorNornicDBConfig(getenv)
		if err != nil {
			return nil, nil, err
		}
	}
	// Re-checks the applied graph schema marker while this process runs, so a
	// schema application recorded underneath an already-started projector stops
	// its canonical writes instead of only refusing the next one to start.
	schemaFence, err := graphschemacompat.NewWriteFenceForRuntime(postgres.SQLQueryer(database), getenv)
	if err != nil {
		return nil, nil, err
	}
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(parent, getenv)
	if err != nil {
		return nil, nil, err
	}

	warnUnboundedNeo4jWriteTimeout(slog.Default(), graphBackend, getenv)
	rawExecutor := projectorNeo4jExecutor{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
		TxTimeout:    projectorCanonicalTransactionTimeout(graphBackend, getenv),
		Instruments:  instruments,
	}
	if nornicDBConfig.GroupedWritesRequested {
		slog.Warn("NornicDB canonical grouped writes requested; committing per dependency phase — whole-materialization atomic is unsupported on NornicDB (#4027)",
			"graph_backend", string(graphBackend),
			"env_var", projectorNornicDBCanonicalGroupedWritesEnv)
	}
	executor := projectorCanonicalExecutorForGraphBackend(
		rawExecutor,
		graphBackend,
		nornicDBConfig,
		getenv,
		tracer,
		instruments,
		captureSession,
	)
	writer := sourcecypher.NewCanonicalNodeWriter(
		executor,
		neo4jBatchSize(getenv),
		instruments,
	).WithTracer(tracer).WithTerraformStateOwnershipResolver(
		projectorTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(postgres.PostgresTerraformBackendQuery{DB: database})},
	).WithTerraformStateConfigMatchResolver(
		projectorTerraformStateConfigMatchResolver{driver: driver, databaseName: cfg.DatabaseName},
	).WithKustomizeOverlayResolver(
		projectorKustomizeOverlayResolver{driver: driver, databaseName: cfg.DatabaseName},
	).WithSchemaWriteFence(schemaFence.Check)
	writer = configureProjectorCanonicalWriter(writer, graphBackend, nornicDBConfig)

	return writer,
		projectorNeo4jDriverCloser{Driver: driver, captureSession: captureSession},
		nil
}

func projectorNornicDBCanonicalWriteTimeout(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(canonicalWriteTimeoutEnv))
	if raw == "" {
		return defaultNornicDBCanonicalWriteTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultNornicDBCanonicalWriteTimeout
	}
	return parsed
}

// projectorCanonicalTransactionTimeout returns the server-side transaction
// timeout for projector graph writes. NornicDB keeps its
// ESHU_CANONICAL_WRITE_TIMEOUT default; Neo4j applies the variable only when
// it is explicitly configured.
func projectorCanonicalTransactionTimeout(
	graphBackend runtimecfg.GraphBackend,
	getenv func(string) string,
) time.Duration {
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		return projectorNornicDBCanonicalWriteTimeout(getenv)
	}
	return neo4jCanonicalWriteTimeout(getenv)
}

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
