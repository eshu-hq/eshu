// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !windows

package eshulocal

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestStartEmbeddedPostgresBootstrapsThroughForkedDriverLive starts the real
// embedded Postgres runtime and connects to the database it created.
//
// Every other test in this package injects a fake through newEmbeddedPostgres,
// which is right for testing this package's own lifecycle logic but means none
// of them exercises the library underneath. That matters now: go.mod replaces
// embedded-postgres with a fork whose only change is the driver its bookkeeping
// connection uses -- CREATE DATABASE and the post-start health check moved from
// lib/pq to the pgx stdlib driver. A mocked runtime cannot tell whether that
// swap works, and a failure there would surface as `eshu local` hanging or
// failing on a developer machine rather than in CI.
//
// Gated behind ESHU_EMBEDDED_POSTGRES_LIVE because it downloads a Postgres
// distribution on first run and binds a port. Skipping is the correct default;
// what this test exists for is to be runnable, and to fail loudly when the
// bookkeeping path breaks.
func TestStartEmbeddedPostgresBootstrapsThroughForkedDriverLive(t *testing.T) {
	if os.Getenv("ESHU_EMBEDDED_POSTGRES_LIVE") == "" {
		t.Skip("set ESHU_EMBEDDED_POSTGRES_LIVE to start a real embedded Postgres")
	}

	root := t.TempDir()
	layout, err := BuildLayout(
		func(key string) string {
			if key == "ESHU_HOME" {
				return root
			}
			return ""
		},
		func() (string, error) { return root, nil },
		"linux",
		root,
	)
	if err != nil {
		t.Fatalf("BuildLayout() error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	managed, err := StartEmbeddedPostgres(ctx, layout)
	if err != nil {
		t.Fatalf("StartEmbeddedPostgres() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := managed.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	// Reaching here already proves the forked health check succeeded: Start does
	// not return until it passes. This asserts the database the forked
	// createDatabase path made is actually usable, which the health check alone
	// does not establish.
	db, err := sql.Open("pgx", managed.DSN)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	defer func() { _ = db.Close() }()

	var current string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&current); err != nil {
		t.Fatalf("SELECT current_database() error = %v, want nil", err)
	}
	if current != localPostgresDatabase {
		t.Fatalf("current_database() = %q, want %q", current, localPostgresDatabase)
	}
}
