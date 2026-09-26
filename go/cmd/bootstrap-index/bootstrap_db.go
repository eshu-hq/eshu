// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/trace"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// bootstrapSQLDB wraps a *sql.DB so it satisfies both bootstrapDB (Close) and
// db.ExecQueryer (QueryContext returns db.Rows, not *sql.Rows).
type bootstrapSQLDB struct {
	postgres.SQLDB
	raw *sql.DB
}

func (b *bootstrapSQLDB) Close() error { return b.raw.Close() }

var _ secretlines.Conner = (*bootstrapSQLDB)(nil)

// Conn pins one connection for the secret-lines bulk-load run lock
// (secretlines.Conner). The connection keeps its session for the whole run;
// database/sql never closes an in-use connection for ConnMaxLifetime.
func (b *bootstrapSQLDB) Conn(ctx context.Context) (*sql.Conn, error) { return b.raw.Conn(ctx) }

func openBootstrapDB(ctx context.Context, getenv func(string) string) (bootstrapDB, error) {
	// Every bootstrap connection is a deferred-derivation session: content_files
	// triggers skip its writes and the finalizer rebuilds the hardcoded-secret
	// side table after collection (#7125).
	db, err := runtimecfg.OpenPostgresWithSession(ctx, getenv, secretlines.DeferredSessionSQL)
	if err != nil {
		return nil, err
	}
	return &bootstrapSQLDB{SQLDB: postgres.SQLDB{DB: db}, raw: db}, nil
}

// applySchemaFromEnv applies the bootstrap layout with content search
// indexes deferred, honoring the #6956 coordination knobs
// (postgres.OwnershipWaitEnv, postgres.LockRetryBudgetEnv) the same way
// db-migrate does, and logging the migrator's events through the runtime's
// telemetry logger.
func applySchemaFromEnv(getenv func(string) string) applyBootstrapFn {
	return func(ctx context.Context, db bootstrapDB, logger *slog.Logger) error {
		options, err := schemaOptionsFromEnv(getenv, logger)
		if err != nil {
			return err
		}
		return postgres.ApplyBootstrapWithOptions(ctx, db, options)
	}
}

// schemaOptionsFromEnv is the bootstrap-index option set: knobs from the
// environment, content search indexes deferred, the given logger.
func schemaOptionsFromEnv(getenv func(string) string, logger *slog.Logger) (postgres.BootstrapOptions, error) {
	options, err := postgres.BootstrapOptionsFromEnv(getenv)
	if err != nil {
		return postgres.BootstrapOptions{}, err
	}
	options.DeferContentSearchIndexes = true
	options.Logger = logger
	return options, nil
}

func openBootstrapGraph(ctx context.Context, database bootstrapDB, getenv func(string) string, tracer trace.Tracer, instruments *telemetry.Instruments) (graphDeps, error) {
	writer, closer, err := openBootstrapCanonicalWriter(ctx, database, getenv, tracer, instruments)
	if err != nil {
		return graphDeps{}, err
	}
	return graphDeps{writer: writer, close: closer.Close}, nil
}
