// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestGenerationRetentionProbeMatchesGroupedPassLive proves the #7279 probe
// statements are exact: on the migrated schema they report the same
// per-(generation, table) row counts and delete the same content, reference,
// file and infra-mirror rows as the frozen #6809 grouped-pass statements.
//
// The corpus carries every protection edge: a cross-scope holder (a mirror
// scope's active generation names repo-x1 keys), retained tombstones that must
// not protect, duplicate candidate facts for one key, empty and missing key
// fields, keys shared by several candidates (attribution ties), and candidate
// subsets and orders like the ones the row-limit skip-and-recount path passes.
func TestGenerationRetentionProbeMatchesGroupedPassLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	corpus := retentionProbeCorpus{prefix: "x", scopes: 4, superseded: 4, entities: 120, files: 30, filler: 10, old: true}
	seedRetentionProbeCorpus(t, ctx, database, corpus)
	seedRetentionProbeEdges(t, ctx, database)

	all := retentionProbeCandidates(corpus, 3)
	reversed := slices.Clone(all)
	slices.Reverse(reversed)
	for _, tc := range []struct {
		name       string
		candidates []string
	}{
		{"all-oldest-first", all},
		{"reversed", reversed},
		{"recount-subset", all[:6]},
		{"single", all[:1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probeCounts := retentionCountsWith(t, ctx, database, generationRetentionRowCountsQuery, tc.candidates)
			groupedCounts := retentionCountsWith(t, ctx, database, legacyGenerationRetentionRowCountsQuery, tc.candidates)
			// The frozen oracle covers the 13 #6809 tables; the probe
			// covers those plus the #7396 cascade children and the #7700
			// evidence grandchild. Equality holds on the oracle's tables;
			// the pair count pins the probe's full 29-table coverage
			// (13 + 15 + 1).
			legacyTables := map[string]bool{}
			for key := range groupedCounts {
				if i := strings.LastIndexByte(key, '|'); i >= 0 {
					legacyTables[key[i+1:]] = true
				}
			}
			probeLegacy := map[string]int64{}
			for key, n := range probeCounts {
				if i := strings.LastIndexByte(key, '|'); i >= 0 {
					if legacyTables[key[i+1:]] {
						probeLegacy[key] = n
					}
				}
			}
			if !maps.Equal(probeLegacy, groupedCounts) {
				t.Errorf("row counts differ:\n probe   %v\n grouped %v", probeLegacy, groupedCounts)
			}
			if len(groupedCounts) != len(tc.candidates)*13 {
				t.Errorf("oracle covers %d (generation, table) pairs, want %d", len(groupedCounts), len(tc.candidates)*13)
			}
			if len(probeCounts) != len(tc.candidates)*29 {
				t.Errorf("row counts cover %d (generation, table) pairs, want %d", len(probeCounts), len(tc.candidates)*29)
			}
			probeKept := retentionPruneSurvivors(t, ctx, database, tc.candidates, [3]string{
				pruneContentFileReferencesForGenerationsQuery,
				pruneContentEntitiesForGenerationsQuery,
				pruneContentFilesForGenerationsQuery,
			})
			groupedKept := retentionPruneSurvivors(t, ctx, database, tc.candidates, [3]string{
				legacyPruneContentFileReferencesForGenerationsQuery,
				legacyPruneContentEntitiesForGenerationsQuery,
				legacyPruneContentFilesForGenerationsQuery,
			})
			for table, keys := range groupedKept {
				if !slices.Equal(probeKept[table], keys) {
					t.Errorf("%s survivors differ: probe kept %d rows, grouped kept %d", table, len(probeKept[table]), len(keys))
				}
			}
			// The edges must bite: the full batch deletes rows of every table.
			total := retentionCountTotals(probeCounts)
			if len(tc.candidates) == len(all) &&
				(total["content_entities"] == 0 || total["content_files"] == 0 || total["infra_resource_entities"] == 0) {
				t.Errorf("corpus deletes nothing for %s: %v", tc.name, total)
			}
		})
	}
}

// seedRetentionProbeEdges adds the protection edge cases to the "x" corpus.
func seedRetentionProbeEdges(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	// A mirror scope whose active generation holds repo-x1 keys that otherwise
	// only candidates hold: a cross-scope holder must protect them.
	seedRetentionProbeCorpus(t, ctx, database, retentionProbeCorpus{prefix: "mirror", scopes: 1})
	insertEdgeFacts := `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1 || '/' || $3 || '/' || k || '/' || $6, g.scope_id, g.generation_id, $3, $1 || '/' || $3 || '/' || k || '/' || $6,
    'git', $1 || '/' || $3 || '/' || k || '/' || $6, now(), now(), $4,
    jsonb_strip_nulls(jsonb_build_object('repo_id', $2::text, 'entity_id', 'e-' || $5 || '-' || k, 'relative_path', 'p-' || k))
FROM scope_generations AS g, generate_series(0, 20) k WHERE g.generation_id = $1`
	for _, kind := range []string{"content_entity", "file"} {
		// mirror1-act names repo-x1 keys 0..20: they stay.
		execRetentionSeed(t, ctx, database, insertEdgeFacts, "mirror1-act", "repo-x1", kind, false, "x1", "mirror")
		// x2's active generation tombstones repo-x2 keys 0..20: they still go.
		execRetentionSeed(t, ctx, database, insertEdgeFacts, "x2-act", "repo-x2", kind, true, "x2", "tomb")
		// x3-g0 names its own keys twice: one content row, one delete.
		execRetentionSeed(t, ctx, database, insertEdgeFacts, "x3-g0", "repo-x3", kind, false, "x3", "dup")
	}
	// Empty and missing key fields in a candidate never select a row, even the
	// content rows stored behind an empty key.
	execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'edge-empty/' || kind || '/' || n, 'x4', 'x4-g0', kind, 'edge-empty/' || kind || '/' || n, 'git',
    'edge-empty/' || kind || '/' || n, now(), now(), payload
FROM (VALUES ('content_entity'), ('file')) AS kinds(kind),
     (VALUES (1, '{"repo_id":"repo-x4","entity_id":"","relative_path":""}'::jsonb),
             (2, '{"repo_id":"","entity_id":"e-x4-0","relative_path":"p-0"}'::jsonb),
             (3, '{"entity_id":"e-x4-1","relative_path":"p-1"}'::jsonb)) AS edge(n, payload)`)
	execRetentionSeed(t, ctx, database, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-x4', '', 'x', 'h', 1, now()), ('', 'p-0', 'x', 'h', 1, now())`)
}

// retentionCountsWith runs a row-count statement in a rolled-back transaction
// and returns its rows keyed "generation|table".
func retentionCountsWith(t *testing.T, ctx context.Context, database *sql.DB, statement string, candidates []string) map[string]int64 {
	t.Helper()
	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin count: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, statement, candidates)
	if err != nil {
		t.Fatalf("row count: %v", err)
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int64{}
	for rows.Next() {
		var generation, table string
		var n int64
		if err := rows.Scan(&generation, &table, &n); err != nil {
			t.Fatalf("scan row count: %v", err)
		}
		counts[generation+"|"+table] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row count: %v", err)
	}
	return counts
}

// retentionCountTotals sums "generation|table" counts per table.
func retentionCountTotals(counts map[string]int64) map[string]int64 {
	totals := map[string]int64{}
	for key, n := range counts {
		for i := len(key) - 1; i >= 0; i-- {
			if key[i] == '|' {
				totals[key[i+1:]] += n
				break
			}
		}
	}
	return totals
}

// retentionPruneSurvivors runs the three content prunes (refs, entities,
// files, in production order) and the infra orphan delete in a rolled-back
// transaction, and returns the sorted keys left in each table.
func retentionPruneSurvivors(t *testing.T, ctx context.Context, database *sql.DB, candidates []string, prunes [3]string) map[string][]string {
	t.Helper()
	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin prune: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for i, statement := range prunes {
		if _, err := tx.ExecContext(ctx, statement, candidates); err != nil {
			t.Fatalf("prune %d: %v", i, err)
		}
		if i == 1 {
			if _, err := inventory.DeleteOrphanedRows(ctx, tx, candidates); err != nil {
				t.Fatalf("infra orphan delete: %v", err)
			}
		}
	}
	survivors := map[string][]string{}
	for table, query := range map[string]string{
		"content_entities":        `SELECT repo_id || '|' || entity_id FROM content_entities`,
		"content_files":           `SELECT repo_id || '|' || relative_path FROM content_files`,
		"content_file_references": `SELECT repo_id || '|' || relative_path || '|' || reference_value FROM content_file_references`,
		"infra_resource_entities": `SELECT repo_id || '|' || entity_id FROM infra_resource_entities`,
	} {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		var keys []string
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s: %v", table, err)
			}
			keys = append(keys, key)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		_ = rows.Close()
		slices.Sort(keys)
		survivors[table] = keys
	}
	return survivors
}
