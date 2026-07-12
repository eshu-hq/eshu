// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// scopeGenerationEnumProofSchemaSQL is the minimal scope/generation/fact table
// set listScopeGenerationWorkQuery selects over. It carries exactly the columns
// the enumeration projects (source_system, scope_kind, parent_scope_id,
// active_generation_id, collector_kind, partition_key, payload on scopes;
// observed_at, ingested_at, status, trigger_kind, freshness_hint on
// generations), so the DSN-gated proof exercises the real joins and DISTINCT the
// hermetic scan test cannot.
const scopeGenerationEnumProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    source_system TEXT NOT NULL,
    scope_kind TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    active_generation_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL DEFAULT '',
    payload JSONB NULL
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL
);

CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);
`

// TestListScopeGenerationWorkLive proves listScopeGenerationWorkQuery parses and
// runs against the real Postgres schema and enumerates the ACTIVE generation per
// scope, not every generation whose facts happen to linger. The fixture gives
// scope-a a superseded historical generation (gen-a0) whose facts still exist
// alongside its active generation (gen-a1); the enumeration must return gen-a1
// only. Re-draining gen-a0 would commit a `superseded` generation the reducer
// never projects on a fresh ingest, so the re-drain would not reproduce the
// baseline graph (codex #5136 P2). scope-a also carries two facts in gen-a1, so
// the per-generation collapse is exercised too. Ordered by
// (scope_id, generation_id). Gated on ESHU_SCOPE_GENERATION_ENUM_PROOF_DSN; the
// hermetic scan/shape test (TestListScopeGenerationWorkEnumeratesCorpus) is the
// always-run CI guard.
func TestListScopeGenerationWorkLive(t *testing.T) {
	dsn := os.Getenv("ESHU_SCOPE_GENERATION_ENUM_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SCOPE_GENERATION_ENUM_PROOF_DSN to run the scope-generation enumeration Postgres proof")
	}

	ctx := context.Background()
	db := openScopeGenerationEnumProofDB(t, dsn)

	if _, err := db.ExecContext(ctx, scopeGenerationEnumProofSchemaSQL); err != nil {
		t.Fatalf("provision schema: %v", err)
	}

	observed := time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC)
	ingested := observed.Add(5 * time.Minute)
	seedScopeGenerationEnumFixture(t, ctx, db, observed, ingested)

	works, err := NewFactStore(ExecQueryer(SQLDB{DB: db})).ListScopeGenerationWork(ctx)
	if err != nil {
		t.Fatalf("ListScopeGenerationWork() error = %v, want nil", err)
	}

	if got, want := len(works), 2; got != want {
		t.Fatalf("work count = %d, want %d (one active non-terminal generation per scope; superseded gen-a0, the duplicate fact, and terminal-only scope-c must not add works)", got, want)
	}
	for _, w := range works {
		if w.Scope.ScopeID == "scope-c" {
			t.Fatalf("scope-c (latest generation terminal) must be excluded, got a work for it: gen=%q status=%q", w.Generation.GenerationID, w.Generation.Status)
		}
	}
	if works[0].Scope.ScopeID != "scope-a" || works[1].Scope.ScopeID != "scope-b" {
		t.Fatalf("scope order = %q,%q, want scope-a,scope-b", works[0].Scope.ScopeID, works[1].Scope.ScopeID)
	}
	if works[0].Generation.Status != scope.GenerationStatus("accepted") {
		t.Fatalf("scope-a generation status = %q, want accepted (the active generation, not superseded gen-a0)", works[0].Generation.Status)
	}
	if works[0].Scope.SourceSystem != "git" || works[0].Scope.CollectorKind != scope.CollectorKind("collector-git") {
		t.Fatalf("scope-a hydration = %q/%q, want git/collector-git", works[0].Scope.SourceSystem, works[0].Scope.CollectorKind)
	}
	if !works[0].Generation.ObservedAt.Equal(observed) {
		t.Fatalf("scope-a observed_at = %v, want %v", works[0].Generation.ObservedAt, observed)
	}
	if works[0].Generation.GenerationID != "gen-a1" || works[0].Generation.ScopeID != "scope-a" {
		t.Fatalf("scope-a generation = %q/%q, want gen-a1/scope-a", works[0].Generation.GenerationID, works[0].Generation.ScopeID)
	}
}

func openScopeGenerationEnumProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS fact_records, scope_generations, ingestion_scopes CASCADE")
		_ = db.Close()
	})
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	// Start from a clean slate so a prior aborted run does not leak tables.
	if _, err := db.Exec("DROP TABLE IF EXISTS fact_records, scope_generations, ingestion_scopes CASCADE"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	return db
}

func seedScopeGenerationEnumFixture(t *testing.T, ctx context.Context, db *sql.DB, observed, ingested time.Time) {
	t.Helper()
	scopes := []struct {
		scopeID, source, kind, collector, gen string
	}{
		{"scope-a", "git", "repository", "collector-git", "gen-a1"},
		{"scope-b", "github", "repository", "collector-github", "gen-b1"},
	}
	for _, s := range scopes {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO ingestion_scopes (scope_id, source_system, scope_kind, active_generation_id, collector_kind, partition_key, payload)
			 VALUES ($1,$2,$3,$4,$5,'', '{}'::jsonb)`,
			s.scopeID, s.source, s.kind, s.gen, s.collector); err != nil {
			t.Fatalf("insert scope %s: %v", s.scopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO scope_generations (generation_id, scope_id, observed_at, ingested_at, status, trigger_kind, freshness_hint)
			 VALUES ($1,$2,$3,$4,'accepted','scheduled','')`,
			s.gen, s.scopeID, observed, ingested); err != nil {
			t.Fatalf("insert generation %s: %v", s.gen, err)
		}
	}
	// scope-a also has a superseded historical generation (gen-a0, ingested
	// earlier) whose facts still linger. active_generation_id points at gen-a1, so
	// the enumeration must pick gen-a1 and skip gen-a0 — re-draining a superseded
	// generation would not match the baseline graph.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scope_generations (generation_id, scope_id, observed_at, ingested_at, status, trigger_kind, freshness_hint)
		 VALUES ('gen-a0','scope-a',$1,$2,'superseded','scheduled','')`,
		observed.Add(-time.Hour), ingested.Add(-time.Hour)); err != nil {
		t.Fatalf("insert superseded generation gen-a0: %v", err)
	}

	// scope-c has NO active pointer and its only (hence newest) generation is
	// terminal (failed) with a fact. latestGenerationCTE falls back to that newest
	// generation, so the terminal-status filter must drop scope-c entirely —
	// otherwise CommitScopeGeneration would abort on the terminal status.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO ingestion_scopes (scope_id, source_system, scope_kind, active_generation_id, collector_kind, partition_key, payload)
		 VALUES ('scope-c','git','repository',NULL,'collector-git','', '{}'::jsonb)`); err != nil {
		t.Fatalf("insert scope-c: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scope_generations (generation_id, scope_id, observed_at, ingested_at, status, trigger_kind, freshness_hint)
		 VALUES ('gen-c1','scope-c',$1,$2,'failed','scheduled','')`,
		observed, ingested); err != nil {
		t.Fatalf("insert failed generation gen-c1: %v", err)
	}

	// scope-a carries two facts in the active generation; the per-generation
	// collapse must yield one enumerated work. gen-a0's lingering fact must not
	// produce a second work for scope-a.
	factRows := []struct{ factID, scopeID, gen string }{
		{"fact-a1", "scope-a", "gen-a1"},
		{"fact-a2", "scope-a", "gen-a1"},
		{"fact-a0", "scope-a", "gen-a0"},
		{"fact-b1", "scope-b", "gen-b1"},
		{"fact-c1", "scope-c", "gen-c1"},
	}
	for _, f := range factRows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO fact_records (fact_id, scope_id, generation_id) VALUES ($1,$2,$3)`,
			f.factID, f.scopeID, f.gen); err != nil {
			t.Fatalf("insert fact %s: %v", f.factID, err)
		}
	}
}
