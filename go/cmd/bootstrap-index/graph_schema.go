// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	graphSchemaStatementTimeoutEnv     = "ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT"
	defaultGraphSchemaStatementTimeout = 2 * time.Minute
)

type ensureBootstrapGraphSchemaFn func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error

type openBootstrapGraphSchemaFn func(context.Context, func(string) string) (bootstrapGraphSchema, error)

// graphRowReader runs one Cypher statement in its own auto-commit transaction
// and returns its rows.
type graphRowReader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// bootstrapGraphSchema is what the marker-missing path needs from the graph:
// the backend, a DDL executor, a row reader for the Module import_key check
// (required on Neo4j), and a close func (nil when there is nothing to close).
type bootstrapGraphSchema struct {
	backend  graph.SchemaBackend
	executor graph.CypherExecutor
	reader   graphRowReader
	close    func() error
}

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

	handle, err := openSchemaFn(ctx, getenv)
	if err != nil {
		return err
	}
	if handle.close != nil {
		defer func() {
			err = errors.Join(err, handle.close())
		}()
	}
	backend := handle.backend

	if logger != nil {
		logger.InfoContext(
			ctx, "graph schema marker missing; applying bootstrap-index graph schema",
			slog.String("graph_backend", string(backend)),
		)
	}
	if err = graph.EnsureSchemaWithBackendStrict(ctx, handle.executor, logger, backend); err != nil {
		return fmt.Errorf("bootstrap-index graph schema apply: %w", err)
	}

	if backend == graph.SchemaBackendNeo4j {
		if err = refuseUnmigratedModules(ctx, handle.reader); err != nil {
			return err
		}
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

// moduleImportKeyRunbook names the deployment procedure for a graph whose
// canonical Module nodes still need the #8168 migration.
const moduleImportKeyRunbook = "docs/public/deployment/service-runtimes-bootstrap.md#deployment-contract"

// refuseUnmigratedModules keeps bootstrap-index from writing a Module=import_key
// schema marker over a graph an earlier release populated. The marker admits
// writers that MERGE canonical Module nodes on import_key; over unkeyed
// (name, lang) nodes they would mint a second node per module, the duplicates
// #8168 removes. Only eshu-bootstrap-data-plane migrates those nodes, so this
// path refuses and names it rather than run a destructive migration from a
// one-shot seeder. It runs the same statement as the migration's verify step;
// on a fresh graph both counts are zero and it costs one Module label scan.
// It runs on Neo4j only, the scope of the migration.
func refuseUnmigratedModules(ctx context.Context, reader graphRowReader) error {
	if reader == nil {
		return errors.New("bootstrap-index graph schema: no graph reader for the Module import_key check; refusing to write the schema marker")
	}
	rows, err := reader.Run(ctx, sourcecypher.CanonicalModuleImportKeyPendingCypher, nil)
	if err != nil {
		return fmt.Errorf("bootstrap-index Module import_key check: %w", err)
	}
	unkeyed, adopted, err := sourcecypher.ParseCanonicalModuleImportKeyPending(rows)
	if err != nil {
		return fmt.Errorf("bootstrap-index Module import_key check: %w; refusing to write the schema marker", err)
	}
	if unkeyed == 0 && adopted == 0 {
		return nil
	}
	return fmt.Errorf("bootstrap-index will not write the graph schema marker: the graph holds %d unkeyed canonical Module nodes "+
		"and %d IMPORTS edges on semantic Modules written by a release before #8168; stop every writer from that release, "+
		"run eshu-bootstrap-data-plane (it migrates them), then rerun bootstrap-index (%s)",
		unkeyed, adopted, moduleImportKeyRunbook)
}

func openBootstrapGraphSchema(
	ctx context.Context,
	getenv func(string) string,
) (bootstrapGraphSchema, error) {
	backend, err := bootstrapGraphSchemaBackend(getenv)
	if err != nil {
		return bootstrapGraphSchema{}, err
	}
	timeout, err := bootstrapGraphSchemaStatementTimeout(getenv)
	if err != nil {
		return bootstrapGraphSchema{}, err
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return bootstrapGraphSchema{}, err
	}
	executor := bootstrapNeo4jExecutor{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
		TxTimeout:    timeout,
	}
	return bootstrapGraphSchema{
		backend:  backend,
		executor: executor,
		reader:   executor,
		close: func() error {
			return closeBootstrapNeo4jDriver(driver)
		},
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

// Run implements graphRowReader: one statement in an auto-commit transaction
// bounded by the executor's transaction timeout, its rows as maps. It opens a
// write-mode session, like the DDL, so a clustered deployment answers from the
// leader that just applied the schema rather than from a lagging follower.
func (e bootstrapNeo4jExecutor) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if e.Driver == nil {
		return nil, fmt.Errorf("neo4j driver is required")
	}
	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()
	result, err := session.Run(ctx, cypher, params, e.transactionConfigurers()...)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}
