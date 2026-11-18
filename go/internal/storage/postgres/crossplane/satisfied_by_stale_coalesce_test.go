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

	"github.com/eshu-hq/eshu/go/internal/reducer"
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

// coalesceSweep runs one periodic stale-generation sweep pass. Since
// #10863 the claims only fence stale rows at hand-out through the
// read-only gate; this out-of-claim pass (the production
// ReducerStaleGenerationSweeper's statement) is what terminalizes them,
// so the coalesce proofs drive it explicitly in production order.
func coalesceSweep(ctx context.Context, t *testing.T, queue postgres.ReducerQueue) int {
	t.Helper()
	swept, err := queue.SupersedeStaleReducerGenerations(ctx, 1000)
	if err != nil {
		t.Fatalf("SupersedeStaleReducerGenerations: %v", err)
	}
	return swept
}

// drainCoalesceQueue drains like drainQuietDeltaQueue, ticking one sweep
// pass before each claim: the production sweeper's fastest-cadence form,
// and the pairing that prunes each scope's stale siblings against their
// carrier before the carrier executes (after it succeeds, a fenced
// sibling would turn claimable again and re-run the materialization).
func drainCoalesceQueue(ctx context.Context, t *testing.T, queue postgres.ReducerQueue, rt *reducer.Runtime) {
	t.Helper()
	for range 8 {
		coalesceSweep(ctx, t, queue)
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if !claimed {
			return
		}
		result, err := rt.Execute(ctx, intent)
		if err != nil {
			t.Fatalf("Execute(%s): %v", intent.IntentID, err)
		}
		if result.Status == reducercontract.ResultStatusSuperseded {
			t.Fatalf("Execute(%s) superseded (generation %s): the replay was dropped", intent.IntentID, intent.GenerationID)
		}
		if err := queue.Ack(ctx, intent, result); err != nil {
			t.Fatalf("Ack(%s): %v", intent.IntentID, err)
		}
	}
	t.Fatal("queue did not drain within 8 claims")
}

// testCrossplaneStaleRowCoalesceLive is the #10269 acceptance proof, run as
// the stale_coalesce subtest of the scheduled redrive proof. The shim's
// seven guard cases run over the real ReducerQueue and Runtime: stale
// per-generation rows coalesce onto the newest pending/retrying carrier,
// while the #10106 rebind debt (sole stale row, dead-letter newest) is
// never absorbed. Since #10863 the pair is the read-only claim gate plus
// the periodic sweep: claims fence stale siblings at hand-out and the
// sweep terminalizes them, so the drains below tick the sweep between
// claims exactly as the already-retargeted parent-package coalesce
// proofs do.
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
		// The drain helper caps at 8 claims; 5 scopes need 5.
		drainCoalesceQueue(ctx, t, queue, rt)

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

	// Renamed from "collapse in one batch claim": since #10863 the batch
	// claim only fences the stale siblings, and the sweep pass ticking
	// before it is what terminalizes them. The execution assertions are
	// unchanged.
	t.Run("three queued generations collapse under sweep plus batch claim", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "chain.example.org", "XChainClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "chain-xrd", "chain-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-chain", time.Minute)
		seedCoalesceScope(ctx, t, db, queue, "coal-chain",
			[]string{"coal-chain-g1", "coal-chain-g2", "coal-chain-g3"}, group, kind, now)

		if swept := coalesceSweep(ctx, t, queue); swept != 2 {
			t.Fatalf("sweep superseded %d rows, want the 2 stale siblings", swept)
		}
		intents, err := queue.ClaimBatch(ctx, 8)
		if err != nil {
			t.Fatalf("ClaimBatch: %v", err)
		}
		if len(intents) != 1 || intents[0].GenerationID != "coal-chain-g3" {
			t.Fatalf("ClaimBatch intents = %v, want only the newest carrier coal-chain-g3", intents)
		}
		for _, gen := range []string{"coal-chain-g1", "coal-chain-g2"} {
			if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-chain", gen); got != "superseded" {
				t.Errorf("stale row %s status = %q, want superseded by the sweep before the batch claim", gen, got)
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
		drainCoalesceQueue(ctx, t, queue, rt)
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
		drainCoalesceQueue(ctx, t, queue, rt)
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
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "epq.example.org", "XEpqClaim"
		seedCrossplaneRedriveXRD(ctx, t, db, "epq-xrd", "epq-xrd-gen", group, kind, now.Add(time.Minute))
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "coalesce-epq-seed", time.Minute)
		seedCoalesceScope(ctx, t, db, queue, "coal-epq",
			[]string{"coal-epq-g1", "coal-epq-g2"}, group, kind, now)

		// Another worker leases the carrier g2 and commits before the
		// sweep's lock step runs (the parent package's
		// carrier-claimed-before-lock retarget). Carrier membership is
		// membership in that lock step's output, decided by row-self
		// status quals, so the claimed carrier never enters the locked
		// set: with no locked carrier, the sweep spares g1 as the #10106
		// rebind fallback, and the claim gate fences it only while the
		// carrier stays in flight. The pre-#10863 form raced the carrier
		// claim against the claim's own blocking lock step; claims no
		// longer lock stale rows, and the standalone sweep is SKIP
		// LOCKED, so that blocked-pid interleaving no longer exists.
		crossplaneRedriveMustExec(ctx, t, db, `
			UPDATE fact_work_items
			SET status = 'claimed', lease_owner = 'epq-carrier-worker', claim_until = $1
			WHERE scope_id = 'coal-epq' AND generation_id = 'coal-epq-g2'
			  AND domain = 'crossplane_satisfied_by_materialization'
		`, time.Now().UTC().Add(time.Hour))
		if swept := coalesceSweep(ctx, t, queue); swept != 0 {
			t.Fatalf("sweep superseded %d rows, want 0 (claimed carrier never counts)", swept)
		}
		if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
			t.Fatalf("Claim = claimed %t err %v, want no hand-out while the carrier is leased", claimed, err)
		}
		if got := quietDeltaWorkItemStatus(ctx, t, db, "coal-epq", "coal-epq-g1"); got != "pending" {
			t.Fatalf("stale row status = %q, want pending: superseding it against a carrier claimed before the lock would destroy the rebind fallback", got)
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
		drainCoalesceQueue(ctx, t, queue, rt)
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

		// One sweeper pass ticks before the race, as the independent
		// production sweeper may: it terminalizes both stale siblings
		// against the still-pending newest carrier. Racing first would
		// leave the middle row as the in-flight carrier's rebind
		// fallback, so this is the order that keeps the superseded
		// assertions below at full strength; the two claimers then race
		// for the sole surviving carrier.
		if swept := coalesceSweep(ctx, t, seedQueue); swept != 2 {
			t.Fatalf("sweep superseded %d rows, want the 2 stale siblings", swept)
		}

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
