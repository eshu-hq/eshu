// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build issue7033_canary_startup

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	issue7033CanaryAdminDSNEnv  = "ESHU_TEST_ISSUE7033_CANARY_POSTGRES_DSN"
	issue7033CanaryOptInEnv     = "ESHU_TEST_ISSUE7033_CANARY_POSTGRES_DISPOSABLE"
	issue7033CanaryNeo4jURIEnv  = "ESHU_TEST_ISSUE7033_CANARY_NEO4J_URI"
	issue7033CanaryNeo4jUserEnv = "ESHU_TEST_ISSUE7033_CANARY_NEO4J_USERNAME"
	issue7033CanaryNeo4jPassEnv = "ESHU_TEST_ISSUE7033_CANARY_NEO4J_PASSWORD" // #nosec G101 -- test environment variable name.
)

// TestIssue7033CanaryStartupReadOnlyLive proves the exact canary safety
// precondition: a Neo4j-backed API can start with both startup-backfill
// markers complete while every connection in its Postgres pool is read-only.
// It is opt-in because it creates and force-drops a disposable Postgres
// database and requires an isolated Neo4j endpoint. The disabled bootstrap
// mode still attempts a best-effort governance audit; that attempted insert
// must be rejected without aborting startup or persisting an audit event.
func TestIssue7033CanaryStartupReadOnlyLive(t *testing.T) {
	neo4jURI := strings.TrimSpace(os.Getenv(issue7033CanaryNeo4jURIEnv))
	if neo4jURI == "" {
		t.Skipf("set %s to run the isolated Neo4j startup proof", issue7033CanaryNeo4jURIEnv)
	}
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv(issue7033CanaryAdminDSNEnv),
		os.Getenv(issue7033CanaryOptInEnv),
		3*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply disposable schema: %v", err)
	}
	seedIssue7033CanaryMarkers(t, ctx, db)
	before := issue7033CanaryPersistentState(t, ctx, db)
	readonlyDSN := issue7033CanaryReadOnlyDSN(t, ctx, db, os.Getenv(issue7033CanaryAdminDSNEnv))

	getenv := issue7033CanaryGetenv(readonlyDSN, neo4jURI)
	handler, cleanup, _, err := wireAPI(ctx, getenv, nil, nil)
	if err != nil {
		t.Fatalf("wireAPI() with read-only Postgres pool: %v", err)
	}
	defer cleanup()

	for _, path := range []string{"/healthz", "/readyz"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d: %s", path, recorder.Code, http.StatusOK, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v0/code/topics/investigate", bytes.NewBufferString(`{"topic":"issue7033canary","limit":1}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST code-topic route status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if after := issue7033CanaryPersistentState(t, ctx, db); after != before {
		t.Fatalf("read-only canary changed persistent Postgres state: before=%s after=%s", before, after)
	}
}

func seedIssue7033CanaryMarkers(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO graph_node_owner_backfill_state (backfill_key, completed_at)
VALUES ('cloud_resource_owner:v1', clock_timestamp())`); err != nil {
		t.Fatalf("seed completed owner backfill marker: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO infra_resource_entity_backfill_markers (marker_name, completed_at)
VALUES ($1, clock_timestamp())`, inventory.BackfillMarker); err != nil {
		t.Fatalf("seed completed infra backfill marker: %v", err)
	}
}

func issue7033CanaryPersistentState(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var markers, audits int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_owner_backfill_state WHERE backfill_key = 'cloud_resource_owner:v1'`).Scan(&markers); err != nil {
		t.Fatalf("count owner marker: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM governance_audit_events`).Scan(&audits); err != nil {
		t.Fatalf("count governance audit events: %v", err)
	}
	return fmt.Sprintf("owner_markers=%d governance_audits=%d", markers, audits)
}

func issue7033CanaryReadOnlyDSN(t *testing.T, ctx context.Context, db *sql.DB, adminDSN string) string {
	t.Helper()
	var database string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	config, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse disposable database admin DSN: %v", err)
	}
	config.Database = database
	config.RuntimeParams["default_transaction_read_only"] = "on"
	return stdlib.RegisterConnConfig(config)
}

func issue7033CanaryGetenv(readonlyDSN, neo4jURI string) func(string) string {
	values := map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE":               authBootstrapModeDisabled,
		"ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED": "false",
		"ESHU_GRAPH_BACKEND":                     "neo4j",
		"ESHU_POSTGRES_DSN":                      readonlyDSN,
		"ESHU_QUERY_PROFILE":                     "production",
		"NEO4J_DATABASE":                         "neo4j",
		"NEO4J_PASSWORD":                         os.Getenv(issue7033CanaryNeo4jPassEnv),
		"NEO4J_URI":                              neo4jURI,
		"NEO4J_USERNAME":                         os.Getenv(issue7033CanaryNeo4jUserEnv),
	}
	return func(key string) string { return values[key] }
}
