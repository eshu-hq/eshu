package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	QueryContext(context.Context, string, ...any) (postgres.Rows, error)
}

type bootstrapDB interface {
	bootstrapExecutor
	Close() error
}

type neo4jDeps struct {
	executor  graph.CypherExecutor
	inspector graphSchemaInspector
	close     func() error
}

type openBootstrapDBFn func(context.Context, func(string) string) (bootstrapDB, error)
type applyPostgresFn func(context.Context, bootstrapExecutor) error
type openNeo4jFn func(context.Context, func(string) string) (neo4jDeps, error)
type applyNeo4jFn func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error

const (
	graphSchemaStatementTimeoutEnv     = "ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT"
	defaultGraphSchemaStatementTimeout = 2 * time.Minute
)

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
		graph.EnsureSchemaWithBackendStrict,
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

	backend, err := schemaBackendFromEnv(getenv)
	if err != nil {
		return err
	}
	statementTimeout, err := graphSchemaStatementTimeout(getenv)
	if err != nil {
		return err
	}

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

	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		return err
	}
	applied, err := graphSchemaAlreadyApplied(ctx, db, backend, fingerprint)
	if err != nil {
		return err
	}
	if applied {
		logger.Info("graph schema already applied",
			telemetry.EventAttr("bootstrap.graph.skipped"),
			"graph_backend", backend,
			"schema_fingerprint", fingerprint,
			"statement_count", statementCount,
		)
		return nil
	}

	nd, err := openNeo4jFn(ctx, getenv)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := nd.close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	graphExecutor := graph.CypherExecutor(&statementTimeoutExecutor{
		executor: nd.executor,
		timeout:  statementTimeout,
	})
	if graphSchemaAdoptionEnabled(getenv) {
		adopted, err := adoptExistingGraphSchema(ctx, db, nd.inspector, logger, backend, fingerprint, statementCount)
		if err != nil {
			return err
		}
		if adopted {
			return nil
		}
	}
	if err = applyNeo4jFn(ctx, graphExecutor, logger, backend); err != nil {
		return err
	}
	if err = markGraphSchemaApplied(ctx, db, backend, fingerprint, statementCount); err != nil {
		return err
	}
	logger.Info("graph schema applied", telemetry.EventAttr("bootstrap.graph.applied"), "graph_backend", backend)

	return nil
}

const graphSchemaAppliedQuery = `
SELECT EXISTS (
    SELECT 1
    FROM graph_schema_applications
    WHERE backend = $1
      AND schema_fingerprint = $2
)
`

const markGraphSchemaAppliedQuery = `
INSERT INTO graph_schema_applications (
    backend,
    schema_fingerprint,
    statement_count,
    applied_at
) VALUES (
    $1, $2, $3, NOW()
)
ON CONFLICT (backend, schema_fingerprint) DO UPDATE
SET statement_count = EXCLUDED.statement_count,
    applied_at = EXCLUDED.applied_at
`

func graphSchemaFingerprint(backend graph.SchemaBackend) (string, int, error) {
	statements, err := graph.SchemaStatementsForBackend(backend)
	if err != nil {
		return "", 0, err
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(string(backend)))
	_, _ = hasher.Write([]byte{0})
	for _, statement := range statements {
		_, _ = hasher.Write([]byte(statement))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), len(statements), nil
}

func graphSchemaAlreadyApplied(
	ctx context.Context,
	db bootstrapExecutor,
	backend graph.SchemaBackend,
	fingerprint string,
) (bool, error) {
	rows, err := db.QueryContext(ctx, graphSchemaAppliedQuery, string(backend), fingerprint)
	if err != nil {
		return false, fmt.Errorf("query graph schema marker: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("query graph schema marker: %w", err)
		}
		return false, nil
	}
	var applied bool
	if err := rows.Scan(&applied); err != nil {
		return false, fmt.Errorf("scan graph schema marker: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("query graph schema marker: %w", err)
	}
	return applied, nil
}

func markGraphSchemaApplied(
	ctx context.Context,
	db bootstrapExecutor,
	backend graph.SchemaBackend,
	fingerprint string,
	statementCount int,
) error {
	if _, err := db.ExecContext(ctx, markGraphSchemaAppliedQuery, string(backend), fingerprint, statementCount); err != nil {
		return fmt.Errorf("mark graph schema applied: %w", err)
	}
	return nil
}

func graphSchemaStatementTimeout(getenv func(string) string) (time.Duration, error) {
	raw := getenv(graphSchemaStatementTimeoutEnv)
	if raw == "" {
		return defaultGraphSchemaStatementTimeout, nil
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", graphSchemaStatementTimeoutEnv, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", graphSchemaStatementTimeoutEnv)
	}
	return timeout, nil
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
	db, err := runtimecfg.OpenPostgres(ctx, getenv)
	if err != nil {
		return nil, err
	}
	return bootstrapSQLDB{SQLDB: postgres.SQLDB{DB: db}}, nil
}

type bootstrapSQLDB struct {
	postgres.SQLDB
}

func (db bootstrapSQLDB) Close() error {
	return db.DB.Close()
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
		inspector: &neo4jSchemaExecutor{
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

type statementTimeoutExecutor struct {
	executor graph.CypherExecutor
	timeout  time.Duration
}

func (e *statementTimeoutExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	statementCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	err := e.executor.ExecuteCypher(statementCtx, stmt)
	if err != nil {
		if errors.Is(statementCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("graph schema statement exceeded %s: %w", e.timeout, context.DeadlineExceeded)
		}
		return err
	}
	if errors.Is(statementCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("graph schema statement exceeded %s: %w", e.timeout, context.DeadlineExceeded)
	}
	return nil
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
