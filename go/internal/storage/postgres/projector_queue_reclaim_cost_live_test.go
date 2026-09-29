// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Cost proof for #7388: the projector claim statement's two reclaim UPDATEs now
// fold the prior failure into failure_details. Like the #7320 write-cost test,
// it counts bytes written, which do not depend on host load, over the shipped
// claim statement with and without the fold. "Before" is derived from the
// shipped constant by reversing the change, so it cannot drift.
//
// Opt in with ESHU_7320_COST_PROOF=1 and the claim-proof DSN; the server needs
// pg_stat_statements preloaded, as for the #7320 proof. Knobs:
// ESHU_7388_COST_ROWS (500 rows reclaimed per run).

// reclaimFoldPattern matches the reclaim writers' failure_details assignment as
// shipped: a CASE that keeps an already reclaimed row's details and otherwise
// folds the prior failure into the reclaim's own keys.
var reclaimFoldPattern = regexp.MustCompile(`failure_details = CASE\s+WHEN stale\.failure_class = 'projector_stale_scope_reclaim' THEN stale\.failure_details\s+ELSE \((jsonb_build_object\([^)]*\)) \|\| ` + regexp.QuoteMeta(priorFailureStaleSQL) + `\)::text\s+END`)

// reclaimBeforeText returns the claim statement as it was before #7388 and how
// many reclaim assignments it rewrote.
func reclaimBeforeText(shipped string) (string, int) {
	n := len(reclaimFoldPattern.FindAllStringIndex(shipped, -1))
	return reclaimFoldPattern.ReplaceAllString(shipped, "failure_details = $1"), n
}

// costSeedReclaim seeds n scopes, each with an expired claimed row (carrying a
// retry's failure) beside a live lease on a newer generation, so one claim
// reclaims n expired duplicates.
func costSeedReclaim(t *testing.T, database *sql.DB, n, detailBytes int) {
	t.Helper()
	costReset(t, database)
	costExec(t, database,
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'scope-'||i, 'repository','git','scope-'||i,'git','scope-'||i, now(), now(), 'active' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g1','scope-'||i,'push', now()-interval '2 hours', now()-interval '2 hours','pending' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'scope-'||i||'-g2','scope-'||i,'push', now()-interval '1 hour', now()-interval '1 hour','active', now()-interval '1 hour' FROM generate_series(1,%d) i`, n),
		`UPDATE ingestion_scopes SET active_generation_id = scope_id || '-g2'`,
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, lease_owner, claim_until, failure_class, failure_message, failure_details, payload, created_at, updated_at)
SELECT 'projector_scope-'||i||'_g1','scope-'||i,'scope-'||i||'-g1','projector','source_local','claimed',3, now()-interval '90 minutes','cost-dead', now()-interval '1 minute','projection_retryable','retry', %s,'{}'::jsonb, now()-interval '2 hours', now()-interval '90 minutes' FROM generate_series(1,%d) i`, costDetailExpr(detailBytes), n),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, lease_owner, claim_until, payload, created_at, updated_at)
SELECT 'projector_scope-'||i||'_g2','scope-'||i,'scope-'||i||'-g2','projector','source_local','running',1, now()-interval '1 hour','cost-live', now()+interval '1 hour','{}'::jsonb, now()-interval '1 hour', now()-interval '1 hour' FROM generate_series(1,%d) i`, n),
		`VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`)
}

// TestReclaimBeforeTextReversesTheChange keeps the derivation honest without a
// database: it must find both reclaim assignments and leave the supersede folds
// alone.
func TestReclaimBeforeTextReversesTheChange(t *testing.T) {
	before, n := reclaimBeforeText(claimProjectorWorkQuery)
	if n != 2 {
		t.Fatalf("reclaim assignments rewritten = %d, want the two reclaim UPDATEs", n)
	}
	if got, want := strings.Count(before, priorFailureStaleSQL), strings.Count(claimProjectorWorkQuery, priorFailureStaleSQL)-2; got != want {
		t.Fatalf("folds left in the before text = %d, want %d (the supersede folds stay)", got, want)
	}
	if strings.Contains(before, "projector_stale_scope_reclaim' THEN stale.failure_details") {
		t.Fatal("the before text still carries the keep-details CASE")
	}
}

// TestReclaimPriorFailureWriteCost measures what the fold adds per reclaimed
// row: WAL bytes and records, pages dirtied and rows, at two details widths, in
// the steady regime and right after a checkpoint (the first touch of each page
// writes a full-page image).
func TestReclaimPriorFailureWriteCost(t *testing.T) {
	requireCostProof(t)
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	ctx := context.Background()
	after := claimProjectorWorkQuery
	before, rewritten := reclaimBeforeText(after)
	if rewritten != 2 {
		t.Fatalf("derived the before text from %d reclaim assignments, want 2", rewritten)
	}
	rows := costEnvInt("ESHU_7388_COST_ROWS", 500)
	const reps = 3
	for _, bytes := range []int{800, 4096} {
		for _, regime := range []string{"steady", "post_checkpoint"} {
			sums := map[string]*walSample{"before": {}, "after": {}}
			for rep := 0; rep < reps; rep++ {
				order := []string{"before", "after"}
				if rep%2 == 1 {
					order = []string{"after", "before"}
				}
				for _, variant := range order {
					costSeedReclaim(t, database, rows, bytes)
					text := after
					if variant == "before" {
						text = before
					}
					now := time.Now().UTC()
					s := walMeasure(ctx, t, database, text, []walCall{{[]any{now, "cost", now.Add(time.Minute), ""}}}, regime == "post_checkpoint")
					if int(s.reclaimed) != rows {
						t.Fatalf("%s %s: reclaimed %v rows, want %d", variant, regime, s.reclaimed, rows)
					}
					acc := sums[variant]
					acc.reclaimed += s.reclaimed
					acc.walRecords += s.walRecords
					acc.walBytes += s.walBytes
					acc.walFPI += s.walFPI
					acc.dirtied += s.dirtied
					acc.execMillis += s.execMillis
				}
			}
			b, a := sums["before"], sums["after"]
			t.Logf("RECLAIM projector_claim regime=%s rows=%d detail_bytes=%d mean_of_%d: wal_bytes/row %.1f -> %.1f (%+.1f) wal_records/row %.2f -> %.2f dirtied/row %.3f -> %.3f exec_ms/row %.4f -> %.4f",
				regime, rows, bytes, reps,
				perRow(b.walBytes, b.reclaimed), perRow(a.walBytes, a.reclaimed), perRow(a.walBytes, a.reclaimed)-perRow(b.walBytes, b.reclaimed),
				perRow(b.walRecords, b.reclaimed), perRow(a.walRecords, a.reclaimed),
				perRow(b.dirtied, b.reclaimed), perRow(a.dirtied, a.reclaimed),
				perRow(b.execMillis, b.reclaimed), perRow(a.execMillis, a.reclaimed))
		}
	}
}
