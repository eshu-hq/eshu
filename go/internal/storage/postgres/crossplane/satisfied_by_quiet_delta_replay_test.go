// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
)

// quietDeltaSpyEdgeWriter records each edge batch with the generation the
// handler stamped it under, so a rebound run proves which generation's
// snapshot it materialized.
type quietDeltaSpyEdgeWriter struct {
	mu      sync.Mutex
	written []map[string]any
	gens    []string
}

func (s *quietDeltaSpyEdgeWriter) WriteCrossplaneSatisfiedByEdges(
	_ context.Context, rows []map[string]any, _, generationID, _ string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, rows...)
	for range rows {
		s.gens = append(s.gens, generationID)
	}
	return nil
}

func (s *quietDeltaSpyEdgeWriter) RetractCrossplaneSatisfiedByEdges(
	context.Context, []string, string, string,
) error {
	return nil
}

func (s *quietDeltaSpyEdgeWriter) snapshot() ([]map[string]any, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.written...), append([]string(nil), s.gens...)
}

// quietDeltaRuntime builds the reducer runtime the way cmd/reducer does:
// NewDefaultRuntime over DefaultHandlers with the Postgres generation
// freshness check (see cmd/reducer/main.go).
func quietDeltaRuntime(t *testing.T, db *sql.DB, spy *quietDeltaSpyEdgeWriter) *reducer.Runtime {
	t.Helper()
	database := postgres.SQLDB{DB: db}
	handlers := reducer.DefaultHandlers{
		FactLoader:           postgres.NewFactStore(database),
		GenerationCheck:      postgres.NewGenerationFreshnessCheck(database),
		ActiveGeneration:     postgres.NewActiveGenerationID(database),
		PriorGenerationCheck: postgres.NewPriorGenerationCheck(database),
	}
	handlers.CrossplaneSatisfiedByEdgeWriter = spy
	rt, err := reducer.NewDefaultRuntime(handlers)
	if err != nil {
		t.Fatalf("NewDefaultRuntime: %v", err)
	}
	return rt
}

// testCrossplaneQuietDeltaReplaySurvivesSupersessionLive is the #10106
// regression, run as the quiet_delta_replay subtest of the scheduled
// TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive. A Crossplane
// SATISFIED_BY replay scheduled for a claim scope's full generation used to
// be dropped when a quiet delta of that scope activated before the reducer
// ran it: the claim sweep terminalized the still-pending row as superseded,
// Runtime.execute returned an already-claimed row as superseded, and the
// quiet delta derived no SATISFIED_BY intent of its own, so the edge stayed
// missing. Both interleavings must now re-run the replay against the delta
// generation's active snapshot through the real ReducerQueue and Runtime.
func testCrossplaneQuietDeltaReplaySurvivesSupersessionLive(t *testing.T) {
	t.Run("pending replay then quiet delta", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "quiet.example.org", "XQuietClaim"
		seedCrossplaneRedriveClaimScope(ctx, t, db, "quiet-app", "quiet-app-full", group, kind, 1, now)
		seedCrossplaneRedriveXRD(ctx, t, db, "quiet-xrd", "quiet-xrd-gen", group, kind, now.Add(time.Minute))

		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "quiet-delta-proof", time.Minute)
		sweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
			DB:       postgres.SQLQueryer{DB: db},
			State:    crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: db}),
			Replayer: queue,
			Owner:    "quiet-delta-proof",
		}
		result, err := sweeper.Sweep(ctx, "quiet-xrd", "quiet-xrd-gen")
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if result.TargetsEnqueued != 1 {
			t.Fatalf("Sweep targets = %d, want 1 (%+v)", result.TargetsEnqueued, result)
		}
		assertCrossplaneRedriveWorkItemPending(ctx, t, db, "quiet-app", "quiet-app-full")

		// The quiet delta activates before the reducer claims the replay.
		// It touches no K8s file, so it derives no intent of its own.
		seedCrossplaneRedriveDelta(ctx, t, db, "quiet-app", "quiet-app-delta", "c0", now.Add(2*time.Minute))

		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		drainQuietDeltaQueue(ctx, t, queue, rt)

		written, gens := spy.snapshot()
		if len(written) != 1 || written[0]["claim_uid"] != "quiet-app-uid-0" {
			t.Fatalf("SATISFIED_BY rows after quiet delta = %v, want the base claim quiet-app-uid-0", written)
		}
		if gens[0] != "quiet-app-delta" {
			t.Fatalf("edge generation = %q, want quiet-app-delta (rebound to the active snapshot)", gens[0])
		}
		if status := quietDeltaWorkItemStatus(ctx, t, db, "quiet-app", "quiet-app-full"); status != "succeeded" {
			t.Fatalf("replay work item status = %q, want succeeded", status)
		}
	})

	t.Run("claimed replay then quiet delta", func(t *testing.T) {
		db, _ := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "claimed.example.org", "XClaimedClaim"
		seedCrossplaneRedriveClaimScope(ctx, t, db, "claimed-app", "claimed-app-full", group, kind, 1, now)
		seedCrossplaneRedriveXRD(ctx, t, db, "claimed-xrd", "claimed-xrd-gen", group, kind, now.Add(time.Minute))

		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "quiet-delta-proof", time.Minute)
		scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, "claimed-app", "claimed-app-full")
		if err != nil || !scheduled {
			t.Fatalf("ReplayCrossplaneSatisfiedByMaterialization = %v, %v; want true, nil", scheduled, err)
		}
		intent, claimed, err := queue.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("Claim = %v, %v; want a claimed intent", claimed, err)
		}
		if intent.GenerationID != "claimed-app-full" {
			t.Fatalf("claimed generation = %q, want claimed-app-full", intent.GenerationID)
		}

		// The quiet delta activates after the claim, before execution.
		seedCrossplaneRedriveDelta(ctx, t, db, "claimed-app", "claimed-app-delta", "c0", now.Add(2*time.Minute))

		spy := &quietDeltaSpyEdgeWriter{}
		rt := quietDeltaRuntime(t, db, spy)
		result, err := rt.Execute(ctx, intent)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Status != reducercontract.ResultStatusSucceeded {
			t.Fatalf("Execute status = %q (%s), want succeeded against the delta snapshot", result.Status, result.EvidenceSummary)
		}
		if err := queue.Ack(ctx, intent, result); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		written, gens := spy.snapshot()
		if len(written) != 1 || gens[0] != "claimed-app-delta" {
			t.Fatalf("SATISFIED_BY rows = %v gens %v, want one edge under claimed-app-delta", written, gens)
		}
	})

	t.Run("concurrent claimers execute the rebound replay once", func(t *testing.T) {
		db, targetDSN := crossplaneRedriveProofDB(t)
		ctx := context.Background()
		now := time.Now().UTC()
		const group, kind = "race.example.org", "XRaceClaim"
		seedCrossplaneRedriveClaimScope(ctx, t, db, "race-app", "race-app-full", group, kind, 1, now)
		seedCrossplaneRedriveXRD(ctx, t, db, "race-xrd", "race-xrd-gen", group, kind, now.Add(time.Minute))

		seedQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "race-seed", time.Minute)
		if scheduled, err := seedQueue.ReplayCrossplaneSatisfiedByMaterialization(ctx, "race-app", "race-app-full"); err != nil || !scheduled {
			t.Fatalf("schedule replay = %v, %v; want true, nil", scheduled, err)
		}
		seedCrossplaneRedriveDelta(ctx, t, db, "race-app", "race-app-delta", "c0", now.Add(2*time.Minute))

		dbA := crossplaneRedriveProofConn(t, targetDSN)
		dbB := crossplaneRedriveProofConn(t, targetDSN)
		queues := []postgres.ReducerQueue{
			postgres.NewReducerQueue(postgres.SQLDB{DB: dbA}, "race-a", time.Minute),
			postgres.NewReducerQueue(postgres.SQLDB{DB: dbB}, "race-b", time.Minute),
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		claimedIntents := make([]reducercontract.Intent, 2)
		claimedOK := make([]bool, 2)
		claimErrs := make([]error, 2)
		for i := range queues {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				claimedIntents[i], claimedOK[i], claimErrs[i] = queues[i].Claim(ctx)
			}()
		}
		close(start)
		wg.Wait()
		winners := 0
		var winner postgres.ReducerQueue
		var winnerIntent reducercontract.Intent
		for i := range queues {
			if claimErrs[i] != nil {
				t.Fatalf("concurrent Claim[%d]: %v", i, claimErrs[i])
			}
			if claimedOK[i] {
				winners++
				winner, winnerIntent = queues[i], claimedIntents[i]
			}
		}
		if winners != 1 {
			t.Fatalf("concurrent claim winners = %d, want exactly 1", winners)
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
		if len(written) != 1 || gens[0] != "race-app-delta" {
			t.Fatalf("SATISFIED_BY rows = %v gens %v, want one edge under race-app-delta", written, gens)
		}
	})
}

// drainQuietDeltaQueue claims, executes, and acks through the real queue
// and runtime until no claimable work remains, failing on any superseded
// result: a dropped replay is the #10106 bug, not a drain outcome.
func drainQuietDeltaQueue(ctx context.Context, t *testing.T, queue postgres.ReducerQueue, rt *reducer.Runtime) {
	t.Helper()
	for range 8 {
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

func quietDeltaWorkItemStatus(ctx context.Context, t *testing.T, db *sql.DB, scopeID, generationID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx, `
		SELECT status FROM fact_work_items
		WHERE scope_id = $1 AND generation_id = $2
		  AND stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
	`, scopeID, generationID).Scan(&status); err != nil {
		t.Fatalf("read work item status: %v", err)
	}
	return status
}
