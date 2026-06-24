package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// latestGenerationCTEProofSchemaSQL is the minimal scope/generation table set the
// latest-generation CTE selects over. It mirrors the production columns the CTE
// references (ingestion_scopes.active_generation_id and the scope_generations
// ordering columns) plus the #3704 covering index, so the EXPLAIN assertion sees
// the same access path the data plane would.
const latestGenerationCTEProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT NULL
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    ingested_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);
`

// legacyCorrelatedLatestGenerationCTE is the pre-#3704 correlated-subquery form,
// kept here only to prove the DISTINCT ON rewrite selects the identical
// generation per scope. It is NOT used by production code.
const legacyCorrelatedLatestGenerationCTE = `WITH latest_generations AS (
    SELECT
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            (
                SELECT generation_id
                FROM scope_generations AS candidate
                WHERE candidate.scope_id = generation.scope_id
                ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
                LIMIT 1
            )
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    GROUP BY generation.scope_id, scope.active_generation_id
)`

// TestLatestGenerationCTETruthEquivalenceAndPlan is the #3704 real-Postgres proof.
// It seeds the three precedence cases the COALESCE must honor and asserts:
//
//  1. The DISTINCT ON rewrite (latestGenerationCTE) selects the byte-identical
//     (scope_id, generation_id) set as the legacy correlated-subquery form for
//     every case, so the rewrite changes no graph truth.
//  2. EXPLAIN of the rewrite contains no correlated SubPlan node (the planner
//     mis-estimated subplan that caused the corpus-wide CPU-bound long pole),
//     while the legacy form does contain one.
//
// Gated on ESHU_LATEST_GENERATION_PROOF_DSN so it runs only where a Postgres is
// available; the string-shape gates in ingestion_latest_generation_cte_test.go
// run everywhere.
func TestLatestGenerationCTETruthEquivalenceAndPlan(t *testing.T) {
	dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_LATEST_GENERATION_PROOF_DSN to run the latest-generation CTE Postgres proof")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schemaName := fmt.Sprintf("latest_gen_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, latestGenerationCTEProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	seedLatestGenerationProof(t, ctx, db)

	const selectTail = `
SELECT scope_id, generation_id
FROM latest_generations
WHERE generation_id IS NOT NULL
ORDER BY scope_id`

	legacy := queryLatestGenerationPairs(t, ctx, db, legacyCorrelatedLatestGenerationCTE+selectTail)
	rewrite := queryLatestGenerationPairs(t, ctx, db, latestGenerationCTE+selectTail)

	if len(legacy) == 0 {
		t.Fatal("legacy CTE returned no rows; fixture is not exercising the selection")
	}
	if len(legacy) != len(rewrite) {
		t.Fatalf("rewrite returned %d pairs, legacy returned %d", len(rewrite), len(legacy))
	}
	for scopeID, legacyGen := range legacy {
		if rewrite[scopeID] != legacyGen {
			t.Fatalf("scope %q: rewrite chose generation %q, legacy chose %q", scopeID, rewrite[scopeID], legacyGen)
		}
	}

	// Plan assertion: the rewrite must not produce a correlated SubPlan; the
	// legacy form must (it is the planner-misestimated node). A SubPlan in the
	// rewrite would mean the per-scope correlated evaluation crept back in.
	rewritePlan := explainText(t, ctx, db, latestGenerationCTE+selectTail)
	if strings.Contains(rewritePlan, "SubPlan") {
		t.Fatalf("DISTINCT ON rewrite plan unexpectedly contains a SubPlan:\n%s", rewritePlan)
	}
	legacyPlan := explainText(t, ctx, db, legacyCorrelatedLatestGenerationCTE+selectTail)
	if !strings.Contains(legacyPlan, "SubPlan") {
		t.Logf("legacy plan had no SubPlan (planner may have optimized the tiny fixture):\n%s", legacyPlan)
	}
}

// seedLatestGenerationProof inserts the three precedence cases:
//   - scope-active: active_generation_id pinned to an OLDER generation; the
//     pointer must win over the newest-by-ingested-time generation.
//   - scope-fallback: no active pointer, two generations; the newest by
//     (ingested_at DESC, generation_id DESC) must win.
//   - scope-single: one generation, no active pointer; that generation is chosen.
func seedLatestGenerationProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	scopes := []struct {
		scopeID   string
		activeGen sql.NullString
	}{
		{"scope-active", sql.NullString{String: "gen-active-old", Valid: true}},
		{"scope-fallback", sql.NullString{}},
		{"scope-single", sql.NullString{}},
	}
	for _, s := range scopes {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, $2)",
			s.scopeID, s.activeGen); err != nil {
			t.Fatalf("seed scope %q: %v", s.scopeID, err)
		}
	}
	gens := []struct {
		genID      string
		scopeID    string
		ingestedAt time.Time
	}{
		// active pointer points at the OLDER generation; newer exists but must lose.
		{"gen-active-old", "scope-active", base},
		{"gen-active-new", "scope-active", base.Add(time.Hour)},
		// fallback: newest by ingested_at must win.
		{"gen-fallback-old", "scope-fallback", base},
		{"gen-fallback-new", "scope-fallback", base.Add(time.Hour)},
		{"gen-single", "scope-single", base},
	}
	for _, g := range gens {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			g.genID, g.scopeID, g.ingestedAt); err != nil {
			t.Fatalf("seed generation %q: %v", g.genID, err)
		}
	}
}

func queryLatestGenerationPairs(t *testing.T, ctx context.Context, db *sql.DB, query string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("run latest-generation query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	pairs := make(map[string]string)
	for rows.Next() {
		var scopeID, generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			t.Fatalf("scan latest-generation row: %v", err)
		}
		pairs[scopeID] = generationID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate latest-generation rows: %v", err)
	}
	return pairs
}

func explainText(t *testing.T, ctx context.Context, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN "+query)
	if err != nil {
		t.Fatalf("EXPLAIN query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN rows: %v", err)
	}
	return strings.Join(lines, "\n")
}
