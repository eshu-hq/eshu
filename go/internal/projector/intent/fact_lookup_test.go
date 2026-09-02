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

// TestFactLookupFirstMatchingKindPredicateEvaluatesPerDistinctKind proves the
// registry-lookup predicate is called once per DISTINCT kind present, not once
// per fact — the whole point of routing open-registry probes (the
// secrets_iam_trust_chain, service_catalog_correlation, and
// observability_coverage_correlation families) through this helper instead of
// a full envelope scan. Relocated from root's forwarder tests when the
// observability-coverage-correlation extraction removed the forwarder's last
// root caller.
func TestFactLookupFirstMatchingKindPredicateEvaluatesPerDistinctKind(t *testing.T) {
	t.Parallel()

	lookup := NewFactLookup([]facts.Envelope{
		{FactID: "f1", FactKind: "kind_a"},
		{FactID: "f2", FactKind: "kind_a"},
		{FactID: "f3", FactKind: "kind_a"},
		{FactID: "f4", FactKind: "kind_b"},
	})

	calls := map[string]int{}
	kindPredicate := func(kind string) bool {
		calls[kind]++
		return kind == "kind_a"
	}

	envelope, ok := lookup.FirstMatchingKindPredicate(kindPredicate, func(facts.Envelope) bool { return true })
	if !ok {
		t.Fatal("FirstMatchingKindPredicate ok = false, want true")
	}
	if envelope.FactID != "f1" {
		t.Fatalf("FirstMatchingKindPredicate FactID = %q, want f1", envelope.FactID)
	}
	if got, want := calls["kind_a"], 1; got != want {
		t.Fatalf("kindPredicate(kind_a) called %d times, want %d (once per distinct kind, not per fact)", got, want)
	}
	if got, want := calls["kind_b"], 1; got != want {
		t.Fatalf("kindPredicate(kind_b) called %d times, want %d", got, want)
	}
}

func TestFactLookupFirstMatchingKindPredicateNoMatch(t *testing.T) {
	t.Parallel()

	lookup := NewFactLookup([]facts.Envelope{{FactID: "f1", FactKind: "kind_a"}})
	_, ok := lookup.FirstMatchingKindPredicate(func(string) bool { return false }, func(facts.Envelope) bool { return true })
	if ok {
		t.Fatal("FirstMatchingKindPredicate with rejecting kindPredicate ok = true, want false")
	}
}
