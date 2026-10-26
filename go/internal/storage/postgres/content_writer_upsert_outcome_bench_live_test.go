// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8950 disposable PostgreSQL 18 measurement of the content_files upsert outcome pre-read against the batch upsert it precedes, on a findings-heavy batch and on 500 small rows, for a new-commit rewrite and an identical replay; opt-in (ESHU_CONTENT_UPSERT_OUTCOME_BENCH=1), otherwise a one-round smoke that asserts only that the pre-read classifies the replay; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// outcomeBenchRows turns corpus records into prepared rows stamped with commit.
func outcomeBenchRows(repoID, commit string, records []content.Record) []preparedFileRow {
	rows := make([]preparedFileRow, 0, len(records))
	for _, rec := range records {
		hash, _ := fileContentHash(rec)
		row := upsertOutcomeRow(rec.Path, rec.Body, hash, commit, "go")
		row.repoID = repoID
		rows = append(rows, row)
	}
	return rows
}

// smallRowCorpus builds n small config-sized files.
func smallRowCorpus(n int) []content.Record {
	records := make([]content.Record, 0, n)
	for i := range n {
		var b strings.Builder
		for line := 0; b.Len() < 2048; line++ {
			fmt.Fprintf(&b, "key_%d_%d: value-%d\n", i, line, i*line)
		}
		records = append(records, content.Record{Path: fmt.Sprintf("conf/f%04d.yaml", i), Body: b.String()})
	}
	return records
}

// TestContentFileUpsertOutcomePreReadCostLive times the pre-read next to the
// batch upsert it precedes (the upsert_files stage work for one batch),
// interleaved on one database: a new-commit rewrite of every row, then an
// identical replay the guard skips. It logs medians and the overhead ratio and
// asserts only the classification, so it can be run on each side of a change.
// ESHU_CONTENT_UPSERT_OUTCOME_BENCH=1 runs the measurement corpora: 50 files
// of about 469 KB with 491 findings each, and batches of 500, 50, 20, 5 and 1
// rows of about 2 KB (the pre-read has a fixed per-batch cost, so the small
// batches every Write ends in cost the most). ESHU_CONTENT_UPSERT_OUTCOME_BENCH_ROUNDS
// overrides the round count of every corpus.
func TestContentFileUpsertOutcomePreReadCostLive(t *testing.T) {
	full := os.Getenv("ESHU_CONTENT_UPSERT_OUTCOME_BENCH") == "1"
	type corpus struct {
		name    string
		records []content.Record
		rounds  int
	}
	corpora := []corpus{{"findings_6x64KB", findingsHeavyCorpus(6, 64*1024, 60), 2}, {"small_50x2KB", smallRowCorpus(50), 2}}
	if full {
		corpora = []corpus{
			{"findings_50x469KB", findingsHeavyCorpus(50, 469*1024, 491), 15},
			{"small_500x2KB", smallRowCorpus(500), 30},
			{"small_50x2KB", smallRowCorpus(50), 60},
			{"small_20x2KB", smallRowCorpus(20), 60},
			{"small_5x2KB", smallRowCorpus(5), 60},
			{"small_1x2KB", smallRowCorpus(1), 60},
		}
		if v := os.Getenv("ESHU_CONTENT_UPSERT_OUTCOME_BENCH_ROUNDS"); v != "" {
			var override int
			if _, err := fmt.Sscanf(v, "%d", &override); err != nil || override < 1 {
				t.Fatalf("ESHU_CONTENT_UPSERT_OUTCOME_BENCH_ROUNDS=%q: want a positive integer", v)
			}
			for i := range corpora {
				corpora[i].rounds = override
			}
		}
	}
	ctx, database := openContentUpsertGuardDatabase(t)
	writer := NewContentWriter(SQLDB{DB: database})
	writer.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Background rows give the primary key a production-shaped depth, so the
	// pre-read plan and timing are not those of a near-empty table.
	background := 2000
	if full {
		background = 30000
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, commit_sha, content, content_hash, line_count, language, indexed_at, generation_id)
SELECT 'bg-' || (g % 300), 'bg/file' || g || '.go', 'c-bg', repeat('x', 4096) || g, 'bg-hash-' || g, 1, 'go', now(), 'gen-bg'
FROM generate_series(1, $1::int) AS g`, background); err != nil {
		t.Fatalf("background rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, "ANALYZE content_files"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	for _, c := range corpora {
		rounds := c.rounds
		repoID := "repo-8950-bench-" + c.name
		seed := outcomeBenchRows(repoID, "c-seed", c.records)
		if err := writer.upsertContentFileBatches(ctx, seed, writer.now(), "gen-0", nil); err != nil {
			t.Fatalf("%s seed: %v", c.name, err)
		}
		var plan []string
		keyRepos := make([]string, len(seed))
		keyPaths := make([]string, len(seed))
		for i, row := range seed {
			keyRepos[i], keyPaths[i] = row.repoID, row.path
		}
		rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+contentFileOutcomePreReadSQL, keyRepos, keyPaths)
		if err != nil {
			t.Fatalf("%s explain pre-read: %v", c.name, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			plan = append(plan, line)
		}
		_ = rows.Close()
		t.Logf("%s pre-read plan:\n%s", c.name, strings.Join(plan, "\n"))

		measure := func(label string, commit func(round int) string, want fileOutcome) {
			var pre, up []time.Duration
			for round := range rounds {
				batch := outcomeBenchRows(repoID, commit(round), c.records)
				start := time.Now()
				outcomes, err := writer.classifyFileBatch(ctx, batch, "gen-0")
				pre = append(pre, time.Since(start))
				if err != nil {
					t.Fatalf("%s %s classify: %v", c.name, label, err)
				}
				for i, got := range outcomes {
					if got != want {
						t.Fatalf("%s %s: %s classified %s, want %s", c.name, label, batch[i].path, got, want)
					}
				}
				start = time.Now()
				if err := writer.upsertContentFileBatches(ctx, batch, writer.now(), "gen-0", nil); err != nil {
					t.Fatalf("%s %s upsert: %v", c.name, label, err)
				}
				up = append(up, time.Since(start))
			}
			mp, mu := medianDuration(pre), medianDuration(up)
			t.Logf("BENCH %s %s rows=%d rounds=%d preread_median_ms=%.1f upsert_files_median_ms=%.1f overhead_pct=%.2f",
				c.name, label, len(c.records), rounds, float64(mp)/1e6, float64(mu)/1e6, 100*float64(mp)/float64(mu))
		}
		measure("rewrite_new_commit", func(round int) string { return fmt.Sprintf("c-%d", round) }, outcomeMetadataOnly)
		last := fmt.Sprintf("c-%d", rounds-1)
		measure("identical_replay", func(int) string { return last }, outcomeUnchangedSkipped)
	}
}
