// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8294 completion-driven deployable_unit_correlation wake on the full bootstrap schema through the production RelationshipStore: the scope replay floor leaves below-floor succeeded history succeeded while an at-floor item reopens (EXPLAIN of the production statement locks only the at-floor row), a missing fingerprint row reopens, an unchanged fingerprint stays succeeded on a retry wake, a live re-run of UpsertResolved reports Inserted=0 yet the retry wake still reopens and then converges, and a running item keeps its lease with cross_scope_replay_required set so its ACK returns it to pending; isolated schema per subtest from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; about 15 s for the file on local PostgreSQL 18 on 2026-10-19; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// The #8294 completion-wake live proofs (review F1-F5). Every step drives
// production code: the fingerprint-gate fixture seeds succeeded
// deployable_unit_correlation items, UpsertResolved appends rows under an
// already-active generation, and ReopenDeployableUnitCorrelationsForRepos is
// the wake under test. Gated on ESHU_REDUCER_FAIRNESS_PROOF_DSN or
// ESHU_POSTGRES_DSN, on the full production bootstrap schema.

func completionWakeStore(db *sql.DB) *RelationshipStore {
	return NewRelationshipStore(SQLDB{DB: db})
}

func workItemReplayBit(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) bool {
	t.Helper()
	var bit bool
	if err := db.QueryRowContext(ctx,
		`SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&bit); err != nil {
		t.Fatalf("read replay bit for %s: %v", workItemID, err)
	}
	return bit
}

func TestCompletionWakeFloorMissingAndUnchangedLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()

	t.Run("below_floor_history_stays_succeeded_at_floor_reopens", func(t *testing.T) {
		db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
		w := seedFingerprintGateWorld(t, ctx, db)
		// duc-old succeeded on gen-a-1; advancing scope A moves the replay
		// floor to gen-a-2, below which duc-old is dead history.
		succeedFingerprintGateItem(t, ctx, db, "duc-old", w.a, []string{"repo-a"}, w.at)
		advanceFingerprintGateScopeGeneration(t, ctx, db, w.a.scopeID, "gen-a-2")
		newScope := fingerprintGateScope{scopeID: w.a.scopeID, genID: "gen-a-2", repoID: "repo-a"}
		succeedFingerprintGateItem(t, ctx, db, "duc-new", newScope, []string{"repo-a"}, w.at)
		// Append under B's already-active generation: repo-a readers' input
		// changed after both successes.
		upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")

		if _, err := completionWakeStore(db).ReopenDeployableUnitCorrelationsForRepos(
			ctx, []string{"repo-a", "repo-b-worker"}); err != nil {
			t.Fatalf("completion wake: %v", err)
		}
		if got := workItemStatus(t, ctx, db, "duc-old"); got != "succeeded" {
			t.Errorf("duc-old (below replay floor) status = %q, want succeeded", got)
		}
		if got := workItemStatus(t, ctx, db, "duc-new"); got != "pending" {
			t.Errorf("duc-new (at replay floor, input changed) status = %q, want pending", got)
		}
	})

	t.Run("floor_plan_uses_scope_replay_floor", func(t *testing.T) {
		db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
		w := seedFingerprintGateWorld(t, ctx, db)
		// Seed below-floor history: 30 succeeded items on gen-a-1, then move
		// scope A's floor to gen-a-2. The wake statement's own plan must
		// carry the materialized floor CTE, and executing it must touch
		// only the at-floor item.
		for i := range 30 {
			repo := fmt.Sprintf("repo-h%02d", i)
			succeedFingerprintGateItem(t, ctx, db, fmt.Sprintf("duc-h%02d", i), w.a, []string{repo}, w.at)
			upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b", repo)
		}
		advanceFingerprintGateScopeGeneration(t, ctx, db, w.a.scopeID, "gen-a-2")
		newScope := fingerprintGateScope{scopeID: w.a.scopeID, genID: "gen-a-2", repoID: "repo-a"}
		succeedFingerprintGateItem(t, ctx, db, "duc-new", newScope, []string{"repo-a"}, w.at)
		upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin explain tx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		repos := []string{"repo-a", "repo-b", "repo-b-worker"}
		for i := range 30 {
			repos = append(repos, fmt.Sprintf("repo-h%02d", i))
		}
		rows, err := tx.QueryContext(ctx,
			"EXPLAIN (ANALYZE, BUFFERS) "+reopenDeployableUnitCorrelationsForReposQuery,
			time.Now().UTC(), array.StringArray(repos))
		if err != nil {
			t.Fatalf("explain wake: %v", err)
		}
		plan := ""
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan line: %v", err)
			}
			plan += line + "\n"
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("explain rows: %v", err)
		}
		t.Logf("completion wake reopen plan over 30 below-floor + 1 at-floor items:\n%s", plan)
		if !strings.Contains(plan, "scope_replay_floor") {
			t.Fatalf("wake plan does not use the scope_replay_floor CTE:\n%s", plan)
		}
		// Inside the same tx the analyzed UPDATE already ran: only the
		// at-floor item may be pending.
		var pending int
		if err := tx.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE domain = 'deployable_unit_correlation' AND status = 'pending'
  AND work_item_id LIKE 'duc-%'`).Scan(&pending); err != nil {
			t.Fatalf("count pending in explain tx: %v", err)
		}
		if pending != 1 {
			t.Fatalf("pending after analyzed wake = %d, want 1 (at-floor only)", pending)
		}
	})

	t.Run("missing_fingerprint_row_reopens", func(t *testing.T) {
		db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
		seedFingerprintGateWorld(t, ctx, db)
		if _, err := db.ExecContext(ctx,
			`DELETE FROM deployable_unit_reopen_fingerprints WHERE work_item_id = 'duc-a'`); err != nil {
			t.Fatalf("drop duc-a fingerprint: %v", err)
		}
		if _, err := completionWakeStore(db).ReopenDeployableUnitCorrelationsForRepos(
			ctx, []string{"repo-a"}); err != nil {
			t.Fatalf("completion wake: %v", err)
		}
		if got := workItemStatus(t, ctx, db, "duc-a"); got != "pending" {
			t.Errorf("duc-a (missing fingerprint) status = %q, want pending", got)
		}
	})

	t.Run("unchanged_fingerprint_stays_succeeded", func(t *testing.T) {
		db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
		seedFingerprintGateWorld(t, ctx, db)
		// No append after duc-a's success: a retry wake over its repos must
		// not churn it back to pending.
		woken, err := completionWakeStore(db).ReopenDeployableUnitCorrelationsForRepos(
			ctx, []string{"repo-a"})
		if err != nil {
			t.Fatalf("completion wake: %v", err)
		}
		if woken.Reopened != 0 || woken.ReplayFlagged != 0 {
			t.Errorf("unchanged wake = %+v, want zero", woken)
		}
		if got := workItemStatus(t, ctx, db, "duc-a"); got != "succeeded" {
			t.Errorf("duc-a (unchanged input) status = %q, want succeeded", got)
		}
	})
}

func TestCompletionWakeRunningItemReplayBitLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)

	// duc-run read the pre-append input and is still running when the wake
	// fires: the wake must not steal its lease, and its later ACK must not
	// land it succeeded on stale input.
	claim := claimFingerprintGateItem(t, ctx, db, "duc-run", w.a.scopeID, w.a.genID,
		reducer.DomainDeployableUnitCorrelation, w.at)
	if err := recordFingerprintGateInput(ctx, db, claim,
		readFingerprintGateInput(t, ctx, db, []string{"repo-a"})); err != nil {
		t.Fatalf("record duc-run fingerprint: %v", err)
	}
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")
	if _, err := completionWakeStore(db).ReopenDeployableUnitCorrelationsForRepos(
		ctx, []string{"repo-a", "repo-b-worker"}); err != nil {
		t.Fatalf("completion wake: %v", err)
	}
	if got := workItemStatus(t, ctx, db, "duc-run"); got != "running" {
		t.Fatalf("duc-run status after wake = %q, want running (lease kept)", got)
	}
	if !workItemReplayBit(t, ctx, db, "duc-run") {
		t.Fatal("duc-run cross_scope_replay_required = false after wake, want true")
	}
	ackFingerprintGateItem(t, ctx, db, "duc-run", w.at)
	if got := workItemStatus(t, ctx, db, "duc-run"); got != "pending" {
		t.Fatalf("duc-run status after ACK = %q, want pending (ACK trigger replay)", got)
	}
	if workItemReplayBit(t, ctx, db, "duc-run") {
		t.Fatal("duc-run cross_scope_replay_required still true after ACK trigger consumed it")
	}
}

// TestCompletionWakeRetryConvergesAfterLostWakeLive is the F3/AC1 proof: a
// live re-run of UpsertResolved over stored identities reports Inserted=0
// (real INSERT 0 0 through the driver), and the wake run from that retry's
// result still reopens the item the first pass's lost wake missed.
func TestCompletionWakeRetryConvergesAfterLostWakeLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	store := completionWakeStore(db)

	row := relationships.ResolvedRelationship{
		SourceRepoID: "repo-b-worker", TargetRepoID: "repo-a",
		SourceEntityID: "entity:repo-b-worker", TargetEntityID: "entity:repo-a",
		RelationshipType: relationships.RelDeploysFrom, Confidence: 0.95,
		EvidenceCount: 1, Rationale: "completion wake retry proof",
		ResolutionSource: relationships.ResolutionSourceInferred,
	}
	first, err := store.UpsertResolved(ctx, w.b.genID, []relationships.ResolvedRelationship{row})
	if err != nil {
		t.Fatalf("first UpsertResolved: %v", err)
	}
	if first.Inserted != 1 || len(first.RepoIDs) != 2 {
		t.Fatalf("first UpsertResolved = %+v, want Inserted=1 with 2 repos", first)
	}
	// The first pass's wake is lost (crash/failure between the autocommit
	// insert and the wake). The retry re-resolves the same rows.
	second, err := store.UpsertResolved(ctx, w.b.genID, []relationships.ResolvedRelationship{row})
	if err != nil {
		t.Fatalf("retry UpsertResolved: %v", err)
	}
	if second.Inserted != 0 {
		t.Fatalf("retry UpsertResolved Inserted = %d, want 0 (INSERT 0 0)", second.Inserted)
	}
	if len(second.RepoIDs) != 2 {
		t.Fatalf("retry UpsertResolved RepoIDs = %v, want the full resolved scope", second.RepoIDs)
	}
	woken, err := store.ReopenDeployableUnitCorrelationsForRepos(ctx, second.RepoIDs)
	if err != nil {
		t.Fatalf("retry completion wake: %v", err)
	}
	if woken.Reopened < 1 {
		t.Fatalf("retry wake = %+v, want duc-a reopened", woken)
	}
	if got := workItemStatus(t, ctx, db, "duc-a"); got != "pending" {
		t.Fatalf("duc-a status after retry wake = %q, want pending", got)
	}
	// And it converges: once duc-a re-succeeds on the new input, a repeat
	// wake over the same repos reopens nothing.
	succeedFingerprintGateItem(t, ctx, db, "duc-a", w.a, []string{"repo-a"}, w.at.Add(time.Minute))
	woken, err = store.ReopenDeployableUnitCorrelationsForRepos(ctx, second.RepoIDs)
	if err != nil {
		t.Fatalf("repeat completion wake: %v", err)
	}
	if woken.Reopened != 0 || woken.ReplayFlagged != 0 {
		t.Fatalf("repeat wake = %+v, want zero after re-success", woken)
	}
}

// waitForCompletionWakeLockWaiter polls until some other backend is waiting on a lock, so
// an interleave test can act while the wake is provably blocked. Scoped to
// the current database: an unscoped poll can match an unrelated backend's
// lock wait on the shared server and return before this wake is blocked,
// passing vacuously without exercising the EvalPlanQual recheck.
func waitForCompletionWakeLockWaiter(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&waiting)
		if err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no backend waited on a lock within 10s; interleave did not form")
}
