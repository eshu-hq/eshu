// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestInvestigateCodeTopicEntityNameFirstPostgresLive proves the #7033
// name-first entity split against disposable PostgreSQL. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN to an administrative postgres-database
// DSN and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
//
// Worst-case shape: 2,000 filler rows with TOAST-sized source_cache values
// take early heap; the rare term zephyr matches only three deep rows; the
// capped term quasar matches 300 early source-only rows plus five deep name
// rows. Sixteen terms force the 250-row per-term cap.
func TestInvestigateCodeTopicEntityNameFirstPostgresLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}

	if _, err := database.ExecContext(ctx, `
INSERT INTO content_entities
  (entity_id, repo_id, relative_path, entity_type, entity_name,
   start_line, end_line, language, source_cache, indexed_at)
SELECT 'filler-' || i,
       'repo-tail', 'src/filler-' || lpad(i::text, 5, '0') || '.go', 'Function',
       'filler_' || lpad(i::text, 5, '0'),
       1, 10, 'go',
       'filler payload ' || i || ' ' || repeat('xq', 8000),
       now()
FROM generate_series(1, 2000) AS i
`); err != nil {
		t.Fatalf("seed filler rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_entities
  (entity_id, repo_id, relative_path, entity_type, entity_name,
   start_line, end_line, language, source_cache, indexed_at)
SELECT 'quasar-src-' || i,
       'repo-tail', 'src/quasar-' || lpad(i::text, 5, '0') || '.go', 'Function',
       'widget_' || lpad(i::text, 5, '0'),
       1, 10, 'go',
       'quasar power cell ' || i || ' ' || repeat('qw', 4000),
       now()
FROM generate_series(1, 300) AS i
`); err != nil {
		t.Fatalf("seed early source-only quasar rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_entities
  (entity_id, repo_id, relative_path, entity_type, entity_name,
   start_line, end_line, language, source_cache, indexed_at)
VALUES
  ('quasar-coil-1', 'repo-tail', 'src/deep/coil-1.go', 'Function',
   'quasar_coil_alpha', 1, 10, 'go', 'a coil without the term', now()),
  ('quasar-coil-2', 'repo-tail', 'src/deep/coil-2.go', 'Function',
   'quasar_coil_beta', 1, 10, 'go', 'another coil without the term', now()),
  ('quasar-coil-3', 'repo-tail', 'src/deep/coil-3.go', 'Function',
   'quasar_coil_gamma', 1, 10, 'go', 'quasar wound coil gamma', now()),
  ('quasar-coil-4', 'repo-tail', 'src/deep/coil-4.go', 'Function',
   'quasar_coil_delta', 1, 10, 'go', 'plain delta winding', now()),
  ('quasar-coil-5', 'repo-tail', 'src/deep/coil-5.go', 'Function',
   'quasar_coil_epsilon', 1, 10, 'go', 'quasar wound coil epsilon', now()),
  ('zephyr-name', 'repo-tail', 'src/deep/zephyr-name.go', 'Function',
   'zephyr_gadget', 1, 10, 'go', 'a gadget without the term', now()),
  ('zephyr-src', 'repo-tail', 'src/deep/zephyr-src.go', 'Function',
   'plain_widget', 1, 10, 'go', 'zephyr drift compensator', now()),
  ('zephyr-both', 'repo-tail', 'src/deep/zephyr-both.go', 'Function',
   'zephyr_engine', 1, 10, 'go', 'zephyr drive manifold', now())
`); err != nil {
		t.Fatalf("seed deep match rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `ANALYZE content_entities`); err != nil {
		t.Fatalf("analyze content_entities: %v", err)
	}

	terms := []string{
		"quasar", "zephyr",
		"zznohit01", "zznohit02", "zznohit03", "zznohit04",
		"zznohit05", "zznohit06", "zznohit07", "zznohit08",
		"zznohit09", "zznohit10", "zznohit11", "zznohit12",
		"zznohit13", "zznohit14",
	}
	reader := NewContentReader(database)
	rows, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		Terms: terms, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v", err)
	}

	byEntity := make(map[string]codequery.CodeTopicEvidenceRow, len(rows))
	for _, row := range rows {
		if row.SourceKind != "entity" {
			continue
		}
		byEntity[row.EntityID] = row
	}

	// Uncapped rare term: every deep match is retained with its exact term.
	for _, want := range []string{"zephyr-name", "zephyr-src", "zephyr-both"} {
		got, ok := byEntity[want]
		if !ok {
			t.Fatalf("missing uncapped zephyr row %s in %d entity rows", want, len(byEntity))
		}
		if len(got.MatchedTerms) != 1 || got.MatchedTerms[0] != "zephyr" {
			t.Fatalf("row %s matched terms = %v, want [zephyr]", want, got.MatchedTerms)
		}
		// No per-row uncapped assertion here: pool_truncated is a
		// statement-level flag, and the over-cap quasar pool marks every
		// row of this 16-term request. The oracle multiset comparison below
		// proves the zephyr pool itself is uncapped (3 rows under the cap).
	}

	// Capped term: the pool holds exactly the 250-row cap and stays marked,
	// and the deep name matches win the bounded pool over early heap-order
	// source rows. The old unordered lateral cannot satisfy this: its first
	// 250 heap-order matches exclude every deep name row.
	quasarRows := 0
	for _, row := range byEntity {
		for _, term := range row.MatchedTerms {
			if term == "quasar" {
				quasarRows++
			}
		}
	}
	if quasarRows != 250 {
		t.Fatalf("quasar pool rows = %d, want 250 (exact per-term cap)", quasarRows)
	}
	for _, want := range []string{"quasar-coil-1", "quasar-coil-2", "quasar-coil-3", "quasar-coil-4", "quasar-coil-5"} {
		if _, ok := byEntity[want]; !ok {
			t.Fatalf("missing deep name row %s: name-first pool must prioritize name matches", want)
		}
	}
	capped := false
	for _, row := range rows {
		capped = capped || row.PoolTruncated
	}
	if !capped {
		t.Fatal("pool truncated = false, want capped marker for the over-cap quasar pool")
	}

	// Differential against the current OR shape: an independently written
	// unordered lateral with the shipped predicate must return the identical
	// zephyr multiset when the pool is uncapped.
	oracleRows, err := database.QueryContext(ctx, `
SELECT m.entity_id
FROM (VALUES ('zephyr')) AS terms(term)
CROSS JOIN LATERAL (
  SELECT e.entity_id
  FROM content_entities e
  WHERE (e.entity_name ILIKE '%' || terms.term || '%'
         OR e.source_cache ILIKE '%' || terms.term || '%')
  LIMIT 250
) m
`)
	if err != nil {
		t.Fatalf("oracle lateral: %v", err)
	}
	defer func() { _ = oracleRows.Close() }()
	var oracle []string
	for oracleRows.Next() {
		var id string
		if err := oracleRows.Scan(&id); err != nil {
			t.Fatalf("scan oracle row: %v", err)
		}
		oracle = append(oracle, id)
	}
	if err := oracleRows.Err(); err != nil {
		t.Fatalf("oracle rows: %v", err)
	}
	var candidate []string
	for id, row := range byEntity {
		for _, term := range row.MatchedTerms {
			if term == "zephyr" {
				candidate = append(candidate, id)
			}
		}
	}
	sort.Strings(oracle)
	sort.Strings(candidate)
	if strings.Join(oracle, ",") != strings.Join(candidate, ",") {
		t.Fatalf("zephyr multiset drift:\noracle:    %v\ncandidate: %v", oracle, candidate)
	}
}
