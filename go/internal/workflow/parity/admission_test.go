// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Duplicate admission probes the guarded admission path: the in-tree store
// admits scheduled work through CreateRunWithWorkItemsIfNoOpenTargets, an
// atomic per-target guard, while the satellite serves CreateRun and
// EnqueueWorkItems as separate calls with no equivalent route (a
// client-side read-then-write shim is refused per the #9428 ruling).
//
// TDD history: this file started as TestDuplicateAdmissionAlignment asserting
// the aligned behavior on both legs. It failed on the satellite leg
// (guard_unsupported vs ok), proving the gap; the final test below pins the
// documented pair instead, mirroring the TestParityPinGap precedent.

// guardedRun builds the admission run with an explicit id.
func guardedRun(s *Suite, id string) workflow.Run {
	return workflow.Run{
		RunID: id, TriggerKind: workflow.TriggerKindSchedule,
		Status:    workflow.RunStatusCollectionActive,
		CreatedAt: s.Now, UpdatedAt: s.Now,
	}
}

// guardedItems builds items for runID on the given scope set with the full
// guard tuple (kind, instance, scope, tenant, workspace, subject, policy,
// acceptance unit) held identical across admissions: only run, work-item,
// and generation identity differ (the #4586 bucket-edge shape). A second
// admission on the same tuple is a duplicate the guard must refuse, while
// no unique index can tell the admissions apart.
func guardedItems(s *Suite, runID string, first int, scopes []string) []workflow.WorkItem {
	items := make([]workflow.WorkItem, 0, len(scopes))
	for i, scopeID := range scopes {
		id := s.ItemID(first + i)
		items = append(items, workflow.WorkItem{
			WorkItemID: id, RunID: runID,
			CollectorKind:       "jira",
			CollectorInstanceID: "jira-primary",
			SourceSystem:        "jira", ScopeID: scopeID,
			AcceptanceUnitID: "au-" + scopeID, SourceRunID: "sr-" + id, GenerationID: "gen-" + id,
			Status:    workflow.WorkItemStatusPending,
			CreatedAt: s.Now, UpdatedAt: s.Now,
		})
	}
	return items
}

// completeAll claims and completes every pending item, bounded, so the
// scenario ends drained whatever the legs admitted.
func completeAll(s *Suite, ctx context.Context, backend Backend) {
	s.T.Helper()
	for round := range 8 {
		item, claim, issued, err := backend.ClaimNextEligible(
			ctx, s.Selector("owner-drain", fmt.Sprintf("%s-claim-drain-%d", s.RunID, round)), s.Now, s.Lease)
		if err != nil {
			s.T.Fatalf("%s: drain claim: %v", backend.Name(), err)
		}
		if !issued {
			return
		}
		if err := backend.CompleteClaim(ctx, s.Mutation(
			item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)); err != nil {
			s.T.Fatalf("%s: drain complete: %v", backend.Name(), err)
		}
	}
	s.T.Fatalf("%s: drain left work behind after 8 rounds", backend.Name())
}

// TestDuplicateAdmissionGap pins the arbiter-noted admission gap in place:
// the in-tree store refuses a second guarded admission on an already
// covered target tuple (eligible=0, inserted=0), while the satellite has no
// guarded admission route and reports ErrGuardedAdmissionNotSupported. Each
// leg asserts its own documented behavior, so movement on either side fails
// loudly: a satellite guarded route must update this test, never loosen it.
func TestDuplicateAdmissionGap(t *testing.T) {
	ctx := context.Background()
	inTree := openInTreeBackend(t, os.Getenv("PARITY_INTREE_DSN"))
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })
	satellite := OpenSatellite(t, os.Getenv("PARITY_SATELLITE_URL"))

	admit := func(s *Suite, backend Backend) *Outcome {
		out := &Outcome{}
		scopes := []string{"scope-" + s.ItemID(0), "scope-" + s.ItemID(1)}
		adm1, err := backend.AdmitGuarded(ctx, guardedRun(s, s.RunID+"-a"), guardedItems(s, s.RunID+"-a", 0, scopes))
		out.Record("admit-1 eligible=%d inserted=%d err=%s", adm1.EligibleTargets, adm1.InsertedWorkItems, Classify(err))
		adm2, err := backend.AdmitGuarded(ctx, guardedRun(s, s.RunID+"-b"), guardedItems(s, s.RunID+"-b", 2, scopes))
		out.Record("admit-2 eligible=%d inserted=%d err=%s", adm2.EligibleTargets, adm2.InsertedWorkItems, Classify(err))
		completeAll(s, ctx, backend)
		drainScenario(s, ctx, backend, out)
		return out
	}
	expect := func(t *testing.T, leg string, got *Outcome, want []string) {
		t.Helper()
		if len(got.Events) != len(want) {
			t.Fatalf("%s: event counts differ: got=%v want=%v", leg, got.Events, want)
		}
		for i := range want {
			if got.Events[i] != want[i] {
				t.Fatalf("%s: event %d differs: got=%v want=%v", leg, i, got.Events, want)
			}
		}
	}

	suiteA := NewSuite(t, "duplicate-admission")
	outA := admit(suiteA, inTree)
	expect(t, "in-tree", outA, []string{
		"admit-1 eligible=2 inserted=2 err=ok",
		"admit-2 eligible=0 inserted=0 err=ok",
		"drained=true",
	})
	suiteB := NewSuite(t, "duplicate-admission")
	suiteB.Now = suiteA.Now
	outB := admit(suiteB, satellite)
	expect(t, "satellite", outB, []string{
		"admit-1 eligible=0 inserted=0 err=fault",
		"admit-2 eligible=0 inserted=0 err=fault",
		"drained=true",
	})
	ResetSatellite(t, satelliteDSN)
}
