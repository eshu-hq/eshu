// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fencingRehearsalDigestEnv carries the satellite digest under test as a
// recorded field, never as a pinned constant: a deploy under a new digest
// needs no code change.
const fencingRehearsalDigestEnv = "ESHU_FENCING_DIGEST_UNDER_TEST"

// rehearsalSelector claims on the satellite-declared seed instance. The
// harness declares no instance row itself.
func rehearsalSelector(owner, claim string) workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: RehearsalInstanceID,
		OwnerID:             owner,
		ClaimID:             claim,
		RotationSalt:        "parity-fixed-salt",
	}
}

// seedRehearsalItem enqueues one pending item on the seed instance.
func seedRehearsalItem(t *testing.T, ctx context.Context, backend Backend, s *Suite, n int) workflow.WorkItem {
	t.Helper()
	id := s.ItemID(n)
	item := workflow.WorkItem{
		WorkItemID: id, RunID: s.RunID,
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: RehearsalInstanceID,
		SourceSystem:        "jira", ScopeID: "scope-" + id,
		AcceptanceUnitID: "au-" + id, SourceRunID: "sr-" + id, GenerationID: "gen-" + id,
		Status:    workflow.WorkItemStatusPending,
		CreatedAt: s.Now, UpdatedAt: s.Now,
	}
	if err := backend.EnqueueWorkItems(ctx, []workflow.WorkItem{item}); err != nil {
		t.Fatalf("%s: seed rehearsal item: %v", backend.Name(), err)
	}
	return item
}

// commitRehearsalGeneration commits one generation into core under the
// satellite pin, mirroring the production plane path.
func commitRehearsalGeneration(ctx context.Context, s *Suite, coreDB *sql.DB, pinner fence.Pinner,
	item workflow.WorkItem, claim workflow.Claim, at time.Time, factID string,
) error {
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: coreDB})
	store.Now = func() time.Time { return at }
	store.ClaimPlane = &postgres.ClaimPlaneFence{Pinner: pinner}
	mutation := s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)
	mutation.ObservedAt = at
	mutation.ScopeID, mutation.SourceRunID, mutation.GenerationID = item.ScopeID, item.SourceRunID, item.GenerationID
	stream := make(chan facts.Envelope, 1)
	stream <- facts.Envelope{
		FactID: s.RunID + "-" + factID, ScopeID: item.ScopeID, GenerationID: item.GenerationID,
		FactKind: "jira_issue", StableFactKey: factID, FencingToken: claim.FencingToken, ObservedAt: at,
		SourceRef: facts.Ref{SourceSystem: "jira", FactKey: factID},
	}
	close(stream)
	return store.CommitClaimedScopeGeneration(ctx, mutation, scope.IngestionScope{
		ScopeID: item.ScopeID, SourceSystem: "jira", ScopeKind: scope.KindRepository,
		CollectorKind: item.CollectorKind, PartitionKey: item.ScopeID,
	}, scope.ScopeGeneration{
		GenerationID: item.GenerationID, ScopeID: item.ScopeID, ObservedAt: at, IngestedAt: at,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
	}, stream)
}

// TestFencingSnapshotRehearsal is the #9376 criterion-3 live window: the
// stale-commit race, the ambiguous-timeout replay, the high-water snapshot
// assertion, and the rollback/teardown against the deployed satellite plus
// core. It skips without the satellite URL and DSN, exactly like the
// contention satellite leg, so the coordinator runs it in a granted window.
// The harness declares no collector instance row: the satellite declares
// RehearsalInstanceID from its own instances file.
func TestFencingSnapshotRehearsal(t *testing.T) {
	ctx := context.Background()
	_, coreDB := openInTreeBackendWithDB(t, os.Getenv("PARITY_INTREE_DSN"))
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })
	satellite := OpenSatellite(t, os.Getenv("PARITY_SATELLITE_URL"))
	pinner := satellite.(*satelliteBackend).client

	s := NewSuite(t, "fencing-snapshot")
	out := &Outcome{}
	digest := os.Getenv(fencingRehearsalDigestEnv)
	if digest == "" {
		digest = "not_declared"
	}
	out.Record("satellite digest=%s", digest)
	s.SeedRun(ctx, satellite)
	item := seedRehearsalItem(t, ctx, satellite, s, 0)
	var second workflow.Claim

	t.Run("stale-commit-race", func(t *testing.T) {
		_, first, issued, err := satellite.ClaimNextEligible(ctx, rehearsalSelector("writer-1", s.RunID+"-claim-1"), s.Now, s.Lease)
		if err != nil || !issued {
			t.Fatalf("%s: writer 1 claim: issued=%v err=%v", satellite.Name(), issued, err)
		}
		reapAt := s.Now.Add(10 * time.Minute)
		reaped, err := satellite.ReapExpiredClaims(ctx, reapAt, 10, 0)
		if err != nil || len(reaped) != 1 {
			t.Fatalf("%s: reap writer 1: reaped=%d err=%v", satellite.Name(), len(reaped), err)
		}
		claimAt := reapAt.Add(time.Minute)
		_, second, issued, err = satellite.ClaimNextEligible(ctx, rehearsalSelector("writer-2", s.RunID+"-claim-2"), claimAt, s.Lease)
		if err != nil || !issued {
			t.Fatalf("%s: writer 2 claim: issued=%v err=%v", satellite.Name(), issued, err)
		}
		out.Record("tokens first=%d second=%d", first.FencingToken, second.FencingToken)
		freshErr := commitRehearsalGeneration(ctx, s, coreDB, pinner, item, second, claimAt, "fact-writer-2")
		out.Call("writer 2 commit", freshErr)
		staleErr := commitRehearsalGeneration(ctx, s, coreDB, pinner, item, first, claimAt, "fact-writer-1")
		if err := CheckStaleRace(freshErr, staleErr); err != nil {
			t.Fatalf("%s: %v", satellite.Name(), err)
		}
		out.Record("writer 1 refused stale")
		complete := s.Mutation(item.WorkItemID, second.ClaimID, second.FencingToken, second.OwnerID)
		complete.ObservedAt = claimAt
		out.Call("writer 2 complete", satellite.CompleteClaim(ctx, complete))
	})

	t.Run("ambiguous-timeout-replay", func(t *testing.T) {
		deadline := s.Now.Add(s.Lease)
		request := fence.Request{
			WorkItemID: item.WorkItemID, ClaimID: second.ClaimID,
			FencingToken: second.FencingToken, ScopeID: item.ScopeID, SourceRunID: item.SourceRunID,
			GenerationID: item.GenerationID, Lease: s.Lease,
		}
		first, err := ReplayAmbiguousPin(ctx, pinner, workflow.ClaimPlaneErrorCode, request, deadline)
		if err != nil {
			t.Fatalf("%s: replay pin: %v", satellite.Name(), err)
		}
		// The identical-argument retry must keep the plane's first commit
		// id: a retry after an ambiguous timeout is idempotent.
		again, err := satellite.PinForCommit(ctx, second.ClaimID, second.FencingToken,
			item.ScopeID, item.SourceRunID, item.GenerationID, deadline)
		if err != nil {
			t.Fatalf("%s: re-pin: %v", satellite.Name(), err)
		}
		if first != again {
			t.Fatalf("%s: re-pin commit id %q differs from first pin %q; retry after timeout is not idempotent", satellite.Name(), again, first)
		}
		out.Record("replay keeps commit id")
	})

	t.Run("high-water-snapshot", func(t *testing.T) {
		rows, err := ReadFencingSnapshot(ctx, postgres.SQLDB{DB: coreDB}, s.RunID+"-%")
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		out.Record("snapshot digest=%s", SnapshotDigest(rows))
		want := map[FencingHighWaterKey]int64{{GenerationID: item.GenerationID, WorkItemID: item.WorkItemID}: second.FencingToken}
		if err := CheckHighWater(rows, want); err != nil {
			t.Fatalf("%s: %v", satellite.Name(), err)
		}
		out.Record("high-water holds token %d", second.FencingToken)
	})

	t.Run("rollback-teardown", func(t *testing.T) {
		deleted, err := TeardownRehearsalRows(ctx, postgres.SQLDB{DB: coreDB}, s.RunID)
		if err != nil {
			t.Fatalf("teardown: %v", err)
		}
		out.Record("teardown deleted facts=%d fences=%d generations=%d",
			deleted["fact_records"], deleted["generation_commit_fences"], deleted["scope_generations"])
		_, _, issued, err := satellite.ClaimNextEligible(ctx, rehearsalSelector("owner-drain", s.RunID+"-claim-drain"), s.Now, s.Lease)
		if err != nil || issued {
			t.Fatalf("%s: drain: issued=%v err=%v, rehearsal left work behind", satellite.Name(), issued, err)
		}
		out.Record("drained=true")
	})
}
