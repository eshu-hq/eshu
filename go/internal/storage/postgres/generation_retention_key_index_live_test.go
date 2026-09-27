// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// retentionKeyIndexPolicy prunes every superseded generation older than an hour.
var retentionKeyIndexPolicy = GenerationRetentionPolicy{
	MinSupersededGenerations: 0,
	MaxSupersededAge:         time.Hour,
	BatchGenerationLimit:     100,
	BatchRowLimit:            10_000_000,
	PolicyScope:              "global",
	PolicyRevision:           "7279-key-index",
}

// TestGenerationRetentionRefusesWithoutValidKeyIndexLive proves the #7279
// precondition on the real catalog: a dropped key index, an invalid one (the
// state a failed CREATE INDEX CONCURRENTLY leaves behind), and a valid index of
// the right name but the wrong shape each refuse the cycle before any scope is
// locked or any row deleted. Restoring the real index lets the next cycle run.
func TestGenerationRetentionRefusesWithoutValidKeyIndexLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	corpus := retentionProbeCorpus{prefix: "r", scopes: 2, superseded: 2, entities: 20, files: 10, filler: 2, old: true}
	seedRetentionProbeCorpus(t, ctx, database, corpus)
	const fileKeyIndex = `CREATE INDEX CONCURRENTLY fact_records_file_key_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'relative_path'))
    WHERE fact_kind = 'file' AND is_tombstone = FALSE`

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	for _, tc := range []struct {
		name  string
		setup []string
	}{
		{"dropped", []string{"DROP INDEX fact_records_file_key_idx"}},
		{"invalid", []string{
			// Duplicate keys make the unique concurrent build fail after it has
			// created the catalog entry, leaving indisvalid = false.
			"DROP INDEX fact_records_file_key_idx",
			strings.Replace(fileKeyIndex, "CREATE INDEX", "CREATE UNIQUE INDEX", 1),
		}},
		{"wrong-shape", []string{
			"DROP INDEX fact_records_file_key_idx",
			`CREATE INDEX fact_records_file_key_idx ON fact_records ((payload->>'repo_id')) WHERE fact_kind = 'file'`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, statement := range tc.setup {
				_, err := database.ExecContext(ctx, statement)
				// The invalid case's unique build is meant to fail.
				expectedFailure := tc.name == "invalid" && i == 1
				if err != nil && !expectedFailure {
					t.Fatalf("setup %q: %v", statement, err)
				}
			}
			generationsBefore := retentionRowCount(t, ctx, database, "scope_generations")
			_, err := store.PruneSupersededGenerations(ctx, retentionKeyIndexPolicy)
			if !errors.Is(err, ErrGenerationRetentionKeyIndexUnavailable) {
				t.Fatalf("PruneSupersededGenerations() error = %v, want ErrGenerationRetentionKeyIndexUnavailable", err)
			}
			if got := retentionRowCount(t, ctx, database, "scope_generations"); got != generationsBefore {
				t.Fatalf("refused cycle changed scope_generations from %d to %d rows", generationsBefore, got)
			}
			// The refused cycle left no scope locked.
			if _, err := database.ExecContext(ctx, "SELECT 1 FROM ingestion_scopes FOR UPDATE NOWAIT"); err != nil {
				t.Fatalf("scope rows still locked after refusal: %v", err)
			}
			execRetentionSeed(t, ctx, database, "DROP INDEX IF EXISTS fact_records_file_key_idx")
			execRetentionSeed(t, ctx, database, fileKeyIndex)
		})
	}
	result, err := store.PruneSupersededGenerations(ctx, retentionKeyIndexPolicy)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() after restoring the index: %v", err)
	}
	if result.GenerationsPruned != corpus.scopes*corpus.superseded {
		t.Fatalf("GenerationsPruned = %d, want %d once the index is valid", result.GenerationsPruned, corpus.scopes*corpus.superseded)
	}
}

// TestGenerationRetentionScopeMismatchedFactNeverOverDeletesLive pins the leak
// direction of the scope-join invariant (#7279). Candidate facts are read
// through (scope_id, generation_id), so a fact whose scope_id differs from its
// generation's scope is invisible as a candidate: the key only it names keeps
// its content row (a bounded leak) and the fact_records count omits it. The
// retained-holder probe stays global by generation_id, so a mismatched fact in a
// kept generation still protects its key. Neither direction over-deletes.
func TestGenerationRetentionScopeMismatchedFactNeverOverDeletesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	corpus := retentionProbeCorpus{prefix: "s", scopes: 2, superseded: 1, entities: 5, files: 0, filler: 0, old: true}
	seedRetentionProbeCorpus(t, ctx, database, corpus)
	insertMismatched := `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 's2', $2, 'content_entity', $1, 'git', $1, now(), now(),
    jsonb_build_object('repo_id', 'repo-s1', 'entity_id', $3::text))`
	// "leaked": named only by a candidate-generation fact filed under scope s2.
	execRetentionSeed(t, ctx, database, insertMismatched, "mismatch-leaked", "s1-g0", "e-leaked")
	// "kept": named by a normal candidate fact and by a kept-generation fact
	// filed under scope s2.
	execRetentionSeed(t, ctx, database, insertMismatched, "mismatch-kept", "s1-act", "e-kept")
	execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ('normal-kept', 's1', 's1-g0', 'content_entity', 'normal-kept', 'git', 'normal-kept', now(), now(),
    '{"repo_id":"repo-s1","entity_id":"e-kept"}'::jsonb)`)
	for _, entity := range []string{"e-leaked", "e-kept"} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
VALUES ($1, 'repo-s1', $1, 'Function', 'n', 1, 2, 'x', now())`, entity)
	}
	// s1-g0 carries entity keys 0..4 plus normal-kept, 6 facts, and the
	// mismatched fact the cascade also removes.
	factsBefore := retentionRowCount(t, ctx, database, "fact_records")

	result, err := NewGenerationRetentionStore(SQLDB{DB: database}).PruneSupersededGenerations(ctx, retentionKeyIndexPolicy)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	for entity, want := range map[string]int64{"e-leaked": 1, "e-kept": 1} {
		var n int64
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM content_entities WHERE entity_id = $1", entity).Scan(&n); err != nil {
			t.Fatalf("read %s: %v", entity, err)
		}
		if n != want {
			t.Errorf("content_entities %s rows = %d, want %d", entity, n, want)
		}
	}
	removed := factsBefore - retentionRowCount(t, ctx, database, "fact_records")
	// Two candidates (s1-g0, s2-g0), 6 + 5 scope-matched facts counted; the
	// cascade also removes the one mismatched candidate fact the count omits.
	if got := result.RowsPruned["fact_records"]; got != removed-1 || removed != 12 {
		t.Errorf("fact_records counted %d, removed %d; want 11 counted and 12 removed (one mismatched fact uncounted)", got, removed)
	}
}

func retentionRowCount(t *testing.T, ctx context.Context, database *sql.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
