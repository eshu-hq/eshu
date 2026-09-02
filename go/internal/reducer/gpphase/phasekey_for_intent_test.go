// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import "testing"

// TestAcceptanceUnitIDPrefersTheFirstNonBlankEntityKey pins the derivation a
// readiness key is built from: a readiness fact is per acceptance unit, so a
// scope that projects several units must not collapse them onto the scope id.
func TestAcceptanceUnitIDPrefersTheFirstNonBlankEntityKey(t *testing.T) {
	t.Parallel()

	if got := AcceptanceUnitID("scope-1", []string{"  ", " unit-a ", "unit-b"}); got != "unit-a" {
		t.Fatalf("AcceptanceUnitID() = %q, want %q", got, "unit-a")
	}
	if got := AcceptanceUnitID(" scope-1 ", nil); got != "scope-1" {
		t.Fatalf("AcceptanceUnitID() = %q, want %q", got, "scope-1")
	}
	if got := AcceptanceUnitID("   ", []string{"", "   "}); got != "" {
		t.Fatalf("AcceptanceUnitID() = %q, want empty", got)
	}
}

// TestPhaseKeyForIntentBuildsAValidatableKey proves the built key passes
// PhaseKey.Validate, so a family that gates on it reads readiness under the same
// identity the publisher writes.
func TestPhaseKeyForIntentBuildsAValidatableKey(t *testing.T) {
	t.Parallel()

	key, ok := PhaseKeyForIntent("scope-1", "gen-1", []string{"unit-a"}, KeyspaceCloudResourceUID)
	if !ok {
		t.Fatal("PhaseKeyForIntent() ok = false, want true")
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	want := PhaseKey{
		ScopeID:          "scope-1",
		AcceptanceUnitID: "unit-a",
		SourceRunID:      "gen-1",
		GenerationID:     "gen-1",
		Keyspace:         KeyspaceCloudResourceUID,
	}
	if key != want {
		t.Fatalf("PhaseKeyForIntent() = %+v, want %+v", key, want)
	}
}

// TestPhaseKeyForIntentReportsFalseRatherThanABlankKey proves the gate closes on
// an intent that cannot name a bounded slice, instead of handing back a key
// Validate would reject.
func TestPhaseKeyForIntentReportsFalseRatherThanABlankKey(t *testing.T) {
	t.Parallel()

	for name, args := range map[string]struct {
		scopeID      string
		generationID string
		entityKeys   []string
	}{
		"blank scope":      {scopeID: "  ", generationID: "gen-1", entityKeys: []string{"unit-a"}},
		"blank generation": {scopeID: "scope-1", generationID: "", entityKeys: []string{"unit-a"}},
		"no acceptance unit": {
			scopeID:      "   ",
			generationID: "gen-1",
			entityKeys:   []string{"  "},
		},
	} {
		name, args := name, args
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			key, ok := PhaseKeyForIntent(args.scopeID, args.generationID, args.entityKeys, KeyspaceCloudResourceUID)
			if ok {
				t.Fatalf("PhaseKeyForIntent() ok = true, want false (key %+v)", key)
			}
			if key != (PhaseKey{}) {
				t.Fatalf("PhaseKeyForIntent() key = %+v, want zero value", key)
			}
		})
	}
}
