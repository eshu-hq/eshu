// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func guardFixture(allowReap bool) (*guardedBackend, *fakeLeg, *[]*Violation) {
	var seen []*Violation
	inner := newFakeLeg(faults{}, true, 0)
	return newGuardedBackend(inner, allowReap, func(v *Violation) { seen = append(seen, v) }), inner, &seen
}

func syntheticItem() workflow.WorkItem {
	return newItem(Prefix+"wi-1-0", Prefix+"run-1-0", Prefix+"scope-0", "terraform_state", time.Now().UTC())
}

// TestGuardRefusesWritesOutsideTheSyntheticInstance: every write that names
// another collector instance, a non-synthetic id, or a global route is
// refused before it reaches the backend, and the refusal aborts the run.
func TestGuardRefusesWritesOutsideTheSyntheticInstance(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	foreign := syntheticItem()
	foreign.CollectorInstanceID = "synth-8875-tfstate"
	realID := syntheticItem()
	realID.WorkItemID = "wi-real"
	realAU := syntheticItem()
	realAU.AcceptanceUnitID = "au-real"
	realGen := syntheticItem()
	realGen.GenerationID = "gen-real"
	realSource := syntheticItem()
	realSource.SourceSystem = "terraform"
	realSourceRun := syntheticItem()
	realSourceRun.SourceRunID = "sr-real"
	scopedRun := newRun(Prefix+"run-1-8", now)
	scopedRun.RequestedScopeSet = `["scope-real"]`
	collectorRun := newRun(Prefix+"run-1-9", now)
	collectorRun.RequestedCollector = "terraform-state-real"
	cases := map[string]func(g *guardedBackend) error{
		"enqueue on another instance": func(g *guardedBackend) error {
			return g.EnqueueWorkItems(ctx, []workflow.WorkItem{foreign})
		},
		"enqueue a non-synthetic id": func(g *guardedBackend) error {
			return g.EnqueueWorkItems(ctx, []workflow.WorkItem{realID})
		},
		"admit on another instance": func(g *guardedBackend) error {
			_, err := g.AdmitGuarded(ctx, newRun(Prefix+"run-1-9", now), []workflow.WorkItem{foreign})
			return err
		},
		"create a non-synthetic run": func(g *guardedBackend) error { return g.CreateRun(ctx, newRun("run-real", now)) },
		"enqueue a real acceptance unit": func(g *guardedBackend) error {
			return g.EnqueueWorkItems(ctx, []workflow.WorkItem{realAU})
		},
		"enqueue a real generation": func(g *guardedBackend) error {
			return g.EnqueueWorkItems(ctx, []workflow.WorkItem{realGen})
		},
		"enqueue a real source system": func(g *guardedBackend) error {
			return g.EnqueueWorkItems(ctx, []workflow.WorkItem{realSource})
		},
		"enqueue a real source run": func(g *guardedBackend) error {
			return g.EnqueueWorkItems(ctx, []workflow.WorkItem{realSourceRun})
		},
		"create a run naming a real scope set": func(g *guardedBackend) error { return g.CreateRun(ctx, scopedRun) },
		"create a run naming a real collector": func(g *guardedBackend) error { return g.CreateRun(ctx, collectorRun) },
		"claim on another instance": func(g *guardedBackend) error {
			_, _, _, err := g.ClaimNextEligible(ctx, workflow.ClaimSelector{
				CollectorKind:       "terraform_state",
				CollectorInstanceID: "synth-8875-tfstate", OwnerID: Prefix + "owner-0", ClaimID: Prefix + "claim-0",
			}, now, time.Minute)
			return err
		},
		"complete a non-synthetic claim": func(g *guardedBackend) error {
			return g.CompleteClaim(ctx, workflow.ClaimMutation{WorkItemID: "wi-real", ClaimID: Prefix + "c", OwnerID: Prefix + "o"})
		},
		"pin a non-synthetic scope": func(g *guardedBackend) error {
			_, err := g.PinForCommit(ctx, Prefix+"c", 1, "scope-real", Prefix+"sr", Prefix+"gen", now)
			return err
		},
		"reconcile collector instances": func(g *guardedBackend) error {
			return g.ReconcileCollectorInstances(ctx, now, nil)
		},
		"reconcile workflow runs": func(g *guardedBackend) error {
			_, err := g.ReconcileWorkflowRuns(ctx, now)
			return err
		},
		"upsert completeness": func(g *guardedBackend) error { return g.UpsertCompletenessStates(ctx, nil) },
		"reap without the harness reaper": func(g *guardedBackend) error {
			_, err := g.ReapExpiredClaims(ctx, now, 10, time.Second)
			return err
		},
	}
	for name, call := range cases {
		g, inner, seen := guardFixture(false)
		err := call(g)
		if !errors.Is(err, ErrForeignWrite) {
			t.Fatalf("%s: err = %v, want ErrForeignWrite", name, err)
		}
		if len(*seen) != 1 || (*seen)[0].Trigger != TriggerNonSynthetic {
			t.Fatalf("%s: abort callback = %v, want one %s", name, *seen, TriggerNonSynthetic)
		}
		if len(inner.items) != 0 {
			t.Fatalf("%s: refused write reached the backend", name)
		}
	}
}

func TestGuardPassesSyntheticWrites(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	g, inner, seen := guardFixture(true)
	if err := g.CreateRun(ctx, newRun(Prefix+"run-1-0", now)); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := g.EnqueueWorkItems(ctx, []workflow.WorkItem{syntheticItem()}); err != nil {
		t.Fatalf("EnqueueWorkItems: %v", err)
	}
	_, claim, issued, err := g.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       "terraform_state",
		CollectorInstanceID: InstanceID, OwnerID: Prefix + "owner-0", ClaimID: Prefix + "claim-0",
	}, now, time.Minute)
	if err != nil || !issued {
		t.Fatalf("claim: issued=%v err=%v", issued, err)
	}
	if _, err := g.ReapExpiredClaims(ctx, now, 10, time.Second); err != nil {
		t.Fatalf("reap with the harness reaper: %v", err)
	}
	if _, err := g.PinForCommit(ctx, claim.ClaimID, claim.FencingToken, Prefix+"scope-0",
		Prefix+"sr-"+Prefix+"wi-1-0", Prefix+"gen-"+Prefix+"wi-1-0", now.Add(time.Minute)); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if len(*seen) != 0 || len(inner.items) != 1 {
		t.Fatalf("synthetic writes refused (%v) or lost (%d items)", *seen, len(inner.items))
	}
}
