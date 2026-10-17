// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7127 PR-3b equivalence: the G1 fixture's history activated by three flag-on Acks links to byte-equal deltas, links, bucket counts and key state as the same history journaled as backfill rows, and matches the classification oracle; disposable database from ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN (env Skip); about 2 s locally; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// ajLedgerDigests digests a scope's ledger tables with the scope id stripped,
// so two scopes with the same history compare equal.
func ajLedgerDigests(t *testing.T, ctx context.Context, d *sql.DB, scopeID string) map[string]string {
	t.Helper()
	queries := map[string]string{
		"deltas": `SELECT replace(generation_id, $1, '') || '|' || replace(prior_generation_id, $1, '') || '|' || fact_category || '|' ||
            classification || '|' || stable_fact_key || '|' || COALESCE(prior_fact_kind, '') || '|' || COALESCE(current_fact_kind, '') || '|' ||
            COALESCE(encode(prior_state, 'hex'), '') || '|' || COALESCE(encode(current_state, 'hex'), '') || '|' || current_tombstoned
            FROM changed_since_link_deltas WHERE scope_id = $1 ORDER BY 1`,
		"links": `SELECT replace(generation_id, $1, '') || '|' || replace(prior_generation_id, $1, '') || '|' || link_kind || '|' ||
            delta_rows || '|' || files_keys || '|' || content_entities_keys || '|' || facts_keys
            FROM changed_since_links WHERE scope_id = $1 ORDER BY 1`,
		"buckets": `SELECT replace(generation_id, $1, '') || '|' || replace(prior_generation_id, $1, '') || '|' || fact_category || '|' ||
            classification || '|' || key_count FROM changed_since_link_bucket_counts WHERE scope_id = $1 ORDER BY 1`,
		"state": `SELECT fact_category || '|' || stable_fact_key || '|' || fact_kind || '|' || encode(state, 'hex') || '|' || COALESCE(owner_uri, '')
            FROM changed_since_key_state WHERE scope_id = $1 ORDER BY 1`,
	}
	out := map[string]string{}
	for name, query := range queries {
		lines := linkAccuracyLines(t, ctx, d, query, scopeID)
		out[name] = fmt.Sprintf("%s(%d)", linkAccuracyDigest(lines), len(lines))
	}
	return out
}

// TestProjectorAckJournalEquivalence proves 'ack' rows link to the same
// ledger as the G1 fixture's backfill rows (#7127 PR-3a ruling 8.6): the G1
// three-generation history journaled as backfill rows with NULL priors, and
// the same history activated by three flag-on Acks, give byte-equal link
// deltas, links, bucket counts and key state, and the last Ack link matches
// the classification oracle. The Ack rows carry their priors (g0 NULL, g1 <- g0,
// g2 <- g1); that only feeds delta break reasons, so the ledger is equal.
func TestProjectorAckJournalEquivalence(t *testing.T) {
	d := openAckJournalDB(t, 8)
	ctx := context.Background()

	seedLinkAccuracyScope(t, ctx, d, "eqg1")
	for gi := range 3 {
		ajExec(t, d, `INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
            VALUES ('eqg1', $1, NULL, 'backfill', now())`, fmt.Sprintf("eqg1-g%d", gi))
	}
	ajDrainLinks(t, d, "eqg1")
	want := ajLedgerDigests(t, ctx, d, "eqg1")

	seedLinkAccuracyScope(t, ctx, d, "eqak")
	ajExec(t, d, `UPDATE scope_generations SET status = 'pending', activated_at = NULL WHERE scope_id = 'eqak'`)
	for gi := range 3 {
		generationID := fmt.Sprintf("eqak-g%d", gi)
		ajRunning(t, d, "eqak", generationID)
		if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajWork("eqak", generationID), runtime.Result{}); err != nil {
			t.Fatalf("Ack %s: %v", generationID, err)
		}
	}
	journal := linkAccuracyLines(t, ctx, d, `SELECT replace(generation_id, $1, '') || '|' ||
        COALESCE(replace(prior_generation_id, $1, ''), '<null>') || '|' || source
        FROM changed_since_activations WHERE scope_id = $1 ORDER BY activation_seq`, "eqak")
	wantJournal := []string{"-g0|<null>|ack", "-g1|-g0|ack", "-g2|-g1|ack"}
	if fmt.Sprint(journal) != fmt.Sprint(wantJournal) {
		t.Fatalf("Ack journal rows = %v, want %v", journal, wantJournal)
	}
	ajDrainLinks(t, d, "eqak")
	got := ajLedgerDigests(t, ctx, d, "eqak")
	t.Logf("G1 backfill digests: %v", want)
	t.Logf("Ack digests:         %v", got)
	for _, table := range []string{"deltas", "links", "buckets", "state"} {
		if got[table] != want[table] {
			t.Errorf("%s digest differs: Ack %s, backfill %s", table, got[table], want[table])
		}
	}
	// The oracle compares the state too, so it checks the last link.
	if mismatches := linkAccuracyMismatches(t, ctx, d, "eqak", "eqak-g1", "eqak-g2"); len(mismatches) > 0 {
		t.Errorf("oracle mismatches eqak-g1 -> eqak-g2: %v", mismatches)
	}
}
