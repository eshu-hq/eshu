package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	projectorConnectionTimeout           = 10 * time.Second
	defaultNornicDBCanonicalWriteTimeout = 30 * time.Second
	canonicalWriteTimeoutEnv             = "ESHU_CANONICAL_WRITE_TIMEOUT"
)

func buildProjectorService(
	database postgres.SQLDB,
	canonicalWriter projector.CanonicalWriter,
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
	n := runtime.NumCPU()
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
	canonicalWriter projector.CanonicalWriter,
	intentWriter projector.ReducerIntentWriter,
	retryInjector projector.RetryInjector,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.Runtime, error) {
	contentConfig, err := content.LoadWriterConfig(getenv)
	if err != nil {
		return projector.Runtime{}, err
	}

	return projector.Runtime{
		CanonicalWriter:               canonicalWriter,
		ContentWriter:                 postgres.NewContentWriter(database).WithEntityBatchSize(contentConfig.EntityBatchSize),
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

	rawExecutor := projectorNeo4jExecutor{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
		Instruments:  instruments,
	}

	return sourcecypher.NewCanonicalNodeWriter(
			projectorCanonicalExecutorForGraphBackend(rawExecutor, graphBackend, getenv, tracer, instruments),
			neo4jBatchSize(getenv),
			instruments,
		).WithTracer(tracer),
		projectorNeo4jDriverCloser{Driver: driver},
		nil
}

func projectorCanonicalExecutorForGraphBackend(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) sourcecypher.Executor {
	instrumentedExecutor := &sourcecypher.InstrumentedExecutor{
		Inner: &sourcecypher.RetryingExecutor{
			Inner:       rawExecutor,
			MaxRetries:  3,
			Instruments: instruments,
		},
		Tracer:      tracer,
		Instruments: instruments,
	}
	var outer sourcecypher.Executor = instrumentedExecutor
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		outer = sourcecypher.TimeoutExecutor{
			Inner:       instrumentedExecutor,
			Timeout:     projectorNornicDBCanonicalWriteTimeout(getenv),
			TimeoutHint: canonicalWriteTimeoutEnv,
		}
	}
	// Bound concurrent canonical writes so a slow graph backend slows intake
	// instead of dead-lettering recoverable projector work (issue #3560). The
	// wrapper sits outside retry/timeout so one permit covers a whole write
	// attempt; a non-positive ESHU_GRAPH_WRITE_MAX_IN_FLIGHT leaves it a
	// passthrough.
	return graphbackpressure.Wrap(
		outer,
		graphbackpressure.MaxInFlight(getenv),
		instruments,
	)
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

type projectorNeo4jExecutor struct {
	Driver       neo4jdriver.DriverWithContext
	DatabaseName string
	Instruments  *telemetry.Instruments
}

func (e projectorNeo4jExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
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
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			summary, consumeErr := result.Consume(ctx)
			if consumeErr != nil {
				return nil, consumeErr
			}
			counts = append(counts, statementRetractionCounts(stmt, summary))
		}
		return counts, nil
	})
	if err != nil {
		return err
	}
	if counts, ok := rawCounts.([]sourcecypher.StatementRetractionCounts); ok {
		sourcecypher.RecordReconciliationDriftRetractionCounts(ctx, e.Instruments, counts)
	}
	return nil
}

func (e projectorNeo4jExecutor) Execute(ctx context.Context, statement sourcecypher.Statement) error {
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

	result, err := session.Run(ctx, statement.Cypher, statement.Parameters)
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

func statementRetractionCounts(
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

type projectorNeo4jDriverCloser struct {
	Driver neo4jdriver.DriverWithContext
}

func (c projectorNeo4jDriverCloser) Close() error {
	return closeProjectorNeo4jDriver(c.Driver)
}

func closeProjectorNeo4jDriver(driver neo4jdriver.DriverWithContext) error {
	if driver == nil {
		return nil
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), projectorConnectionTimeout)
	defer cancel()
	return driver.Close(closeCtx)
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
