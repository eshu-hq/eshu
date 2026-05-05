package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	projectorConnectionTimeout = 10 * time.Second
)

func buildProjectorService(
	database postgres.SQLDB,
	canonicalWriter projector.CanonicalWriter,
	getenv func(string) string,
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

	runner, err := buildProjectorRuntime(database, canonicalWriter, reducerQueue, retryInjector, getenv)
	if err != nil {
		return projector.Service{}, err
	}

	return projector.Service{
		PollInterval: time.Second,
		WorkSource:   projectorQueue,
		FactStore:    postgres.NewFactStore(database),
		Runner:       runner,
		WorkSink:     projectorQueue,
	}, nil
}

func buildProjectorRuntime(
	database postgres.SQLDB,
	canonicalWriter projector.CanonicalWriter,
	intentWriter projector.ReducerIntentWriter,
	retryInjector projector.RetryInjector,
	getenv func(string) string,
) (projector.Runtime, error) {
	contentConfig, err := content.LoadWriterConfig(getenv)
	if err != nil {
		return projector.Runtime{}, err
	}

	return projector.Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   postgres.NewContentWriter(database).WithEntityBatchSize(contentConfig.EntityBatchSize),
		IntentWriter:    intentWriter,
		PhasePublisher:  postgres.NewGraphProjectionPhaseStateStore(database),
		RepairQueue:     postgres.NewGraphProjectionPhaseRepairQueueStore(database),
		RetryInjector:   retryInjector,
	}, nil
}

func openProjectorCanonicalWriter(
	parent context.Context,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) (projector.CanonicalWriter, io.Closer, error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(parent, getenv)
	if err != nil {
		return nil, nil, err
	}

	rawExecutor := projectorNeo4jExecutor{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
	}

	instrumentedExecutor := &sourcecypher.InstrumentedExecutor{
		Inner:       rawExecutor,
		Tracer:      tracer,
		Instruments: instruments,
	}

	return sourcecypher.NewCanonicalNodeWriter(instrumentedExecutor, neo4jBatchSize(getenv), instruments),
		projectorNeo4jDriverCloser{Driver: driver},
		nil
}

type projectorNeo4jExecutor struct {
	Driver       neo4jdriver.DriverWithContext
	DatabaseName string
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

	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
		}
		return nil, nil
	})
	return err
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
	_, err = result.Consume(ctx)
	return err
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
