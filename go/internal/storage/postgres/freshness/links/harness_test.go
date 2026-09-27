// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// liveDSNEnv names the admin DSN of a disposable PostgreSQL 18 server. The
// harness creates one template database with the full Eshu bootstrap and
// clones it per test, so each test owns every table, including the ones the
// sweeper scans globally.
const liveDSNEnv = "ESHU_POSTGRES_TEST_DSN"

var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// ledgerDB is one test's private database.
type ledgerDB struct {
	ctx   context.Context
	raw   *sql.DB
	store postgres.SQLDB
	dsn   string
}

func adminDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(liveDSNEnv))
	if dsn == "" {
		t.Skipf("set %s to run the changed-since link live proof", liveDSNEnv)
	}
	return dsn
}

func withDatabase(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", liveDSNEnv, err)
	}
	u.Path = "/" + name
	return u.String()
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}

// buildTemplate applies the full bootstrap once per test binary.
func buildTemplate(t *testing.T, dsn string) (string, error) {
	templateOnce.Do(func() {
		name := "cs7127_tmpl_" + randomSuffix(t)
		admin, err := sql.Open("pgx", dsn)
		if err != nil {
			templateErr = err
			return
		}
		defer func() { _ = admin.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
			templateErr = fmt.Errorf("create template: %w", err)
			return
		}
		tmpl, err := sql.Open("pgx", withDatabase(t, dsn, name))
		if err != nil {
			templateErr = err
			return
		}
		if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: tmpl}); err != nil {
			templateErr = fmt.Errorf("bootstrap template: %w", err)
		}
		_ = tmpl.Close()
		templateName = name
	})
	return templateName, templateErr
}

// openLedgerDB clones the bootstrapped template into a private database and
// drops it when the test ends.
func openLedgerDB(t *testing.T) *ledgerDB {
	t.Helper()
	dsn := adminDSN(t)
	tmpl, err := buildTemplate(t, dsn)
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	name := "cs7127_" + randomSuffix(t)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name+" TEMPLATE "+tmpl); err != nil {
		t.Fatalf("clone template: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	testDSN := withDatabase(t, dsn, name)
	raw, err := sql.Open("pgx", testDSN)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	raw.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = raw.Close() })
	return &ledgerDB{ctx: ctx, raw: raw, store: postgres.SQLDB{DB: raw}, dsn: testDSN}
}

func (l *ledgerDB) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := l.raw.ExecContext(l.ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", firstLine(query), err)
	}
}

func firstLine(query string) string {
	query = strings.TrimSpace(query)
	if i := strings.IndexByte(query, '\n'); i > 0 {
		return query[:i]
	}
	return query
}

var fixtureEpoch = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func (l *ledgerDB) seedScope(t *testing.T, scopeID string) {
	t.Helper()
	l.exec(t, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active')`, scopeID, fixtureEpoch)
}

// seedGeneration inserts a generation. activatedAt and supersededAt may be
// the zero time for NULL.
func (l *ledgerDB) seedGeneration(t *testing.T, scopeID, generationID string, isDelta bool, status string,
	activatedAt, supersededAt time.Time,
) {
	t.Helper()
	l.exec(t, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
    ingested_at, status, activated_at, superseded_at)
VALUES ($1, $2, 'snapshot', $3, $4, $4, $5, $6, $7)`,
		generationID, scopeID, isDelta, fixtureEpoch, status, nullTime(activatedAt), nullTime(supersededAt))
}

func nullTime(ts time.Time) any {
	if ts.IsZero() {
		return nil
	}
	return ts
}

func (l *ledgerDB) setActive(t *testing.T, scopeID, generationID string) {
	t.Helper()
	l.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
}

// fact is one fact_records row of a fixture.
type fact struct {
	kind      string
	key       string
	payload   string
	tombstone bool
	uri       string
}

func (l *ledgerDB) insertFacts(t *testing.T, scopeID, generationID string, facts []fact) {
	t.Helper()
	for i, f := range facts {
		payload := f.payload
		if payload == "" {
			payload = "{}"
		}
		var uri any
		if f.uri != "" {
			uri = f.uri
		}
		l.exec(t, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, 'git', $5, $6, $7, $7, $8, $9::jsonb)`,
			fmt.Sprintf("%s/%s/%d", scopeID, generationID, i), scopeID, generationID, f.kind, f.key,
			uri, fixtureEpoch, f.tombstone, payload)
	}
}

// journal writes one activation row the way PR-3b's Ack will.
func (l *ledgerDB) journal(t *testing.T, scopeID, generationID, priorID string) {
	t.Helper()
	l.exec(t, `
INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
VALUES ($1, $2, NULLIF($3, ''), 'ack', $4)`, scopeID, generationID, priorID, fixtureEpoch)
}

// queryStrings returns every row of a query as one string per row.
func (l *ledgerDB) queryStrings(t *testing.T, query string, args ...any) []string {
	t.Helper()
	rows, err := l.raw.QueryContext(l.ctx, query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", firstLine(query), err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func (l *ledgerDB) queryInt(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := l.raw.QueryRowContext(l.ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", firstLine(query), err)
	}
	return n
}
