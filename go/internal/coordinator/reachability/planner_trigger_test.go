// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestPlanZeroTriggerKindIsByteIdentical pins the ruling invariant: omitting
// TriggerKind derives exactly today's bootstrap/schedule RunID, trigger, and
// item identity, so existing and future scheduled runs do not move.
func TestPlanZeroTriggerKindIsByteIdentical(t *testing.T) {
	t.Parallel()

	repository := goRepository("00000001", "eshu-hq", commitA)

	zeroRun, zeroItems, _, err := Plan(testRequest(repository))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	legacyRunID := RunID(testInstance(), "go-modules-scheduled-20261104T120000Z")
	if zeroRun.RunID != legacyRunID {
		t.Fatalf("zero-trigger RunID = %q, want legacy %q", zeroRun.RunID, legacyRunID)
	}
	if zeroRun.TriggerKind != workflow.TriggerKindSchedule {
		t.Fatalf("zero-trigger TriggerKind = %q, want %q", zeroRun.TriggerKind, workflow.TriggerKindSchedule)
	}

	derived := testRequest(repository)
	derived.TriggerKind = workflow.TriggerKindSchedule
	derivedRun, derivedItems, _, err := Plan(derived)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if derivedRun.RunID != zeroRun.RunID || derivedRun.TriggerKind != zeroRun.TriggerKind {
		t.Fatalf("derived run = %+v, want %+v", derivedRun, zeroRun)
	}
	if derivedItems[0].WorkItemID != zeroItems[0].WorkItemID ||
		derivedItems[0].GenerationID != zeroItems[0].GenerationID {
		t.Fatalf("derived item = %+v, want %+v", derivedItems[0], zeroItems[0])
	}
}

// TestPlanOperatorRecoveryTriggerKindsTheRun proves an explicit
// operator_recovery request lands in the RunID's trigger segment and the run,
// distinguishing the operator run from any scheduled run forever.
func TestPlanOperatorRecoveryTriggerKindsTheRun(t *testing.T) {
	t.Parallel()

	request := testRequest(goRepository("00000001", "eshu-hq", commitA))
	request.TriggerKind = workflow.TriggerKindOperatorRecovery
	request.PlanKey = "go-modules-operator-10032-fixture-20261123"

	run, items, _, err := Plan(request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	wantRunID := "scanner_worker:scanner-go-reachability:operator_recovery:derived:go-modules-operator-10032-fixture-20261123"
	if run.RunID != wantRunID {
		t.Fatalf("RunID = %q, want %q", run.RunID, wantRunID)
	}
	if run.TriggerKind != workflow.TriggerKindOperatorRecovery {
		t.Fatalf("TriggerKind = %q, want %q", run.TriggerKind, workflow.TriggerKindOperatorRecovery)
	}
	if len(items) != 1 || items[0].RunID != wantRunID {
		t.Fatalf("items = %+v, want one item under %q", items, wantRunID)
	}
}

// TestPlanRefusesNonOperatorExplicitTriggerKinds proves webhook, replay, and
// unknown kinds are refused on the reachability path: they have no meaning
// for a Go repository page.
func TestPlanRefusesNonOperatorExplicitTriggerKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []workflow.TriggerKind{
		workflow.TriggerKindWebhook, workflow.TriggerKindReplay, workflow.TriggerKindBootstrap, "manual",
	} {
		request := testRequest(goRepository("00000001", "eshu-hq", commitA))
		request.TriggerKind = kind
		if _, _, _, err := Plan(request); err == nil {
			t.Errorf("Plan(TriggerKind=%q) = nil, want refusal", kind)
		}
	}
}
