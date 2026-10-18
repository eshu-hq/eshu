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
	seedGapAdmissionDecisionAt(t, ctx, db,
		w.a.scopeID, w.a.genID, decisionID, anchorID, state, canonicalWrite, time.Now().UTC())
}

// seedGapAdmissionDecisionAt inserts one deployable_unit_correlation
// admission decision over an explicit scope generation with an explicit
// decision time, so the #8371 P1 convergence proof can order a superseding
// decision after the row it replaces deterministically.
func seedGapAdmissionDecisionAt(
	t *testing.T, ctx context.Context, db *sql.DB,
	scopeID, genID, decisionID, anchorID, state, canonicalWrite string, decidedAt time.Time,
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
    'unredacted', '', 'v1', $7, $7, $6::jsonb
)`, decisionID, state, scopeID, genID, anchorID, canonicalWrite, decidedAt); err != nil {
		t.Fatalf("seed admission decision %s: %v", decisionID, err)
	}
}

// TestListAllDeployableUnitAdmittedAnchorReposConvergesAfterSupersedingDecisionLive
// proves the #8371 P1 convergence guard on the real schema: the listing
// scopes to the latest decision per anchor, so an anchor admitted in one
// generation and rejected in a later one is excluded — the next maintenance
// pass is a no-op for it instead of replaying the healthy item forever —
// while a re-admitted anchor and a still-admitted anchor stay listed.
func TestListAllDeployableUnitAdmittedAnchorReposConvergesAfterSupersedingDecisionLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8371 admitted-anchors convergence proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	advanceFingerprintGateScopeGeneration(t, ctx, db, w.a.scopeID, "gen-a-2")
	advanceFingerprintGateScopeGeneration(t, ctx, db, w.a.scopeID, "gen-a-3")
	base := w.at
	// repo-stale: admitted, then rejected by a later generation. The old
	// ANY-admitted-row listing keeps it (the RED case); the latest-decision
	// scoping drops it, so the second pass is a no-op.
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, w.a.genID, "adm-stale-admit", "repo-stale", "admitted", `{"Written":true}`, base)
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, "gen-a-2", "adm-stale-reject", "repo-stale", "rejected", `{"Written":true}`, base.Add(time.Hour))
	// repo-readmit: admitted, rejected, admitted again — latest state wins.
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, w.a.genID, "adm-readmit-1", "repo-readmit", "admitted", `{"Written":true}`, base)
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, "gen-a-2", "adm-readmit-2", "repo-readmit", "rejected", `{"Written":true}`, base.Add(time.Hour))
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, "gen-a-3", "adm-readmit-3", "repo-readmit", "admitted", `{"Written":true}`, base.Add(2*time.Hour))
	// repo-steady: admitted once, never superseded.
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, w.a.genID, "adm-steady", "repo-steady", "admitted", `{"Written":true}`, base)

	store := NewRelationshipStore(SQLDB{DB: db})
	anchors, err := store.ListAllDeployableUnitAdmittedAnchorRepos(ctx)
	if err != nil {
		t.Fatalf("ListAllDeployableUnitAdmittedAnchorRepos() error = %v", err)
	}
	want := []string{"repo-readmit", "repo-steady"}
	if len(anchors) != len(want) {
		t.Fatalf("admitted anchors = %#v, want %#v", anchors, want)
	}
	for i := range want {
		if anchors[i] != want[i] {
			t.Fatalf("admitted anchors = %#v, want %#v", anchors, want)
		}
	}
}
