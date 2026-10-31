// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// skipPublicOnlyDefinitionsForIsolatedTestBootstrap drops public-only
// definitions when a test binary bootstraps a non-public schema, logging
// every skip loudly. Production binaries (testing.Testing() false),
// public-schema bootstraps, unresolvable executors, and schema-lookup
// failures all keep the full strict layout so 241's own guard still fires
// there. Callers needing authority tables must bootstrap public instead.
func skipPublicOnlyDefinitionsForIsolatedTestBootstrap(ctx context.Context, exec db.Executor, definitions []Definition, logger *slog.Logger) []Definition {
	if !testing.Testing() {
		return definitions
	}
	hasPublicOnly := false
	for _, def := range definitions {
		if def.PublicOnly {
			hasPublicOnly = true
			break
		}
	}
	if !hasPublicOnly {
		return definitions
	}
	schema, ok := bootstrapCurrentSchema(ctx, exec)
	if !ok || schema == "public" {
		return definitions
	}
	kept := make([]Definition, 0, len(definitions))
	for _, def := range definitions {
		if !def.PublicOnly {
			kept = append(kept, def)
			continue
		}
		logger.Warn("postgres bootstrap skips public-only migration for isolated test schema",
			"migration", def.Name, "schema", schema)
	}
	return kept
}

// bootstrapCurrentSchema reports the session's first search_path schema for
// query-capable executors. Unknown executor shapes report ok=false so the
// caller keeps the strict full layout.
func bootstrapCurrentSchema(ctx context.Context, exec db.Executor) (string, bool) {
	queryer, ok := bootstrapQueryer(exec)
	if !ok {
		return "", false
	}
	var schema string
	if err := queryer.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return "", false
	}
	return schema, true
}

func bootstrapQueryer(exec db.Executor) (bootstrapSchemaQueryer, bool) {
	if sqldb, ok := exec.(SQLDB); ok {
		if sqldb.DB == nil {
			return nil, false
		}
		return sqldb.DB, true
	}
	queryer, ok := exec.(bootstrapSchemaQueryer)
	return queryer, ok
}

// bootstrapSchemaQueryer is the read half executors need for the isolated
// test-schema probe. *sql.DB, *sql.Conn, and *sql.Tx all satisfy it.
type bootstrapSchemaQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
