// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Live proof for the #7797 delta-base union read path: ListFacts and
// CountFacts against a real Postgres. Opt-in via ESHU_POSTGRES_TEST_DSN; the
// schema is isolated per test and drops with it. Trees: base full generation
// A {keep.go, w.go} at commit A (activated), then delta generation B diffed
// from A {keep.go modified, w.go deleted}.
const deltaUnionLiveSchema = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY
);
CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    source_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    delta_baseline_commit_sha TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE generation_retention_events (
    event_id TEXT PRIMARY KEY,
    scope_id_hash TEXT NOT NULL,
    generation_id_hash TEXT NOT NULL,
    scope_class TEXT NOT NULL,
    policy_scope TEXT NOT NULL,
    policy_revision TEXT NOT NULL,
    generation_observed_at TIMESTAMPTZ NULL,
    generation_superseded_at TIMESTAMPTZ NULL,
    reason TEXT NOT NULL,
    row_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    pruned_at TIMESTAMPTZ NOT NULL
);
`

func openDeltaUnionLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live delta-union proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	postgresproof.InstallTrigramExtension(ctx, t, adminDB)

	schemaName := fmt.Sprintf("delta_union_7797_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(cleanupCtx, "DROP SCHEMA "+array.QuoteIdentifier(schemaName)+" CASCADE")
	})

	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	query := targetURL.Query()
	searchPath := strings.TrimSpace(query.Get("search_path"))
	if searchPath == "" {
		searchPath = "public"
	}
	query.Set("search_path", schemaName+","+searchPath)
	targetURL.RawQuery = query.Encode()

	db, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open isolated Postgres connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, deltaUnionLiveSchema); err != nil {
		t.Fatalf("create delta-union schema: %v", err)
	}
	return ctx, db
}

func seedDeltaUnionLiveScope(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id) VALUES ('scope-live-1')`, nil},
		{`INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
			 observed_at, ingested_at, status, activated_at, delta_baseline_commit_sha)
			VALUES ('gen-live-base', 'scope-live-1', 'sync', 'commit-aaa', false,
				$1, $1, 'superseded', $1, NULL)`, []any{now.Add(-time.Hour)}},
		{`INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
			 observed_at, ingested_at, status, activated_at, delta_baseline_commit_sha)
			VALUES ('gen-live-delta', 'scope-live-1', 'sync', 'commit-bbb', true,
				$1, $1, 'active', $1, 'commit-aaa')`, []any{now}},
		{`INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
			 observed_at, ingested_at, status, activated_at, delta_baseline_commit_sha)
			VALUES ('gen-live-orphan', 'scope-live-1', 'sync', 'commit-ccc', true,
				$1, $1, 'pending', NULL, 'commit-pruned')`, []any{now}},
		{`INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
			 observed_at, ingested_at, status, activated_at, delta_baseline_commit_sha)
			VALUES ('gen-live-old', 'scope-live-1', 'sync', 'commit-old', false,
				$1, $1, 'superseded', $1, NULL)`, []any{now.Add(-2 * time.Hour)}},
		{`INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
			 observed_at, ingested_at, status, activated_at, delta_baseline_commit_sha)
			VALUES ('gen-live-delta2', 'scope-live-1', 'sync', 'commit-ddd', true,
				$1, $1, 'pending', NULL, 'commit-old')`, []any{now}},
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed generation: %v", err)
		}
	}
	facts := []struct {
		id, generation, kind, key, payload string
	}{
		{
			"live-base-repo", "gen-live-base", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha"}`,
		},
		{
			"live-base-keep", "gen-live-base", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`,
		},
		{
			"live-base-w", "gen-live-base", "file", "file:repo-1:w.go",
			`{"repo_id":"repo-1","relative_path":"w.go"}`,
		},
		{
			"live-base-other", "gen-live-base", "file", "file:repo-1:other.go",
			`{"repo_id":"repo-1","relative_path":"other.go"}`,
		},
		{
			"live-delta-repo", "gen-live-delta", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha","delta_generation":true,"delta_relative_paths":["keep.go","x.go"],"delta_deleted_relative_paths":["w.go"]}`,
		},
		{
			"live-delta-keep", "gen-live-delta", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`,
		},
		{
			"live-delta-x", "gen-live-delta", "file", "file:repo-1:x.go",
			`{"repo_id":"repo-1","relative_path":"x.go"}`,
		},
		{
			"live-orphan-repo", "gen-live-orphan", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha","delta_generation":true,"delta_relative_paths":["keep.go"],"delta_deleted_relative_paths":[]}`,
		},
		{
			"live-old-repo", "gen-live-old", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha"}`,
		},
		{
			"live-delta2-repo", "gen-live-delta2", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha","delta_generation":true,"delta_relative_paths":["keep.go"],"delta_deleted_relative_paths":[]}`,
		},
	}
	for _, fact := range facts {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO fact_records
				(fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
				 source_system, source_fact_key, observed_at, ingested_at, payload)
			VALUES ($1, 'scope-live-1', $2, $3, $4, 'git', $4, $5, $5, $6::jsonb)`,
			fact.id, fact.generation, fact.kind, fact.key, now, fact.payload); err != nil {
			t.Fatalf("seed fact %s: %v", fact.id, err)
		}
	}
}

func TestFactStoreListFactsDeltaUnionLive(t *testing.T) {
	ctx, db := openDeltaUnionLiveDB(t)
	seedDeltaUnionLiveScope(t, ctx, db)
	store := NewFactStore(SQLDB{DB: db})

	loaded, err := store.ListFacts(ctx, "scope-live-1", "gen-live-delta")
	if err != nil {
		t.Fatalf("ListFacts(delta) error = %v, want nil", err)
	}
	byID := make(map[string]bool, len(loaded))
	for _, env := range loaded {
		byID[env.FactID] = true
	}
	for _, want := range []string{"live-delta-repo", "live-delta-keep", "live-base-other"} {
		if !byID[want] {
			t.Fatalf("union is missing fact %q: %v", want, byID)
		}
	}
	for _, absent := range []string{"live-base-repo", "live-base-keep", "live-base-w"} {
		if byID[absent] {
			t.Fatalf("union contains dropped base fact %q", absent)
		}
	}

	count, err := store.CountFacts(ctx, "scope-live-1", "gen-live-delta")
	if err != nil {
		t.Fatalf("CountFacts(delta) error = %v, want nil", err)
	}
	if got, want := count, 7; got != want {
		t.Fatalf("CountFacts(delta) = %d, want %d (4 base plus 3 delta)", got, want)
	}
}

func TestFactStoreListFactsDeltaBaseMissingLive(t *testing.T) {
	ctx, db := openDeltaUnionLiveDB(t)
	seedDeltaUnionLiveScope(t, ctx, db)
	store := NewFactStore(SQLDB{DB: db})

	loaded, err := store.ListFacts(ctx, "scope-live-1", "gen-live-orphan")
	if loaded != nil {
		t.Fatalf("ListFacts(orphan delta) = %d facts, want nil", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts(orphan delta) error = %v, want DeltaBaseMissingError", err)
	}
}

// ReadDeltaUnionActive assembles the ACTIVE side through the independent
// SQL: the delta overlaid on its base, with generation annotations.
func TestFactStoreReadDeltaUnionActiveLive(t *testing.T) {
	ctx, db := openDeltaUnionLiveDB(t)
	seedDeltaUnionLiveScope(t, ctx, db)
	store := NewFactStore(SQLDB{DB: db})

	snapshot, err := store.ReadDeltaUnionActive(ctx, []string{"scope-live-1"})
	if err != nil {
		t.Fatalf("ReadDeltaUnionActive() error = %v, want nil", err)
	}
	if len(snapshot.Deltas) != 1 {
		t.Fatalf("deltas = %d, want 1 (the active gen-live-delta)", len(snapshot.Deltas))
	}
	expected := goldengate.AssembleDeltaUnionExpectations(snapshot.Rows, snapshot.Deltas)
	if len(expected) != 1 {
		t.Fatalf("expectations = %d, want 1", len(expected))
	}
	exp := expected[0]
	if exp.BaseGenerationID != "gen-live-base" || exp.ActiveGenerationID != "gen-live-delta" || !exp.ActiveIsDelta {
		t.Fatalf("generation annotation = %+v, want base gen-live-base and active delta gen-live-delta", exp)
	}
	for _, want := range []string{"keep.go", "x.go", "other.go"} {
		if _, ok := exp.Files[want]; !ok {
			t.Fatalf("expected files = %v, want %q", exp.Files, want)
		}
	}
	if _, ok := exp.Files["w.go"]; ok {
		t.Fatalf("expected files = %v, want no deleted w.go", exp.Files)
	}
}

// A superseded base with a retention prune event refuses even though its row
// exists: retention may have taken part of its facts.
func TestFactStoreListFactsPrunedBaseRefusesLive(t *testing.T) {
	ctx, db := openDeltaUnionLiveDB(t)
	seedDeltaUnionLiveScope(t, ctx, db)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO generation_retention_events
			(event_id, scope_id_hash, generation_id_hash, scope_class,
			 policy_scope, policy_revision, reason, pruned_at)
		VALUES ('event-1', $1, $2, 'repository', 'fleet', 'rev-1',
			'superseded_window_expired', now())`,
		retentionHashID("scope", "scope-live-1"),
		retentionHashID("generation", "gen-live-old")); err != nil {
		t.Fatalf("seed prune event: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})

	loaded, err := store.ListFacts(ctx, "scope-live-1", "gen-live-delta2")
	if loaded != nil {
		t.Fatalf("ListFacts(pruned-base delta) = %d facts, want nil", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts(pruned-base delta) error = %v, want DeltaBaseMissingError", err)
	}
}
