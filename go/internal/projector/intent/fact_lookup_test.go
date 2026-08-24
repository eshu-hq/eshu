// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intent

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactLookupPreservesOriginalGenerationOrder(t *testing.T) {
	t.Parallel()

	lookup := NewFactLookup([]facts.Envelope{
		{FactID: "ignored", FactKind: "ignored"},
		{FactID: "second-kind-first", FactKind: "second"},
		{FactID: "first-kind-later", FactKind: "first"},
		{FactID: "second-kind-later", FactKind: "second"},
	})

	if got, ok := lookup.FirstOfKind("second"); !ok || got.FactID != "second-kind-first" {
		t.Fatalf("FirstOfKind() = %#v, %v; want second-kind-first, true", got, ok)
	}
	got, ok := lookup.FirstAcrossKinds(func(facts.Envelope) bool { return true }, "first", "second")
	if !ok || got.FactID != "second-kind-first" {
		t.Fatalf("FirstAcrossKinds() = %#v, %v; want second-kind-first, true", got, ok)
	}
	got, ok = lookup.FirstOfKindMatching("second", func(envelope facts.Envelope) bool {
		return envelope.FactID == "second-kind-later"
	})
	if !ok || got.FactID != "second-kind-later" {
		t.Fatalf("FirstOfKindMatching() = %#v, %v; want second-kind-later, true", got, ok)
	}
	got, ok = lookup.FirstMatchingKindPredicate(
		func(kind string) bool { return kind == "first" || kind == "second" },
		func(envelope facts.Envelope) bool { return envelope.FactID != "second-kind-first" },
	)
	if !ok || got.FactID != "first-kind-later" {
		t.Fatalf("FirstMatchingKindPredicate() = %#v, %v; want first-kind-later, true", got, ok)
	}
}

func TestFactLookupReturnsNoMatchForEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := NewFactLookup(nil)
	if got, ok := lookup.FirstOfKind("missing"); ok {
		t.Fatalf("FirstOfKind() = %#v, true; want no match", got)
	}
}
