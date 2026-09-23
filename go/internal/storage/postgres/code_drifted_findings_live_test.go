// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	codedivergence "github.com/eshu-hq/eshu/go/internal/reducer/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// openDriftedLiveDB opens an isolated-schema Postgres exactly like the #5237
// file-kind gate proof: the shipped drifted statement is unqualified, so the
// schema rides in the DSN search_path and drops with the test.
func openDriftedLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live drifted nil-window proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	schemaName := fmt.Sprintf("drifted_6837_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA "+array.QuoteIdentifier(schemaName)+" CASCADE",
		)
	})

	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	params := targetURL.Query()
	params.Set("search_path", schemaName)
	targetURL.RawQuery = params.Encode()
	db, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated Postgres schema: %v", err)
	}

	required := map[string]bool{
		"ingestion_scopes":                   true,
		"scope_generations":                  true,
		"fact_records":                       true,
		"content_store":                      true,
		"code_function_fingerprint":          true,
		"code_function_fingerprint_shingles": true,
	}
	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		if required[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != len(required) {
		t.Fatalf("isolated schema definitions = %d, want %d", len(definitions), len(required))
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("apply isolated Postgres schema: %v", err)
	}
	return ctx, db
}

// seedDriftedLiveScope seeds one repository scope with a superseded gen-0
// and an active gen-1. gen-0 is superseded because only one active
// generation per scope is allowed (scope_generations_active_scope_idx),
// and the shipped statement only reads the scope's active generation.
func seedDriftedLiveScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, repoKey string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'eshu', $2, 'test', $2, now(), now(), 'active', 'gen-1')`, scopeID, repoKey); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	for _, gen := range []struct{ id, status string }{{"gen-0", "superseded"}, {"gen-1", "active"}} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'test', now(), now(), $3)`, gen.id, scopeID, gen.status); err != nil {
			t.Fatalf("seed generation %s: %v", gen.id, err)
		}
	}
}

func seedDriftedLiveFacts(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	const scopeID = "repo:repo-1"
	seedDriftedLiveScope(t, ctx, db, scopeID, "repo-1")

	insert := func(factID, kind, gen, payload string, tombstone bool) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'eshu', $1, now(), now(), $5, $6::jsonb)`,
			factID, scopeID, gen, kind, tombstone, payload); err != nil {
			t.Fatalf("seed fact %s: %v", factID, err)
		}
	}
	const driftedKind = facts.ReducerCodeDriftedFindingFactKind
	insert("fact-live-a", driftedKind, "gen-1", driftedFindingPayloadJSON(t, "find-live-a", 210, 190), false)
	insert("fact-live-b", driftedKind, "gen-1", driftedFindingPayloadJSON(t, "find-live-b", 300, 300), false)
	insert("fact-live-tomb", driftedKind, "gen-1", driftedFindingPayloadJSON(t, "find-live-tomb", 500, 500), true)
	insert("fact-live-old", driftedKind, "gen-0", driftedFindingPayloadJSON(t, "find-live-old", 500, 500), false)
	insert("fact-live-other", "code_exact_finding", "gen-1", `{"repo_id":"repo-1","finding_id":"other-1"}`, false)
}

// TestDriftedFindingStatsNilWindowLive executes the shipped statement the
// read surface actually runs -- DriftedFindingStats binds a nil window --
// against real Postgres. The nil window must read every active drifted row
// (the merged findings page ranks on these stats); a NULL-bound window that
// drops all rows makes the drifted kind invisible.
func TestDriftedFindingStatsNilWindowLive(t *testing.T) {
	ctx, db := openDriftedLiveDB(t)
	seedDriftedLiveFacts(t, ctx, db)
	store := PostgresCodeDriftedFindingStore{DB: SQLDB{DB: db}}

	stats, err := store.DriftedFindingStats(ctx, "repo-1")
	if err != nil {
		t.Fatalf("DriftedFindingStats() error = %v, want nil", err)
	}
	if len(stats) != 2 {
		t.Fatalf("DriftedFindingStats() = %d rows, want 2 active drifted rows (tombstoned, superseded-generation, and other-kind rows excluded)", len(stats))
	}

	rows, err := store.DriftedFindingRows(ctx, "repo-1", []string{"find-live-a"})
	if err != nil {
		t.Fatalf("DriftedFindingRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("DriftedFindingRows(window) = %d rows, want 1", len(rows))
	}
}

// driftedConvMember builds one synthetic admitted member: 64 tokens (above
// the floor), a production-shaped Go path (no generated/vendored/test
// markers), and the caller's shingle set.
func driftedConvMember(entity, path string, shingles []uint64) codedivergence.MemberRow {
	return codedivergence.MemberRow{
		EntityID: entity, EntityName: "big", EntityType: "Function",
		RelativePath: path, Language: "go",
		StartLine: 10, EndLine: 40, TokenCount: 64,
		Shingles: shingles, FPExact: "exact-" + entity, FPRenamed: "renamed-" + entity,
	}
}

// driftedConvPair admits one synthetic pair through the production verifier
// (Jaccard 0.8: inter 8, union 10), so the convergence proof runs the same
// admission the handler runs.
func driftedConvPair(t *testing.T, a, b codedivergence.MemberRow) codedivergence.AdmittedPair {
	t.Helper()

	admitted, suppressed, reason := codedivergence.ApplyRules(codedivergence.CandidatePair{A: a, B: b, SharedBands: 9})
	if suppressed {
		t.Fatalf("convergence pair suppressed under %q, want admitted", reason)
	}
	return admitted
}

// driftedConvSurvivors returns the finding ids of every drifted fact row
// under one (scope, generation).
func driftedConvSurvivors(t *testing.T, ctx context.Context, db *sql.DB, scopeID string) map[string]bool {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT fact.payload->>'finding_id'
FROM fact_records AS fact
WHERE fact.fact_kind = $1 AND fact.scope_id = $2 AND fact.generation_id = 'gen-1'`,
		facts.ReducerCodeDriftedFindingFactKind, scopeID)
	if err != nil {
		t.Fatalf("read survivors: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var findingID string
		if err := rows.Scan(&findingID); err != nil {
			t.Fatalf("scan survivor: %v", err)
		}
		out[findingID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("survivor rows: %v", err)
	}
	return out
}

// TestDriftedPassRetireConvergesLive runs two full production passes with
// different loader views through the shipped writer against real Postgres —
// pass A admits {P1, P2}, pass B admits {P2, P3} — and proves the
// last-retire-wins invariant the no-transaction record in writer.go rests
// on: after both passes settle, the table holds exactly the last pass's
// complete keep set, never a partial mix. A third pass re-asserting A's
// view proves the same in the other direction.
func TestDriftedPassRetireConvergesLive(t *testing.T) {
	ctx, db := openDriftedLiveDB(t)
	const scopeID = "repo:conv-1"
	seedDriftedLiveScope(t, ctx, db, scopeID, "conv-1")
	writer := codedivergence.PostgresCodeDriftedWriter{DB: SQLDB{DB: db}}

	base := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}
	alt := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 101}
	p1 := driftedConvPair(t,
		driftedConvMember("e1", "a.go", base),
		driftedConvMember("e2", "b.go", alt))
	p2 := driftedConvPair(t,
		driftedConvMember("e3", "c.go", base),
		driftedConvMember("e4", "d.go", alt))
	p3 := driftedConvPair(t,
		driftedConvMember("e5", "e.go", base),
		driftedConvMember("e6", "f.go", alt))

	pass := func(pairs ...codedivergence.AdmittedPair) {
		t.Helper()
		_, err := writer.WriteDriftedFindings(ctx, codedivergence.DriftedWrite{
			IntentID: "intent-conv", ScopeID: scopeID, GenerationID: "gen-1",
			RepoID: "conv-1", SourceSystem: "test", Cause: "test",
			Pairs: pairs, Suppressions: map[string]int{},
		})
		if err != nil {
			t.Fatalf("WriteDriftedFindings() error = %v, want nil", err)
		}
	}
	fid := func(p codedivergence.AdmittedPair) string {
		return codedivergence.DriftedFindingID("conv-1", p.Pair.A, p.Pair.B)
	}
	wantSet := func(pairs ...codedivergence.AdmittedPair) map[string]bool {
		out := map[string]bool{}
		for _, p := range pairs {
			out[fid(p)] = true
		}
		return out
	}
	assertSurvivors := func(want map[string]bool) {
		t.Helper()
		got := driftedConvSurvivors(t, ctx, db, scopeID)
		if len(got) != len(want) {
			t.Fatalf("survivors = %v, want exactly %v", got, want)
		}
		for id := range want {
			if !got[id] {
				t.Fatalf("survivors = %v, want exactly %v", got, want)
			}
		}
	}

	pass(p1, p2)
	assertSurvivors(wantSet(p1, p2))
	pass(p2, p3)
	assertSurvivors(wantSet(p2, p3))
	pass(p1, p2)
	assertSurvivors(wantSet(p1, p2))
}

// TestDriftedMembersExcludeNullShinglesLive proves the members query honors
// the loader's drop contract at the SQL level: a nominated entity whose
// fingerprint row lost its shingle set between the pairs read and the
// members read (exact-only-tier re-fingerprint NULLs shingles and
// fp_renamed together) must simply not return, so the pair drops with a
// no_shingles count. Without the predicate the NULL scan fails the whole
// load into queue retry.
func TestDriftedMembersExcludeNullShinglesLive(t *testing.T) {
	ctx, db := openDriftedLiveDB(t)
	const repoID = "repo-null"

	for _, ent := range []struct{ id, path string }{{"n1", "a.go"}, {"n2", "b.go"}} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, indexed_at)
VALUES ($1, $2, $3, 'Function', 'big', 10, 40, 'go', 'x', now())`, ent.id, repoID, ent.path); err != nil {
			t.Fatalf("seed entity %s: %v", ent.id, err)
		}
	}
	shinglesHex := fingerprint.EncodeShingles([]uint64{1, 2, 3, 4, 5, 6, 7, 8, 9})
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_function_fingerprint (entity_id, repo_id, fp_exact, fp_renamed, shingles, token_count, indexed_at)
VALUES ('n1', $1, 'exact-n1', 'renamed-n1', $2, 64, now()),
       ('n2', $1, 'exact-n2', NULL, NULL, 66, now())`, repoID, shinglesHex); err != nil {
		t.Fatalf("seed fingerprints: %v", err)
	}

	loader := PostgresCodeDriftedEvidenceLoader{DB: SQLDB{DB: db}}
	members, _, err := loader.loadMembers(ctx, repoID, []driftedPairRow{{e1: "n1", e2: "n2"}})
	if err != nil {
		t.Fatalf("loadMembers() error = %v, want nil (NULL-shingle row must not fail the load)", err)
	}
	if _, ok := members["n1"]; !ok {
		t.Fatal("loadMembers() dropped n1, want the shingled member present")
	}
	if _, ok := members["n2"]; ok {
		t.Fatal("loadMembers() returned n2, want the NULL-shingle row excluded so the pair drops per contract")
	}
}
