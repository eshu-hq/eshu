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
)

const requiredDisposableOptIn = "1"

// OpenDisposableDatabase creates a unique database through an explicitly
// approved connection to PostgreSQL's administrative postgres database. The
// generated database is force-dropped during test cleanup.
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
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(databaseName)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create disposable PostgreSQL database: %v", err)
	}
	targetConfig := config.Copy()
	targetConfig.Database = databaseName
	targetConnection := stdlib.RegisterConnConfig(targetConfig)
	targetDB, err := sql.Open("pgx", targetConnection)
	if err != nil {
		stdlib.UnregisterConnConfig(targetConnection)
		dropDisposableDatabase(t, adminDB, databaseName)
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	if err := targetDB.PingContext(ctx); err != nil {
		_ = targetDB.Close()
		stdlib.UnregisterConnConfig(targetConnection)
		dropDisposableDatabase(t, adminDB, databaseName)
		t.Fatalf("ping disposable PostgreSQL database: %v", err)
	}
	t.Cleanup(func() {
		_ = targetDB.Close()
		stdlib.UnregisterConnConfig(targetConnection)
		dropDisposableDatabase(t, adminDB, databaseName)
		_ = adminDB.Close()
	})
	return ctx, targetDB
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
