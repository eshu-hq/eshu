// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8283 success-write fence of the deployable_unit_correlation reopen fingerprint: a stale claim, an expired lease and a succeeded item are rejected with ErrExecutionClaimRejected (C1), an older claim token never replaces a newer row, and the retention delete of the scope generation cascades the row away with its work item; isolated schemas from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; 3.1 s on local PostgreSQL 18.6 on 2026-10-17; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The #8283 success-write fence proofs: the fingerprint row is written only by
// the live claim that read it, a stale claim can never replace a row a later
// claim recorded, and the row dies with its work item.

func TestDeployableUnitReopenFingerprintWriteIsClaimFencedLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	a := fingerprintGateScope{scopeID: "git:8283-a", genID: "gen-a-1", repoID: "repo-a"}
	seedFingerprintGateScope(t, ctx, db, a)
	t1 := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	t2 := t1.Add(time.Minute)

	stale := claimFingerprintGateItem(t, ctx, db, "duc-a", a.scopeID, a.genID, reducer.DomainDeployableUnitCorrelation, t1)
	staleRead := readFingerprintGateInput(t, ctx, db, []string{"repo-a"})
	// The lease moves to a new claim before the stale run writes.
	fresh := claimFingerprintGateItem(t, ctx, db, "duc-a", a.scopeID, a.genID, reducer.DomainDeployableUnitCorrelation, t2)

	err := recordFingerprintGateInput(ctx, db, stale, staleRead)
	if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("stale-claim fingerprint write error = %v, want ErrExecutionClaimRejected", err)
	}
	if got := fingerprintRowCount(t, ctx, db, "duc-a"); got != 0 {
		t.Fatalf("fingerprint rows after a stale-claim write = %d, want 0", got)
	}
	if err := recordFingerprintGateInput(ctx, db, fresh, readFingerprintGateInput(t, ctx, db, []string{"repo-a"})); err != nil {
		t.Fatalf("live-claim fingerprint write: %v", err)
	}
	if got := fingerprintRowCount(t, ctx, db, "duc-a"); got != 1 {
		t.Fatalf("fingerprint rows after the live-claim write = %d, want 1", got)
	}

	// A claim whose lease expired cannot write, even with its own token.
	if _, err := db.ExecContext(ctx,
		`UPDATE fact_work_items SET claim_until = clock_timestamp() - interval '1 second' WHERE work_item_id = 'duc-a'`); err != nil {
		t.Fatalf("expire duc-a lease: %v", err)
	}
	err = recordFingerprintGateInput(ctx, db, fresh, staleRead)
	if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("expired-lease fingerprint write error = %v, want ErrExecutionClaimRejected", err)
	}

	// A succeeded item has no live claim to write with.
	ackFingerprintGateItem(t, ctx, db, "duc-a", t2)
	err = recordFingerprintGateInput(ctx, db, fresh, staleRead)
	if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("succeeded-item fingerprint write error = %v, want ErrExecutionClaimRejected", err)
	}
}

// TestDeployableUnitReopenFingerprintWriteIsMonotonicLive proves the row keeps
// the newest claim's value: a live claim whose token is older than the stored
// row's (only reachable through a clock step on the claiming host) does not
// replace it.
func TestDeployableUnitReopenFingerprintWriteIsMonotonicLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	a := fingerprintGateScope{scopeID: "git:8283-a", genID: "gen-a-1", repoID: "repo-a"}
	seedFingerprintGateScope(t, ctx, db, a)
	newer := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	older := newer.Add(-time.Minute)

	claim := claimFingerprintGateItem(t, ctx, db, "duc-a", a.scopeID, a.genID, reducer.DomainDeployableUnitCorrelation, newer)
	if err := recordFingerprintGateInput(ctx, db, claim, readFingerprintGateInput(t, ctx, db, []string{"repo-a"})); err != nil {
		t.Fatalf("record newer fingerprint: %v", err)
	}
	var want string
	if err := db.QueryRowContext(ctx,
		`SELECT input_fingerprint FROM deployable_unit_reopen_fingerprints WHERE work_item_id = 'duc-a'`).Scan(&want); err != nil {
		t.Fatalf("read newer fingerprint: %v", err)
	}

	upsertFingerprintGateDeploysFrom(t, ctx, db, a.genID, "repo-b", "repo-a")
	back := claimFingerprintGateItem(t, ctx, db, "duc-a", a.scopeID, a.genID, reducer.DomainDeployableUnitCorrelation, older)
	err := recordFingerprintGateInput(ctx, db, back, readFingerprintGateInput(t, ctx, db, []string{"repo-a"}))
	if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("older-token fingerprint write error = %v, want ErrExecutionClaimRejected", err)
	}
	var got string
	if err := db.QueryRowContext(ctx,
		`SELECT input_fingerprint FROM deployable_unit_reopen_fingerprints WHERE work_item_id = 'duc-a'`).Scan(&got); err != nil {
		t.Fatalf("read fingerprint after older-token write: %v", err)
	}
	if got != want {
		t.Fatalf("fingerprint after an older-token write = %q, want the newer claim's %q", got, want)
	}
}

// TestDeployableUnitReopenFingerprintCascadesWithWorkItemLive proves a pruned
// work item takes its fingerprint row with it, through the scope generation
// retention deletes, so no orphan row outlives its item.
func TestDeployableUnitReopenFingerprintCascadesWithWorkItemLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	a := fingerprintGateScope{scopeID: "git:8283-a", genID: "gen-a-1", repoID: "repo-a"}
	seedFingerprintGateScope(t, ctx, db, a)
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	succeedFingerprintGateItem(t, ctx, db, "duc-a", a, []string{"repo-a"}, at)
	if got := fingerprintRowCount(t, ctx, db, "duc-a"); got != 1 {
		t.Fatalf("fingerprint rows before prune = %d, want 1", got)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, a.scopeID); err != nil {
		t.Fatalf("detach active generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, deleteScopeGenerationsForRetentionQuery, []string{a.genID}); err != nil {
		t.Fatalf("retention delete of %s: %v", a.genID, err)
	}
	if got := fingerprintRowCount(t, ctx, db, "duc-a"); got != 0 {
		t.Fatalf("fingerprint rows after the work item was pruned = %d, want 0", got)
	}
}
