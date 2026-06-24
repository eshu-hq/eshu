package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	graphSchemaStatementTimeoutEnv     = "ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT"
	defaultGraphSchemaStatementTimeout = 2 * time.Minute
)

type ensureBootstrapGraphSchemaFn func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error

type openBootstrapGraphSchemaFn func(context.Context, func(string) string) (graph.SchemaBackend, graph.CypherExecutor, func() error, error)

func ensureBootstrapGraphSchema(
	ctx context.Context,
	db bootstrapDB,
	getenv func(string) string,
	logger *slog.Logger,
) error {
	return ensureBootstrapGraphSchemaWithOpener(ctx, db, getenv, logger, openBootstrapGraphSchema)
}

func ensureBootstrapGraphSchemaWithOpener(
	ctx context.Context,
	db bootstrapDB,
	getenv func(string) string,
	logger *slog.Logger,
	openSchemaFn openBootstrapGraphSchemaFn,
) (err error) {
	if _, err := graphschemacompat.RequireCompatibleForRuntime(ctx, db, getenv); err == nil {
		return nil
	} else if !errors.Is(err, graphschemacompat.ErrMissingMarker) {
		return err
	}

	backend, executor, closeFn, err := openSchemaFn(ctx, getenv)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer func() {
			err = errors.Join(err, closeFn())
		}()
	}

	if logger != nil {
		logger.InfoContext(
			ctx, "graph schema marker missing; applying bootstrap-index graph schema",
			slog.String("graph_backend", string(backend)),
		)
	}
	if err = graph.EnsureSchemaWithBackendStrict(ctx, executor, logger, backend); err != nil {
		return fmt.Errorf("bootstrap-index graph schema apply: %w", err)
	}

	app, err := graph.SchemaApplicationForBackend(backend)
	if err != nil {
		return err
	}
	if err = graphschemacompat.MarkApplied(ctx, db, app); err != nil {
		return err
	}
	if logger != nil {
		logger.InfoContext(
			ctx, "bootstrap-index graph schema marked applied",
			slog.String("graph_backend", string(backend)),
			slog.String("schema_fingerprint", app.Fingerprint),
			slog.Int("statement_count", app.StatementCount),
		)
	}
	return nil
}

func openBootstrapGraphSchema(
	ctx context.Context,
	getenv func(string) string,
) (graph.SchemaBackend, graph.CypherExecutor, func() error, error) {
	backend, err := bootstrapGraphSchemaBackend(getenv)
	if err != nil {
		return "", nil, nil, err
	}
	timeout, err := bootstrapGraphSchemaStatementTimeout(getenv)
	if err != nil {
		return "", nil, nil, err
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return "", nil, nil, err
	}
	executor := bootstrapNeo4jExecutor{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
		TxTimeout:    timeout,
	}
	return backend, executor, func() error {
		return closeBootstrapNeo4jDriver(driver)
	}, nil
}

func bootstrapGraphSchemaBackend(getenv func(string) string) (graph.SchemaBackend, error) {
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
		return "", fmt.Errorf("unsupported graph backend for schema %q", backend)
	}
}

func bootstrapGraphSchemaStatementTimeout(getenv func(string) string) (time.Duration, error) {
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

func (e bootstrapNeo4jExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return e.Execute(ctx, sourcecypher.Statement{
		Cypher:     stmt.Cypher,
		Parameters: stmt.Parameters,
	})
}
