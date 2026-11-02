// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestParityStaleWriterCommitFence is the #9376 T12 pin-then-commit parity
// scenario. On each leg a claim is taken (writer 1, token 1), its lease
// lapses and the reaper reclaims the item, writer 2 claims it at token 2 and
// commits the generation into core, and then writer 1 tries to commit.
// The in-tree leg fences writer 1 with the core claim heartbeat on the fact
// transaction; the satellite leg fences it with the claim-plane pin before
// any core transaction. Both legs must commit writer 2, refuse writer 1, and
// leave none of writer 1's facts in core.
func TestParityStaleWriterCommitFence(t *testing.T) {
	ctx := context.Background()
	inTree, coreDB := openInTreeBackendWithDB(t, os.Getenv("PARITY_INTREE_DSN"))
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })
	satellite := OpenSatellite(t, os.Getenv("PARITY_SATELLITE_URL"))

	inTreeSuite := NewSuite(t, "commit-fence-in-tree")
	inTreeOut := runStaleWriterCommitFence(ctx, inTreeSuite, inTree, coreDB, nil)
	satelliteSuite := NewSuite(t, "commit-fence-satellite")
	satelliteSuite.Now = inTreeSuite.Now
	pinner := satellite.(*satelliteBackend).client
	satelliteOut := runStaleWriterCommitFence(ctx, satelliteSuite, satellite, coreDB, pinner)
	inTreeOut.Equal(t, "stale-writer-commit-fence", satelliteOut)
}

func runStaleWriterCommitFence(ctx context.Context, s *Suite, backend Backend, coreDB *sql.DB, pinner fence.Pinner) *Outcome {
	t := s.T
	t.Helper()
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 1)
	item, first, issued, err := backend.ClaimNextEligible(ctx, s.Selector("writer-1", s.RunID+"-claim-1"), s.Now, s.Lease)
	if err != nil || !issued {
		t.Fatalf("%s: writer 1 claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	reapAt := s.Now.Add(10 * time.Minute)
	reaped, err := backend.ReapExpiredClaims(ctx, reapAt, 10, 0)
	if err != nil || len(reaped) != 1 {
		t.Fatalf("%s: reap writer 1: reaped=%d err=%v", backend.Name(), len(reaped), err)
	}
	claimAt := reapAt.Add(time.Minute)
	_, second, issued, err := backend.ClaimNextEligible(ctx, s.Selector("writer-2", s.RunID+"-claim-2"), claimAt, s.Lease)
	if err != nil || !issued {
		t.Fatalf("%s: writer 2 claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("tokens first=%d second=%d", first.FencingToken, second.FencingToken)

	commit := func(claim workflow.Claim, factID string) error {
		store := postgres.NewIngestionStore(postgres.SQLDB{DB: coreDB})
		store.Now = func() time.Time { return claimAt }
		if pinner != nil {
			store.ClaimPlane = &postgres.ClaimPlaneFence{Pinner: pinner}
		}
		mutation := s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)
		mutation.ObservedAt = claimAt
		mutation.ScopeID, mutation.SourceRunID, mutation.GenerationID = item.ScopeID, item.SourceRunID, item.GenerationID
		stream := make(chan facts.Envelope, 1)
		stream <- facts.Envelope{
			FactID: s.RunID + "-" + factID, ScopeID: item.ScopeID, GenerationID: item.GenerationID,
			FactKind: "jira_issue", StableFactKey: factID, FencingToken: claim.FencingToken, ObservedAt: claimAt,
			SourceRef: facts.Ref{SourceSystem: "jira", FactKey: factID},
		}
		close(stream)
		return store.CommitClaimedScopeGeneration(ctx, mutation, scope.IngestionScope{
			ScopeID: item.ScopeID, SourceSystem: "jira", ScopeKind: scope.KindRepository,
			CollectorKind: item.CollectorKind, PartitionKey: item.ScopeID,
		}, scope.ScopeGeneration{
			GenerationID: item.GenerationID, ScopeID: item.ScopeID, ObservedAt: claimAt, IngestedAt: claimAt,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		}, stream)
	}

	out.Call("writer 2 commit", commit(second, "fact-writer-2"))
	staleErr := commit(first, "fact-writer-1")
	var rejection *fence.Rejection
	switch {
	case errors.Is(staleErr, workflow.ErrClaimRejected):
		out.Record("writer 1 refused stale")
	case errors.As(staleErr, &rejection) && rejection.Reason == fence.ReasonStaleFence && rejection.TerminalFailure():
		out.Record("writer 1 refused stale")
	default:
		t.Fatalf("%s: writer 1 commit error = %v, want a stale-claim refusal", backend.Name(), staleErr)
	}
	var staleFacts, freshFacts int
	if err := coreDB.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE fact_id = $1), count(*) FILTER (WHERE fact_id = $2)
		FROM fact_records WHERE generation_id = $3`, s.RunID+"-fact-writer-1", s.RunID+"-fact-writer-2", item.GenerationID).
		Scan(&staleFacts, &freshFacts); err != nil {
		t.Fatalf("%s: count facts: %v", backend.Name(), err)
	}
	out.Record("core facts stale=%d fresh=%d", staleFacts, freshFacts)
	complete := s.Mutation(item.WorkItemID, second.ClaimID, second.FencingToken, second.OwnerID)
	complete.ObservedAt = claimAt
	out.Call("writer 2 complete", backend.CompleteClaim(ctx, complete))
	drainScenario(s, ctx, backend, out)
	return out
}
