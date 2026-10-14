// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

const requiredDisposableOptIn = "1"

// OpenDisposableDatabase creates a unique database through an explicitly
// approved connection to PostgreSQL's administrative postgres database. The
// generated database uses pristine template0 and a public search path, and is
// force-dropped during test cleanup.
func OpenDisposableDatabase(
	t testing.TB,
	adminDSN string,
	optIn string,
	timeout time.Duration,
) (context.Context, *sql.DB) {
	t.Helper()
	if strings.TrimSpace(adminDSN) == "" {
		t.Skip(missingAdminDSNSkipMessage())
	}
	config, err := validateAdminDSN(adminDSN, optIn)
	if err != nil {
		t.Fatalf("refuse destructive PostgreSQL proof target: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", config.ConnString())
	if err != nil {
		t.Fatalf("open PostgreSQL administrative database: %v", err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		t.Fatalf("ping PostgreSQL administrative database: %v", err)
	}

	databaseName := disposableDatabaseName(t)
	// Create from template0 so extensions and relations installed in
	// template1 (for example a pg_trgm left in public) cannot leak into the
	// proof database and shadow the bootstrap's own installs.
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(databaseName)+" TEMPLATE template0"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create disposable PostgreSQL database: %v", err)
	}
	targetConfig := config.Copy()
	targetConfig.Database = databaseName
	// Pin the search path to public so unqualified operator classes such as
	// gin_trgm_ops resolve to this database's own installs even when the
	// caller's session carries a schema-first search_path.
	targetConfig.RuntimeParams = cloneRuntimeParams(config.RuntimeParams)
	targetConfig.RuntimeParams["search_path"] = "public"
	// Connect the way the service runtimes do (runtime.OpenPostgres): every
	// connection carries the derive-aware writer setting, so a proof that
	// seeds infra-typed content rows leaves no rolling-upgrade fence marks.
	//
	// Register the drop before opening the target: every later exit, including
	// a failed open or ping, runs this cleanup, so neither the target handle
	// nor the database leaks on a failure path.
	var targetDB *sql.DB
	t.Cleanup(func() {
		if targetDB != nil {
			_ = targetDB.Close()
		}
		dropDisposableDatabase(t, adminDB, databaseName)
		_ = adminDB.Close()
	})
	targetDB = stdlib.OpenDB(*targetConfig, inventory.WriterConnectOption())
	if err := targetDB.PingContext(ctx); err != nil {
		t.Fatalf("ping disposable PostgreSQL database: %v", err)
	}
	return ctx, targetDB
}

// cloneRuntimeParams copies a pgx runtime-parameter map so the disposable
// target can pin its own search_path without rewriting the shared
// administrative config's session defaults.
func cloneRuntimeParams(params map[string]string) map[string]string {
	out := make(map[string]string, len(params)+1)
	for key, value := range params {
		out[key] = value
	}
	return out
}

func missingAdminDSNSkipMessage() string {
	return "disposable PostgreSQL proof DSN is not set; see the calling test for its environment variable"
}

func validateAdminDSN(dsn, optIn string) (*pgx.ConnConfig, error) {
	if optIn != requiredDisposableOptIn {
		return nil, fmt.Errorf("explicit disposable-database opt-in must equal %q", requiredDisposableOptIn)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse administrative DSN: %w", err)
	}
	if config.Database != "postgres" {
		return nil, fmt.Errorf("administrative DSN database must be %q", "postgres")
	}
	return config, nil
}

func disposableDatabaseName(t testing.TB) string {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate disposable PostgreSQL database name: %v", err)
	}
	return "eshu_content_index_proof_" + hex.EncodeToString(random)
}

func dropDisposableDatabase(t testing.TB, adminDB *sql.DB, databaseName string) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(
		cleanupCtx,
		"DROP DATABASE IF EXISTS "+quoteIdentifier(databaseName)+" WITH (FORCE)",
	); err != nil {
		t.Errorf("drop disposable PostgreSQL database: %v", err)
	}
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
