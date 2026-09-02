// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
)

// reducerIntentFactIndex is the root compatibility wrapper around the neutral,
// immutable fact lookup shared with extracted intent-family packages. Root
// assembly constructs it once per generation and retains ownership of its
// lifetime, family order, queue writes, retries, and telemetry.
type reducerIntentFactIndex struct {
	lookup projectorintent.FactLookup
}

// newReducerIntentFactIndex builds one order-preserving lookup for the 44
// reducer-intent builder probes. The lookup borrows inputFacts, which projection
// keeps immutable for the lifetime of this index.
func newReducerIntentFactIndex(inputFacts []facts.Envelope) *reducerIntentFactIndex {
	return &reducerIntentFactIndex{lookup: projectorintent.NewFactLookup(inputFacts)}
}

func (idx *reducerIntentFactIndex) firstOfKind(kind string) (facts.Envelope, bool) {
	return idx.lookup.FirstOfKind(kind)
}

func (idx *reducerIntentFactIndex) firstOfKindMatching(
	kind string,
	accept func(facts.Envelope) bool,
) (facts.Envelope, bool) {
	return idx.lookup.FirstOfKindMatching(kind, accept)
}

func (idx *reducerIntentFactIndex) firstAcrossKinds(
	accept func(facts.Envelope) bool,
	kinds ...string,
) (facts.Envelope, bool) {
	return idx.lookup.FirstAcrossKinds(accept, kinds...)
}

// documentedReducerIntentProbeCount is the number of distinct reducer-intent
// builder probes appendScopeGenerationReducerIntents calls, cited in README.md.
// TestReducerIntentProbeCountMatchesDocumentedCount parses the dispatcher with
// go/ast and fails if this count or its documented prose drifts.
const documentedReducerIntentProbeCount = 44
