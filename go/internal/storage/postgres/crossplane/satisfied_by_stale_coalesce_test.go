// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// seedCoalesceScope seeds one claim scope whose full generation gens[0]
// carries one claim fact, plus superseded scope_generations for gens[1:]
// with strictly increasing ingested_at, one pending
// crossplane_satisfied_by_materialization row per generation, and a quiet
// delta as the active generation. Every queued row is therefore stale
// against the active snapshot, and the delta derives no intent of its own:
// exactly the backlog a reducer lag leaves behind.
func seedCoalesceScope(
	ctx context.Context, t *testing.T, db *sql.DB, queue postgres.ReducerQueue,
	scopeID string, gens []string, group, kind string, now time.Time,
) {
	t.Helper()
	seedCrossplaneRedriveClaimScope(ctx, t, db, scopeID, gens[0], group, kind, 1, now)
	for i, gen := range gens[1:] {
		at := now.Add(time.Duration(i+1) * 30 * time.Second)
		crossplaneRedriveMustExec(ctx, t, db, `
			INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
			VALUES ($1,$2,'sync',$3,$3,'superseded',$3,'{}'::jsonb)
		`, gen, scopeID, at)
	}
	for _, gen := range gens {
		if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, gen); err != nil || !scheduled {
			t.Fatalf("schedule %s@%s = %v, %v; want true, nil", scopeID, gen, scheduled, err)
		}
	}
	seedCrossplaneRedriveDelta(ctx, t, db, scopeID, scopeID+"-delta", "c0", now.Add(10*time.Minute))
}

// coalesceSetWorkStatus forces one queued row into a non-pending unleased
// status with past visibility, so retrying rows are claimable carriers and
// failed/dead_letter rows carry failure evidence for the prior-failure fold.
func coalesceSetWorkStatus(ctx context.Context, t *testing.T, db *sql.DB, scopeID, generationID, status string) {
	t.Helper()
	crossplaneRedriveMustExec(ctx, t, db, `
		UPDATE fact_work_items
		SET status = $3,
		    lease_owner = NULL,
		    claim_until = NULL,
		    visible_at = '2020-01-01T00:00:00Z'::timestamptz,
		    failure_class = CASE WHEN $3 IN ('failed','dead_letter') THEN 'coalesce_probe_failure' ELSE failure_class END,
		    failure_message = CASE WHEN $3 IN ('failed','dead_letter') THEN 'coalesce probe failure' ELSE failure_message END
		WHERE scope_id = $1 AND generation_id = $2
		  AND stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
	`, scopeID, generationID, status)
}

func coalesceExecutionsByScope(written []map[string]any) map[string]int {
	out := map[string]int{}
	for _, row := range written {
		claimUID, _ := row["claim_uid"].(string)
		out[claimUID]++
	}
	return out
}

// testCrossplaneStaleRowCoalesceLive is the #10269 acceptance proof, run as
// the stale_coalesce subtest of the scheduled redrive proof. The shim's
// seven guard cases run over the real ReducerQueue and Runtime: stale
// per-generation rows coalesce onto the newest pending/retrying carrier,
// while the #10106 rebind debt (sole stale row, dead-letter newest) is
// never absorbed.
func testCrossplaneStaleRowCoalesceLive(t *testing.T) {
	t.Run("guard cases drain to one execution per scope", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "coalesce.example.org", "XCoalesceClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "coal-xrd", "coal-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-proof", time.Minute)

		seedCoalesceScope(ctx, t, db, queue, "coal-sole", []string{"coal-sole-g1"}, group, kind, now)
		seedCoalesceScope(ctx, t, db, queue, "coal-deadletter", []string{"coal-deadletter-g1", "coal-deadletter-g2"}, group, kind, now)
		coalesceSetWorkStatus(ctx, t, db, "coal-deadletter", "coal-deadletter-g2", "dead_letter")
		seedCoalesceScope(ctx, t, db, queue, "coal-failednewest", []string{"coal-failednewest-g1", "coal-failednewest-g2"}, group, kind, now)
		coalesceSetWorkStatus(ctx, t, db, "coal-failednewest", "coal-failednewest-g2", "failed")
		seedCoalesceScope(ctx, t, db, queue, "coal-prunefailed", []string{"coal-prunefailed-g1", "coal-prunefailed-g2"}, group, kind, now)
		coalesceSetWorkStatus(ctx, t, db, "coal-prunefailed", "coal-prunefailed-g1", "failed")
		seedCoalesceScope(ctx, t, db, queue, "coal-retrying", []string{"coal-retrying-g1", "coal-retrying-g2"}, group, kind, now)
		coalesceSetWorkStatus(ctx, t, db, "coal-retrying", "coal-retrying-g2", "retrying")

		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		// The shared drain helper caps at 8 claims; 5 scopes need 5.
		drainQuietDeltaQueue(ctx, t, queue, rt)

		written, gens := spy.snapshot()
		executions := coalesceExecutionsByScope(written)
		for _, scope := range []string{"coal-sole", "coal-deadletter", "coal-failednewest", "coal-prunefailed", "coal-retrying"} {
			if got := executions[scope+"-uid-0"]; got != 1 {
				t.Errorf("scope %s executions = %d, want exactly 1 (all writes %v)", scope, got, written)
			}
		}
		// Every surviving execution materialized its scope's active (delta)
		// snapshot, never its own stale generation.
		for i, row := range written {
			claimUID, _ := row["claim_uid"].(string)
			scope := claimUID[:len(claimUID)-len("-uid-0")]
			if gens[i] != scope+"-delta" {
				t.Errorf("edge %d generation = %q, want %q-delta", i, gens[i], scope)
			}
		}

		wantStatus := map[string]string{
			"coal-sole@coal-sole-g1":                 "succeeded",
			"coal-deadletter@coal-deadletter-g1":     "succeeded",
			"coal-deadletter@coal-deadletter-g2":     "dead_letter",
			"coal-failednewest@coal-failednewest-g1": "succeeded",
			"coal-failednewest@coal-failednewest-g2": "failed",
			"coal-prunefailed@coal-prunefailed-g1":   "superseded",
			"coal-prunefailed@coal-prunefailed-g2":   "succeeded",
			"coal-retrying@coal-retrying-g1":         "superseded",
			"coal-retrying@coal-retrying-g2":         "succeeded",
		}
		for key, want := range wantStatus {
			scope, gen, _ := strings.Cut(key, "@")
			if got := quietDeltaWorkItemStatus(ctx, t, db, scope, gen); got != want {
				t.Errorf("work item %s status = %q, want %q", key, got, want)
			}
		}

		// The pruned failed row keeps its failure evidence under
		// prior_failure (the sweep's existing fold).
		var details string
		if err := db.QueryRowContext(ctx, `
			SELECT failure_details FROM fact_work_items
			WHERE scope_id = 'coal-prunefailed' AND generation_id = 'coal-prunefailed-g1'
			  AND domain = 'crossplane_satisfied_by_materialization'
		`).Scan(&details); err != nil {
			t.Fatalf("read pruned row failure_details: %v", err)
		}
		if !strings.Contains(details, "prior_failure") || !strings.Contains(details, "coalesce_probe_failure") {
			t.Errorf("pruned failed row failure_details = %q, want prior_failure fold with the probe failure class", details)
		}
	})

	t.Run("three queued generations collapse in one batch claim", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "chain.example.org", "XChainClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "chain-xrd", "chain-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-chain", time.Minute)
		seedCoalesceScope(ctx, t, db, queue, "coal-chain",
			[]string{"coal-chain-g1", "coal-chain-g2", "coal-chain-g3"}, group, kind, now)

		intents, err := queue.ClaimBatch(ctx, 8)
		if err != nil {
			t.Fatalf("ClaimBatch: %v", err)
		}
		if len(intents) != 1 || intents[0].GenerationID != "coal-chain-g3" {
			t.Fatalf("ClaimBatch intents = %v, want only the newest carrier coal-chain-g3", intents)
		}
		for _, gen := range []string{"coal-chain-g1", "coal-chain-g2"} {
			if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-chain", gen); got != "superseded" {
				t.Errorf("stale row %s status = %q, want superseded by the batch claim sweep", gen, got)
			}
		}
		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		result, err := rt.Execute(ctx, intents[0])
		if err != nil {
			t.Fatalf("Execute carrier: %v", err)
		}
		if err := queue.Ack(ctx, intents[0], result); err != nil {
			t.Fatalf("Ack carrier: %v", err)
		}
		written, gens := spy.snapshot()
		if len(written) != 1 || gens[0] != "coal-chain-delta" {
			t.Fatalf("SATISFIED_BY rows = %v gens %v, want one edge under coal-chain-delta", written, gens)
		}
		if extra, claimed, err := queue.Claim(ctx); err != nil || claimed {
			t.Fatalf("claim after drain = (%q, %t, %v), want no work", extra.IntentID, claimed, err)
		}
	})

	t.Run("active generation row carries stale siblings", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "activecarrier.example.org", "XActiveCarrierClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "activecarrier-xrd", "activecarrier-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-active-carrier", time.Minute)
		// The delta here is not quiet: it derived its own row, so the
		// carrier sits on the active generation itself.
		seedCoalesceScope(ctx, t, db, queue, "coal-activecarrier",
			[]string{"coal-activecarrier-g1", "coal-activecarrier-g2"}, group, kind, now)
		if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, "coal-activecarrier", "coal-activecarrier-delta"); err != nil || !scheduled {
			t.Fatalf("schedule active-generation row = %v, %v; want true, nil", scheduled, err)
		}
		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		drainQuietDeltaQueue(ctx, t, queue, rt)
		written, gens := spy.snapshot()
		if len(written) != 1 || gens[0] != "coal-activecarrier-delta" {
			t.Fatalf("executions = %v gens %v, want one run under the active generation", written, gens)
		}
		for _, gen := range []string{"coal-activecarrier-g1", "coal-activecarrier-g2"} {
			if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-activecarrier", gen); got != "superseded" {
				t.Errorf("stale row %s status = %q, want superseded against the active carrier", gen, got)
			}
		}
		if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-activecarrier", "coal-activecarrier-delta"); got != "succeeded" {
			t.Errorf("active carrier status = %q, want succeeded (never superseded)", got)
		}
	})

	t.Run("in-flight overlap adds at most one execution", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "inflight.example.org", "XInflightClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "inflight-xrd", "inflight-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-inflight", time.Minute)

		// g1 claims and executes but has not ACKed when g2/g3 queue.
		seedCoalesceScope(ctx, t, db, queue, "coal-inflight", []string{"coal-inflight-g1"}, group, kind, now)
		intent, claimed, err := queue.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("Claim g1 = %v, %v; want a claimed intent", claimed, err)
		}
		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		result, err := rt.Execute(ctx, intent)
		if err != nil {
			t.Fatalf("Execute g1: %v", err)
		}
		for i, gen := range []string{"coal-inflight-g2", "coal-inflight-g3"} {
			at := now.Add(time.Duration(i+1) * 30 * time.Second)
			crossplaneRedriveMustExec(ctx, t, db, `
				INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
				VALUES ($1,'coal-inflight','sync',$2,$2,'superseded',$2,'{}'::jsonb)
			`, gen, at)
			if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, "coal-inflight", gen); err != nil || !scheduled {
				t.Fatalf("schedule %s = %v, %v", gen, scheduled, err)
			}
		}
		if err := queue.Ack(ctx, intent, result); err != nil {
			t.Fatalf("Ack g1: %v", err)
		}
		drainQuietDeltaQueue(ctx, t, queue, rt)
		written, _ := spy.snapshot()
		if len(written) != 2 {
			t.Fatalf("executions = %d, want 2 (in-flight g1 + newest carrier), independent of queued generations", len(written))
		}
		if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-inflight", "coal-inflight-g2"); got != "superseded" {
			t.Errorf("middle row status = %q, want superseded", got)
		}
		if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-inflight", "coal-inflight-g3"); got != "succeeded" {
			t.Errorf("carrier status = %q, want succeeded", got)
		}
	})

	t.Run("sibling claimed before the sweep lock keeps the stale row", func(t *testing.T) {
		db, targetDSN := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "epq.example.org", "XEpqClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "epq-xrd", "epq-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-epq-seed", time.Minute)
		seedCoalesceScope(ctx, t, db, queue, "coal-epq",
			[]string{"coal-epq-g1", "coal-epq-g2"}, group, kind, now)

		// X holds the stale g1 row lock with a version-bumping update. The
		// sweep's lock step snapshots g2 as a pending carrier and waits on
		// X; meanwhile a third session claims g2. When X commits, the
		// EvalPlanQual recheck must re-evaluate the carrier EXISTS on the
		// newest state: g2 claimed is not a carrier, so g1 survives.
		holder := crossplaneRedriveProofConn(t, targetDSN)
		tx, err := holder.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin holder: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `
			UPDATE fact_work_items SET updated_at = updated_at
			WHERE scope_id = 'coal-epq' AND generation_id = 'coal-epq-g1'
			  AND domain = 'crossplane_satisfied_by_materialization'
		`); err != nil {
			t.Fatalf("holder lock: %v", err)
		}
		var holderPID int
		if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatalf("holder pid: %v", err)
		}

		sweeperDB := crossplaneRedriveProofConn(t, targetDSN)
		var sweeperPID int
		if err := sweeperDB.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&sweeperPID); err != nil {
			t.Fatalf("sweeper pid: %v", err)
		}
		sweepQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: sweeperDB}, "coalesce-epq-sweep", time.Minute)
		sweepDone := make(chan error, 1)
		go func() {
			_, _, claimErr := sweepQueue.Claim(ctx)
			sweepDone <- claimErr
		}()
		waitCoalesceBlocked(ctx, t, db, sweeperPID, holderPID)

		crossplaneRedriveMustExec(ctx, t, db, `
			UPDATE fact_work_items
			SET status = 'claimed', lease_owner = 'epq-carrier-worker', claim_until = $1
			WHERE scope_id = 'coal-epq' AND generation_id = 'coal-epq-g2'
			  AND domain = 'crossplane_satisfied_by_materialization'
		`, time.Now().UTC().Add(time.Hour))
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit holder: %v", err)
		}
		if err := <-sweepDone; err != nil {
			t.Fatalf("sweep claim: %v", err)
		}
		if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-epq", "coal-epq-g1"); got == "superseded" {
			t.Fatalf("stale row superseded although its only carrier was claimed before the lock: the rebind fallback was destroyed")
		}
		if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-epq", "coal-epq-g2"); got != "claimed" {
			t.Fatalf("carrier status = %q, want claimed untouched", got)
		}
	})

	t.Run("backlog drain counts executions per scope not per row", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "backlog.example.org", "XBacklogClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "backlog-xrd", "backlog-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-backlog", time.Minute)

		// S=3 scopes x G=3 queued generations = 9 rows. Without
		// coalescing every row runs the full materialization (9
		// executions); with the prune exactly one run per scope remains
		// (3 executions, 6 superseded rows).
		const scopes, gens = 3, 3
		for s := range scopes {
			scopeID := "coal-backlog-" + string(rune('a'+s))
			seedCoalesceScope(ctx, t, db, queue, scopeID,
				[]string{scopeID + "-g1", scopeID + "-g2", scopeID + "-g3"}, group, kind, now.Add(time.Duration(s)*time.Hour))
		}
		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		drainQuietDeltaQueue(ctx, t, queue, rt)
		written, _ := spy.snapshot()
		if len(written) != scopes {
			t.Fatalf("handler executions = %d, want %d (one per scope; %d rows queued)", len(written), scopes, scopes*gens)
		}
		var superseded int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM fact_work_items
			WHERE stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
			  AND status = 'superseded'
			  AND failure_class = 'reducer_superseded_by_newer_active_generation'
		`).Scan(&superseded); err != nil {
			t.Fatalf("count superseded: %v", err)
		}
		if want := scopes * (gens - 1); superseded != want {
			t.Fatalf("superseded rows = %d, want %d", superseded, want)
		}
		t.Logf("backlog drain: rows=%d executions=%d superseded=%d (no-coalesce executions=%d)", scopes*gens, len(written), superseded, scopes*gens)
	})

	t.Run("concurrent claimers coalesce to one execution", func(t *testing.T) {
		db, targetDSN := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "coalerace.example.org", "XCoaleRaceClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "coalerace-xrd", "coalerace-xrd-gen", group, kind, now.Add(time.Minute))
		seedQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-race-seed", time.Minute)
		seedCoalesceScope(ctx, t, db, seedQueue, "coal-race",
			[]string{"coal-race-g1", "coal-race-g2", "coal-race-g3"}, group, kind, now)

		queues := []postgres.ReducerQueue{
			postgres.NewReducerQueue(postgres.SQLDB{DB: crossplaneRedriveProofConn(t, targetDSN)}, "coale-race-a", time.Minute),
			postgres.NewReducerQueue(postgres.SQLDB{DB: crossplaneRedriveProofConn(t, targetDSN)}, "coale-race-b", time.Minute),
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		intents := make([]reducercontract.Intent, 2)
		oks := make([]bool, 2)
		errs := make([]error, 2)
		for i := range queues {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				intents[i], oks[i], errs[i] = queues[i].Claim(ctx)
			}()
		}
		close(start)
		wg.Wait()
		winners := 0
		var winner postgres.ReducerQueue
		var winnerIntent reducercontract.Intent
		for i := range queues {
			if errs[i] != nil {
				t.Fatalf("concurrent Claim[%d]: %v", i, errs[i])
			}
			if oks[i] {
				winners++
				winner, winnerIntent = queues[i], intents[i]
			}
		}
		if winners != 1 {
			t.Fatalf("concurrent claim winners = %d, want exactly 1", winners)
		}
		if winnerIntent.GenerationID != "coal-race-g3" {
			t.Fatalf("winner generation = %q, want the newest carrier coal-race-g3", winnerIntent.GenerationID)
		}
		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		result, err := rt.Execute(ctx, winnerIntent)
		if err != nil {
			t.Fatalf("Execute winner: %v", err)
		}
		if err := winner.Ack(ctx, winnerIntent, result); err != nil {
			t.Fatalf("Ack winner: %v", err)
		}
		written, gens := spy.snapshot()
		if len(written) != 1 || gens[0] != "coal-race-delta" {
			t.Fatalf("SATISFIED_BY rows = %v gens %v, want one edge under coal-race-delta", written, gens)
		}
		for _, gen := range []string{"coal-race-g1", "coal-race-g2"} {
			if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-race", gen); got != "superseded" {
				t.Errorf("stale row %s status = %q, want superseded exactly once", gen, got)
			}
		}
	})
}

// waitCoalesceBlocked polls until pid is blocked by blocker in
// pg_blocking_pids, proving the sweep reached its lock step and is waiting
// on the holder before the test mutates the carrier.
func waitCoalesceBlocked(ctx context.Context, t *testing.T, db *sql.DB, pid, blocker int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := db.QueryRowContext(ctx, `
			SELECT $2 = ANY(pg_blocking_pids($1))
		`, pid, blocker).Scan(&blocked); err != nil {
			t.Fatalf("poll pg_blocking_pids: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid %d never blocked on holder %d within 30s", pid, blocker)
}
