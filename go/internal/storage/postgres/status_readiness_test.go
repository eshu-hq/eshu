// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestStatusReadinessOnLivePostgres exercises the production checker with a
// one-second budget on a warmed connection to live Postgres. It makes no writes.
func TestStatusReadinessOnLivePostgres(t *testing.T) {
	if os.Getenv("ESHU_STATUS_READINESS_LIVE") != "1" {
		t.Skip("set ESHU_STATUS_READINESS_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("ESHU_POSTGRES_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()
	if err := db.PingContext(connectCtx); err != nil {
		t.Fatalf("connect to live Postgres: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	started := time.Now()
	if err := NewStatusStore(SQLQueryer{DB: db}).CheckStatusReadiness(ctx); err != nil {
		t.Fatalf("CheckStatusReadiness() within one second: %v", err)
	}
	t.Logf("core schema readiness completed in %s", time.Since(started))
}

func TestStatusReadinessChecksCoreSchemaWithoutAggregates(t *testing.T) {
	t.Parallel()

	queryer := &recordingQueryer{}
	store := NewStatusStore(queryer)
	if err := store.CheckStatusReadiness(context.Background()); err != nil {
		t.Fatalf("CheckStatusReadiness() error = %v", err)
	}
	if got, want := len(queryer.queries), 1; got != want {
		t.Fatalf("readiness query count = %d, want %d", got, want)
	}
	if !strings.Contains(strings.ToUpper(queryer.queries[0]), "LIMIT 0") {
		t.Fatalf("readiness query can scan data rows: %s", queryer.queries[0])
	}
	for _, relation := range []string{"ingestion_scopes", "fact_work_items", "eshu_schema_migrations"} {
		found := false
		for _, query := range queryer.queries {
			if strings.Contains(query, relation) {
				found = true
			}
			if strings.Contains(query, "active_fact_work_items") || strings.Contains(query, "COUNT(") {
				t.Fatalf("readiness issued an aggregate query: %s", query)
			}
		}
		if !found {
			t.Fatalf("readiness did not check %s: %v", relation, queryer.queries)
		}
	}
}

func TestStatusReadinessFailsOnMissingSchema(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{err: errors.New("relation does not exist")}}}
	err := NewStatusStore(queryer).CheckStatusReadiness(context.Background())
	if err == nil || !strings.Contains(err.Error(), "relation does not exist") {
		t.Fatalf("CheckStatusReadiness() error = %v, want missing-relation error", err)
	}
}

func TestStatusReadinessHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queryer := &recordingQueryer{}
	err := NewStatusStore(queryer).CheckStatusReadiness(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckStatusReadiness() error = %v, want context canceled", err)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("canceled readiness issued queries: %v", queryer.queries)
	}
}
