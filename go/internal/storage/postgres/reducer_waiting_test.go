// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
)

// TestWaitingDeferralMapCoversEveryNonCountingClass fails when a readiness
// class exempt from the retry budget has no waiting-wake map row (#10876,
// acceptance criterion 1). Every parked wait needs a named waits-for and a
// providing save, or the set-aside cannot wake it.
func TestWaitingDeferralMapCoversEveryNonCountingClass(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for _, entry := range WaitingDeferralMap() {
		seen[entry.FailureClass]++
	}
	for _, class := range NonCountingReducerRetryFailureClasses() {
		if seen[class] != 1 {
			t.Errorf("non-counting class %q has %d deferral map rows, want exactly 1", class, seen[class])
		}
	}
}

// TestWaitingDeferralMapCoversOwnGateCountingClasses pins the counting side
// of the map: own-gate counting classes are never parked, and the map must
// say so explicitly so a later enrollment cannot silently park a loud dead
// letter (#10876 decision 2).
func TestWaitingDeferralMapCoversOwnGateCountingClasses(t *testing.T) {
	t.Parallel()

	byClass := map[string]WaitingDeferral{}
	for _, entry := range WaitingDeferralMap() {
		byClass[entry.FailureClass] = entry
	}
	for _, class := range ownGateCountingClasses() {
		entry, ok := byClass[class]
		if !ok {
			t.Errorf("own-gate counting class %q has no deferral map row", class)
			continue
		}
		if entry.WakeSite != WaitingWakeSiteCounting {
			t.Errorf("own-gate counting class %q wake site = %q, want %q (never parked)",
				class, entry.WakeSite, WaitingWakeSiteCounting)
		}
	}
}

// TestWaitingWakeKindIsClosed pins the waits_for_kind enum (#10876 decision
// 5a): every map row names one of the seven kinds, so #10877 can narrow the
// corpus fence to per-item read sets without a schema change.
func TestWaitingWakeKindIsClosed(t *testing.T) {
	t.Parallel()

	for _, entry := range WaitingDeferralMap() {
		if !entry.WaitsForKind.Valid() {
			t.Errorf("class %q names invalid waits_for_kind %q", entry.FailureClass, string(entry.WaitsForKind))
		}
		if entry.WaitsForKind == "" {
			t.Errorf("class %q has an empty waits_for_kind", entry.FailureClass)
		}
	}
}

// TestWaitingWriteSupersededClassesAreNotWaits pins that fresher-admission
// losses are not waits: no providing save will ever arrive for that
// generation, so parking them would wait forever (#10876 decision 1vii).
func TestWaitingWriteSupersededClassesAreNotWaits(t *testing.T) {
	t.Parallel()

	byClass := map[string]WaitingDeferral{}
	for _, entry := range WaitingDeferralMap() {
		byClass[entry.FailureClass] = entry
	}
	for _, class := range []string{
		"aws_cloud_runtime_drift_write_superseded",
		"aws_cloud_runtime_drift_state_pending",
		"supply_chain_impact_write_superseded",
	} {
		entry, ok := byClass[class]
		if !ok {
			t.Errorf("write-superseded class %q has no deferral map row", class)
			continue
		}
		if entry.WakeSite != WaitingWakeSiteNotAWait {
			t.Errorf("write-superseded class %q wake site = %q, want %q",
				class, entry.WakeSite, WaitingWakeSiteNotAWait)
		}
	}
}
