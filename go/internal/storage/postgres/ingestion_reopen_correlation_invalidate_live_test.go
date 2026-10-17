// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8316 fingerprint invalidation when a Repository deleter removes a node: deleting the rows whose repo_ids contain the removed repository re-opens exactly those items, keeps unrelated rows, and is a no-op for empty and unknown inputs; isolated schema from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestInvalidateDeployableUnitReopenFingerprintsForReposLive proves the #8316
// push-invalidation primitive: after a Repository node is deleted from the
// graph, deleting the fingerprint rows whose repo_ids contain it re-opens
// exactly the items that read it, while items that never read it stay
// succeeded with their rows intact.
func TestInvalidateDeployableUnitReopenFingerprintsForReposLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8316 invalidation proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	// duc-a reads repo-a (world), duc-x reads repo-a plus repo-x, duc-c reads
	// repo-c (world).
	succeedFingerprintGateItem(t, ctx, db, "duc-x", w.a, []string{"repo-a", "repo-x"}, w.at.Add(time.Minute))

	store := NewRelationshipStore(SQLDB{DB: db})
	deleted, err := store.InvalidateDeployableUnitReopenFingerprintsForRepos(ctx, []string{"repo-a"})
	if err != nil {
		t.Fatalf("InvalidateDeployableUnitReopenFingerprintsForRepos(repo-a) error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("invalidated rows for repo-a = %d, want 2 (duc-a and duc-x)", deleted)
	}
	for workItemID, want := range map[string]int{"duc-a": 0, "duc-x": 0, "duc-c": 1} {
		if got := fingerprintRowCount(t, ctx, db, workItemID); got != want {
			t.Errorf("fingerprint rows for %s = %d, want %d", workItemID, got, want)
		}
	}

	deleted, err = store.InvalidateDeployableUnitReopenFingerprintsForRepos(ctx, []string{"repo-unknown"})
	if err != nil {
		t.Fatalf("invalidate unknown repo error = %v", err)
	}
	if deleted != 0 {
		t.Fatalf("invalidated rows for unknown repo = %d, want 0", deleted)
	}

	deleted, err = store.InvalidateDeployableUnitReopenFingerprintsForRepos(ctx, nil)
	if err != nil {
		t.Fatalf("invalidate empty input error = %v", err)
	}
	if deleted != 0 {
		t.Fatalf("invalidated rows for empty input = %d, want 0", deleted)
	}
	if got := fingerprintRowCount(t, ctx, db, "duc-c"); got != 1 {
		t.Fatalf("fingerprint rows for duc-c after no-op invalidations = %d, want 1", got)
	}

	// The batch form (array overlap) deletes every row touching any of the
	// removed repositories in one statement.
	succeedFingerprintGateItem(t, ctx, db, "duc-y", w.a, []string{"repo-c", "repo-d"}, w.at.Add(2*time.Minute))
	deleted, err = store.InvalidateDeployableUnitReopenFingerprintsForRepos(ctx, []string{"repo-c", "repo-d"})
	if err != nil {
		t.Fatalf("InvalidateDeployableUnitReopenFingerprintsForRepos(repo-c, repo-d) error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("invalidated rows for repo-c, repo-d = %d, want 2 (duc-c and duc-y)", deleted)
	}
	for workItemID, want := range map[string]int{"duc-c": 0, "duc-y": 0} {
		if got := fingerprintRowCount(t, ctx, db, workItemID); got != want {
			t.Errorf("fingerprint rows for %s after batch invalidation = %d, want %d", workItemID, got, want)
		}
	}
}

// TestListAllDeployableUnitAdmittedAnchorReposLive proves the #8316
// gap-reconciliation Postgres half on the real schema: the full listing
// returns every admitted deployable_unit_correlation source repository,
// skips decisions whose canonical write never landed, keeps legacy rows
// predating the Written key, and excludes other states and domains.
func TestListAllDeployableUnitAdmittedAnchorReposLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8316 admitted-anchors proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	seedGapAdmissionDecision(t, ctx, db, w, "adm-written", "repo-a", "admitted", `{"Written":true}`)
	seedGapAdmissionDecision(t, ctx, db, w, "adm-unwritten", "repo-b", "admitted", `{"Written":false}`)
	seedGapAdmissionDecision(t, ctx, db, w, "adm-legacy", "repo-c", "admitted", `{}`)
	seedGapAdmissionDecision(t, ctx, db, w, "adm-rejected", "repo-d", "rejected", `{"Written":true}`)
	seedGapAdmissionDecision(t, ctx, db, w, "adm-missing", "repo-e", "missing_evidence", `{"Written":false}`)

	store := NewRelationshipStore(SQLDB{DB: db})
	anchors, err := store.ListAllDeployableUnitAdmittedAnchorRepos(ctx)
	if err != nil {
		t.Fatalf("ListAllDeployableUnitAdmittedAnchorRepos() error = %v", err)
	}
	want := []string{"repo-a", "repo-c"}
	if len(anchors) != len(want) {
		t.Fatalf("admitted anchors = %#v, want %#v", anchors, want)
	}
	for i := range want {
		if anchors[i] != want[i] {
			t.Fatalf("admitted anchors = %#v, want %#v", anchors, want)
		}
	}
}

// seedGapAdmissionDecision inserts one deployable_unit_correlation admission
// decision over the world scope A generation for the gap-reconciliation
// listing proof.
func seedGapAdmissionDecision(
	t *testing.T, ctx context.Context, db *sql.DB,
	w fingerprintGateWorld, decisionID, anchorID, state, canonicalWrite string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO admission_decisions (
    decision_id, domain, state, domain_state, scope_id, generation_id,
    anchor_kind, anchor_id, candidate_kind, candidate_id,
    confidence_score, confidence_bucket, confidence_basis,
    freshness_state, freshness_cause,
    redaction_state, redaction_reason, payload_version, decided_at, updated_at, canonical_write
) VALUES (
    $1, 'deployable_unit_correlation', $2, 'admitted-candidate', $3, $4,
    'repository', $5, 'deployable_unit', $1,
    0.95, 'high', 'test-pack',
    'fresh', 'test-seed',
    'unredacted', '', 'v1', now(), now(), $6::jsonb
)`, decisionID, state, w.a.scopeID, w.a.genID, anchorID, canonicalWrite); err != nil {
		t.Fatalf("seed admission decision %s: %v", decisionID, err)
	}
}
