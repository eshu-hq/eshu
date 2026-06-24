// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

// blastSignal builds a replatformingItemSignal for tests.
func blastSignal(dependencyCount, missingEvidenceCount int) replatformingItemSignal {
	return replatformingItemSignal{
		DependencyCount:      dependencyCount,
		MissingEvidenceCount: missingEvidenceCount,
	}
}

// waveTestItem builds a minimal valid migration packet item for wave tests.
func waveTestItem(id string, state ReplatformingSourceState, importReady bool, gate IaCManagementSafetyGate) MigrationPacketItem {
	item := MigrationPacketItem{
		ItemID:       id,
		Provider:     "aws",
		ResourceType: "aws_s3_bucket",
		StableID:     "arn:aws:s3:::" + id,
		SourceState:  state,
		SafetyGate:   gate,
	}
	if importReady {
		item.ImportCandidate = &ReplatformingImportCandidate{
			Status:       ReplatformingImportStatusReady,
			ResourceType: "aws_s3_bucket",
			ImportBlock:  "import {\n  to = aws_s3_bucket." + id + "\n  id = \"" + id + "\"\n}",
		}
	} else {
		item.ImportCandidate = &ReplatformingImportCandidate{
			Status:         ReplatformingImportStatusRefused,
			ResourceType:   "aws_s3_bucket",
			RefusalReasons: []string{"security_review_required"},
		}
	}
	return item
}

func readOnlyGate() IaCManagementSafetyGate {
	return IaCManagementSafetyGate{Outcome: "read_only", ReadOnly: true}
}

func reviewGate() IaCManagementSafetyGate {
	return IaCManagementSafetyGate{Outcome: "review_required", ReviewRequired: true}
}

// TestAssignReplatformingWavesOrdersEarlySafeFirst proves independent, import-ready,
// low-blast-radius items land in the first wave and gated items land last.
func TestAssignReplatformingWavesOrdersEarlySafeFirst(t *testing.T) {
	t.Parallel()

	items := []MigrationPacketItem{
		waveTestItem("safe", ReplatformingSourceStateDerived, true, readOnlyGate()),
		waveTestItem("gated", ReplatformingSourceStateRejected, false, reviewGate()),
		waveTestItem("review", ReplatformingSourceStateStale, false, readOnlyGate()),
	}
	signals := map[string]replatformingItemSignal{
		"safe":   blastSignal(0, 0),
		"gated":  blastSignal(1, 0),
		"review": blastSignal(2, 1),
	}

	waves, groups := assignReplatformingWaves(items, signals)

	if len(waves) == 0 {
		t.Fatal("expected at least one wave")
	}
	// Waves must be strictly increasing in order, starting at 1.
	for i, wave := range waves {
		if wave.Order != i+1 {
			t.Fatalf("wave[%d].Order = %d, want %d", i, wave.Order, i+1)
		}
		if wave.ID == "" {
			t.Fatalf("wave[%d] has empty id", i)
		}
		if wave.Rationale == "" {
			t.Fatalf("wave[%d] has empty rationale", i)
		}
	}

	// The safe item must be in an earlier wave than the review item, which must be
	// earlier than the gated item.
	order := waveOrderByItem(waves)
	if order["safe"] >= order["review"] {
		t.Fatalf("safe wave %d not earlier than review wave %d", order["safe"], order["review"])
	}
	if order["review"] >= order["gated"] {
		t.Fatalf("review wave %d not earlier than gated wave %d", order["review"], order["gated"])
	}

	// The last wave must be the blocked/gated wave.
	last := waves[len(waves)-1]
	if !reflect.DeepEqual(last.ItemIDs, []string{"gated"}) {
		t.Fatalf("last wave item_ids = %v, want [gated]", last.ItemIDs)
	}

	if len(groups) == 0 {
		t.Fatal("expected at least one blast-radius group")
	}
}

// waveOrderByItem maps each item id to the order of the wave that owns it.
func waveOrderByItem(waves []MigrationWave) map[string]int {
	out := map[string]int{}
	for _, wave := range waves {
		for _, id := range wave.ItemIDs {
			out[id] = wave.Order
		}
	}
	return out
}

// TestAssignReplatformingWavesIsDeterministic proves repeated runs and shuffled
// input produce identical wave and group output (no map-iteration nondeterminism).
func TestAssignReplatformingWavesIsDeterministic(t *testing.T) {
	t.Parallel()

	items := []MigrationPacketItem{
		waveTestItem("b", ReplatformingSourceStateDerived, true, readOnlyGate()),
		waveTestItem("a", ReplatformingSourceStateDerived, true, readOnlyGate()),
		waveTestItem("c", ReplatformingSourceStateAmbiguous, false, readOnlyGate()),
		waveTestItem("d", ReplatformingSourceStateExact, true, readOnlyGate()),
	}
	signals := map[string]replatformingItemSignal{
		"a": blastSignal(0, 0),
		"b": blastSignal(0, 0),
		"c": blastSignal(7, 2),
		"d": blastSignal(3, 0),
	}

	waves1, groups1 := assignReplatformingWaves(items, signals)
	// Shuffle the input order; output must not change.
	shuffled := []MigrationPacketItem{items[3], items[0], items[2], items[1]}
	waves2, groups2 := assignReplatformingWaves(shuffled, signals)

	if !reflect.DeepEqual(waves1, waves2) {
		t.Fatalf("waves not deterministic under input reordering:\n%+v\n%+v", waves1, waves2)
	}
	if !reflect.DeepEqual(groups1, groups2) {
		t.Fatalf("groups not deterministic under input reordering:\n%+v\n%+v", groups1, groups2)
	}

	// Within a wave, item_ids must be sorted so output is stable.
	for _, wave := range waves1 {
		for i := 1; i < len(wave.ItemIDs); i++ {
			if wave.ItemIDs[i-1] > wave.ItemIDs[i] {
				t.Fatalf("wave %q item_ids not sorted: %v", wave.ID, wave.ItemIDs)
			}
		}
	}
}

// TestAssignReplatformingWavesBlastRadiusSeverity proves dependency count drives
// blast-radius severity bucketing and that ambiguous/gated items form explicit
// risk groups regardless of dependency count.
func TestAssignReplatformingWavesBlastRadiusSeverity(t *testing.T) {
	t.Parallel()

	items := []MigrationPacketItem{
		waveTestItem("none", ReplatformingSourceStateDerived, true, readOnlyGate()),
		waveTestItem("high", ReplatformingSourceStateDerived, true, readOnlyGate()),
		waveTestItem("ambig", ReplatformingSourceStateAmbiguous, false, readOnlyGate()),
	}
	signals := map[string]replatformingItemSignal{
		"none":  blastSignal(0, 0),
		"high":  blastSignal(8, 0),
		"ambig": blastSignal(0, 3),
	}

	_, groups := assignReplatformingWaves(items, signals)

	sev := map[string]string{}
	for _, group := range groups {
		for _, id := range group.ItemIDs {
			sev[id] = group.Severity
		}
		if group.ID == "" || group.Reason == "" {
			t.Fatalf("blast-radius group missing id/reason: %+v", group)
		}
	}
	if sev["none"] != replatformingBlastSeverityNone {
		t.Fatalf("none severity = %q, want %q", sev["none"], replatformingBlastSeverityNone)
	}
	if sev["high"] != replatformingBlastSeverityHigh {
		t.Fatalf("high severity = %q, want %q", sev["high"], replatformingBlastSeverityHigh)
	}
	if sev["ambig"] != replatformingBlastSeverityBlocked {
		t.Fatalf("ambig severity = %q, want %q (ambiguous must be blocked group)", sev["ambig"], replatformingBlastSeverityBlocked)
	}
}

// TestAssignReplatformingWavesEmptyPlan proves an empty item set yields no waves
// or groups rather than a panic or a fabricated wave.
func TestAssignReplatformingWavesEmptyPlan(t *testing.T) {
	t.Parallel()

	waves, groups := assignReplatformingWaves(nil, nil)
	if len(waves) != 0 {
		t.Fatalf("empty plan waves = %v, want none", waves)
	}
	if len(groups) != 0 {
		t.Fatalf("empty plan groups = %v, want none", groups)
	}
}

// TestApplyReplatformingWavesAssignsItemMembershipAndValidates proves the plan-level
// helper assigns each item's wave_id/blast_radius_group and leaves the plan valid.
func TestApplyReplatformingWavesAssignsItemMembershipAndValidates(t *testing.T) {
	t.Parallel()

	plan := NewReplatformingPlan(ReplatformingPlanScope{Kind: ReplatformingScopeAccount, Account: "111122223333"})
	plan.Items = []MigrationPacketItem{
		waveTestItem("safe", ReplatformingSourceStateDerived, true, readOnlyGate()),
		waveTestItem("gated", ReplatformingSourceStateRejected, false, reviewGate()),
	}
	signals := map[string]replatformingItemSignal{
		"safe":  blastSignal(0, 0),
		"gated": blastSignal(2, 0),
	}

	applyReplatformingWaves(&plan, signals)

	if len(plan.Waves) == 0 {
		t.Fatal("expected populated waves on plan")
	}
	if len(plan.BlastRadiusGroups) == 0 {
		t.Fatal("expected populated blast-radius groups on plan")
	}
	for _, item := range plan.Items {
		if item.WaveID == "" {
			t.Fatalf("item %q has no wave_id assigned", item.ItemID)
		}
		if item.BlastRadiusGroup == "" {
			t.Fatalf("item %q has no blast_radius_group assigned", item.ItemID)
		}
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan with waves failed Validate(): %v", err)
	}
}
