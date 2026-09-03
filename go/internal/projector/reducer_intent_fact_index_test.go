// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestReducerIntentFactIndexFirstOfKind(t *testing.T) {
	t.Parallel()

	inputFacts := []facts.Envelope{
		{FactID: "f1", FactKind: "kind_a"},
		{FactID: "f2", FactKind: "kind_b"},
		{FactID: "f3", FactKind: "kind_a"},
	}
	idx := newReducerIntentFactIndex(inputFacts)

	envelope, ok := idx.firstOfKind("kind_a")
	if !ok {
		t.Fatal("firstOfKind(kind_a) ok = false, want true")
	}
	if envelope.FactID != "f1" {
		t.Fatalf("firstOfKind(kind_a) FactID = %q, want f1 (earliest in original order)", envelope.FactID)
	}

	if _, ok := idx.firstOfKind("kind_missing"); ok {
		t.Fatal("firstOfKind(kind_missing) ok = true, want false")
	}
}

func TestReducerIntentFactIndexFirstOfKindEmpty(t *testing.T) {
	t.Parallel()

	idx := newReducerIntentFactIndex(nil)
	if _, ok := idx.firstOfKind("kind_a"); ok {
		t.Fatal("firstOfKind on empty index ok = true, want false")
	}
}

func TestReducerIntentFactIndexFirstOfKindMatchingSkipsRejected(t *testing.T) {
	t.Parallel()

	inputFacts := []facts.Envelope{
		{FactID: "f1", FactKind: "kind_a", Payload: map[string]any{"ok": false}},
		{FactID: "f2", FactKind: "kind_b", Payload: map[string]any{"ok": true}},
		{FactID: "f3", FactKind: "kind_a", Payload: map[string]any{"ok": true}},
	}
	idx := newReducerIntentFactIndex(inputFacts)

	accept := func(e facts.Envelope) bool {
		ok, _ := e.Payload["ok"].(bool)
		return ok
	}

	envelope, ok := idx.firstOfKindMatching("kind_a", accept)
	if !ok {
		t.Fatal("firstOfKindMatching(kind_a) ok = false, want true")
	}
	if envelope.FactID != "f3" {
		t.Fatalf("firstOfKindMatching(kind_a) FactID = %q, want f3 (first kind_a fact accepted)", envelope.FactID)
	}

	if _, ok := idx.firstOfKindMatching("kind_missing", accept); ok {
		t.Fatal("firstOfKindMatching(kind_missing) ok = true, want false")
	}
}

// TestReducerIntentFactIndexFirstAcrossKindsPreservesOriginalOrder is the
// correctness-critical case for the merge helper: several reducer-intent builder
// probes (e.g. supplychainimpact.BuildSupplyChainImpactReducerIntent,
// containerimageidentity.BuildContainerImageIdentityReducerIntent) choose
// their anchor fact as
// "whichever accepted-kind fact appears earliest in inputFacts", not
// "earliest fact of the first-listed kind". A naive per-kind-priority lookup
// would pick kind_b's fact here even though a kind_a fact appears earlier in
// the original slice.
func TestReducerIntentFactIndexFirstAcrossKindsPreservesOriginalOrder(t *testing.T) {
	t.Parallel()

	inputFacts := []facts.Envelope{
		{FactID: "f0-other", FactKind: "kind_other"},
		{FactID: "f1-b", FactKind: "kind_b"},
		{FactID: "f2-a", FactKind: "kind_a"},
		{FactID: "f3-b", FactKind: "kind_b"},
	}
	idx := newReducerIntentFactIndex(inputFacts)

	accept := func(facts.Envelope) bool { return true }

	// kind_a is passed first in the call, but kind_b's fact (f1-b) is
	// earlier in the original slice, so it must win.
	envelope, ok := idx.firstAcrossKinds(accept, "kind_a", "kind_b")
	if !ok {
		t.Fatal("firstAcrossKinds(kind_a, kind_b) ok = false, want true")
	}
	if envelope.FactID != "f1-b" {
		t.Fatalf("firstAcrossKinds(kind_a, kind_b) FactID = %q, want f1-b (earliest across both kinds)", envelope.FactID)
	}
}

func TestReducerIntentFactIndexFirstAcrossKindsSkipsRejectedRegardlessOfKind(t *testing.T) {
	t.Parallel()

	inputFacts := []facts.Envelope{
		{FactID: "f1-a-reject", FactKind: "kind_a", Payload: map[string]any{"ok": false}},
		{FactID: "f2-b-reject", FactKind: "kind_b", Payload: map[string]any{"ok": false}},
		{FactID: "f3-a-accept", FactKind: "kind_a", Payload: map[string]any{"ok": true}},
	}
	idx := newReducerIntentFactIndex(inputFacts)

	accept := func(e facts.Envelope) bool {
		ok, _ := e.Payload["ok"].(bool)
		return ok
	}

	envelope, ok := idx.firstAcrossKinds(accept, "kind_a", "kind_b")
	if !ok {
		t.Fatal("firstAcrossKinds(kind_a, kind_b) ok = false, want true")
	}
	if envelope.FactID != "f3-a-accept" {
		t.Fatalf("firstAcrossKinds(kind_a, kind_b) FactID = %q, want f3-a-accept", envelope.FactID)
	}
}

func TestReducerIntentFactIndexFirstAcrossKindsNoMatch(t *testing.T) {
	t.Parallel()

	idx := newReducerIntentFactIndex([]facts.Envelope{{FactID: "f1", FactKind: "kind_other"}})
	if _, ok := idx.firstAcrossKinds(func(facts.Envelope) bool { return true }, "kind_a", "kind_b"); ok {
		t.Fatal("firstAcrossKinds with no matching kinds ok = true, want false")
	}
}
