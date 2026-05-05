package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/graph"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type bootstrapExecutor interface {
	postgres.Executor
}

type bootstrapDB interface {
	bootstrapExecutor
	Close() error
}

type neo4jDeps struct {
	executor graph.CypherExecutor
	close    func() error
}

type openBootstrapDBFn func(context.Context, func(string) string) (bootstrapDB, error)
type applyPostgresFn func(context.Context, bootstrapExecutor) error
type openNeo4jFn func(context.Context, func(string) string) (neo4jDeps, error)
type applyNeo4jFn func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-bootstrap-data-plane"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap("eshu-bootstrap-data-plane")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("bootstrap-data-plane bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := newLogger(bootstrap, os.Stderr)
	if err := run(
		context.Background(),
		os.Getenv,
		logger,
		openBootstrapDB,
		func(ctx context.Context, exec bootstrapExecutor) error {
			return postgres.ApplyBootstrap(ctx, exec)
		},
		openNeo4j,
		graph.EnsureSchemaWithBackend,
	); err != nil {
		logger.Error("bootstrap-data-plane failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

func newLogger(bootstrap telemetry.Bootstrap, writer io.Writer) *slog.Logger {
	return telemetry.NewLoggerWithWriter(bootstrap, "bootstrap", "bootstrap-data-plane", writer)
}

func run(
	ctx context.Context,
	getenv func(string) string,
	logger *slog.Logger,
	openDBFn openBootstrapDBFn,
	applyPgFn applyPostgresFn,
	openNeo4jFn openNeo4jFn,
	applyNeo4jFn applyNeo4jFn,
) (err error) {
	logger.Info("starting data-plane schema migration", telemetry.EventAttr("bootstrap.schema.started"))

	// Postgres schema
	db, err := openDBFn(ctx, getenv)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	if err = applyPgFn(ctx, db); err != nil {
		return err
	}
	logger.Info("postgres schema applied", telemetry.EventAttr("bootstrap.postgres.applied"))

	// Neo4j schema
	nd, err := openNeo4jFn(ctx, getenv)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := nd.close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	backend, err := schemaBackendFromEnv(getenv)
	if err != nil {
		return err
	}
	if err = applyNeo4jFn(ctx, nd.executor, logger, backend); err != nil {
		return err
	}
	logger.Info("graph schema applied", telemetry.EventAttr("bootstrap.graph.applied"), "graph_backend", backend)

	return nil
}

func schemaBackendFromEnv(getenv func(string) string) (graph.SchemaBackend, error) {
	backend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return "", err
	}
	switch backend {
	case runtimecfg.GraphBackendNeo4j:
		return graph.SchemaBackendNeo4j, nil
	case runtimecfg.GraphBackendNornicDB:
		return graph.SchemaBackendNornicDB, nil
	default:
		return "", errors.New("unsupported graph backend for schema")
	}
}

func openBootstrapDB(ctx context.Context, getenv func(string) string) (bootstrapDB, error) {
	return runtimecfg.OpenPostgres(ctx, getenv)
}

const neo4jCloseTimeout = 10 * time.Second

func openNeo4j(ctx context.Context, getenv func(string) string) (neo4jDeps, error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return neo4jDeps{}, err
	}

	return neo4jDeps{
		executor: &neo4jSchemaExecutor{
			driver:       driver,
			databaseName: cfg.DatabaseName,
		},
		close: func() error {
			closeCtx, cancel := context.WithTimeout(context.Background(), neo4jCloseTimeout)
			defer cancel()
			return driver.Close(closeCtx)
		},
	}, nil
}

// neo4jSchemaExecutor adapts the Neo4j driver to the graph.CypherExecutor
// interface for schema DDL execution.
type neo4jSchemaExecutor struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

func (e *neo4jSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}
