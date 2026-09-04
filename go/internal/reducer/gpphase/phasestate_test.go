// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestStateForIntentBuildsKeyedState(t *testing.T) {
	t.Parallel()

	observedAt := time.Unix(1700000000, 0).UTC()
	state, ok := StateForIntent(
		IntentAnchor{ScopeID: "scope-a", GenerationID: "gen-1", EntityKeys: []string{"repo:a"}},
		KeyspaceCloudResourceUID,
		PhaseCanonicalNodesCommitted,
		observedAt,
	)
	if !ok {
		t.Fatal("StateForIntent() ok = false, want true")
	}
	want := PhaseState{
		Key: PhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo:a",
			SourceRunID:      "gen-1",
			GenerationID:     "gen-1",
			Keyspace:         KeyspaceCloudResourceUID,
		},
		Phase:       PhaseCanonicalNodesCommitted,
		CommittedAt: observedAt,
		UpdatedAt:   observedAt,
	}
	if state != want {
		t.Fatalf("StateForIntent() = %+v, want %+v", state, want)
	}
}

func TestStateForIntentZeroObservedAtDefaultsToNow(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC()
	state, ok := StateForIntent(
		IntentAnchor{ScopeID: "scope-a", GenerationID: "gen-1"},
		KeyspaceServiceUID,
		PhaseDeploymentMapping,
		time.Time{},
	)
	after := time.Now().UTC()
	if !ok {
		t.Fatal("StateForIntent() ok = false, want true")
	}
	if state.CommittedAt.Before(before) || state.CommittedAt.After(after) {
		t.Fatalf("CommittedAt = %v, want between %v and %v", state.CommittedAt, before, after)
	}
}

func TestStateForIntentRejectsBlankScopeOrGeneration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		anchor IntentAnchor
	}{
		{name: "blank scope", anchor: IntentAnchor{GenerationID: "gen-1"}},
		{name: "blank generation", anchor: IntentAnchor{ScopeID: "scope-a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := StateForIntent(tt.anchor, KeyspaceServiceUID, PhaseDeploymentMapping, time.Now()); ok {
				t.Fatal("StateForIntent() ok = true, want false")
			}
		})
	}
}

// TestStateForIntentValueMatchesStateForIntent pins the delegation this
// hoist relies on (issue #6061): StateForIntentValue must build the exact
// same state as calling StateForIntent with the equivalent anchor, so the
// two never drift onto different key derivations for the same intent.
func TestStateForIntentValueMatchesStateForIntent(t *testing.T) {
	t.Parallel()

	observedAt := time.Unix(1700000000, 0).UTC()
	intent := reducercontract.Intent{
		ScopeID:      "scope-a",
		GenerationID: "gen-1",
		EntityKeys:   []string{"repo:a"},
	}

	got, gotOK := StateForIntentValue(intent, KeyspaceCloudResourceUID, PhaseCanonicalNodesCommitted, observedAt)
	want, wantOK := StateForIntent(
		IntentAnchor{ScopeID: intent.ScopeID, GenerationID: intent.GenerationID, EntityKeys: intent.EntityKeys},
		KeyspaceCloudResourceUID,
		PhaseCanonicalNodesCommitted,
		observedAt,
	)
	if gotOK != wantOK {
		t.Fatalf("ok = %v, want %v", gotOK, wantOK)
	}
	if got != want {
		t.Fatalf("StateForIntentValue() = %+v, want %+v", got, want)
	}
}

func TestStateForIntentValueRejectsBlankScopeOrGeneration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		intent reducercontract.Intent
	}{
		{name: "blank scope", intent: reducercontract.Intent{GenerationID: "gen-1"}},
		{name: "blank generation", intent: reducercontract.Intent{ScopeID: "scope-a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := StateForIntentValue(tt.intent, KeyspaceServiceUID, PhaseDeploymentMapping, time.Now()); ok {
				t.Fatal("StateForIntentValue() ok = true, want false")
			}
		})
	}
}
