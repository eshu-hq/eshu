// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intent

import "github.com/eshu-hq/eshu/go/internal/facts"

// FactLookup is an immutable, order-preserving fact index shared by
// reducer-intent family builders. The concrete value avoids per-family
// interface allocations on the projector fan-out hot path.
type FactLookup struct {
	inputFacts      []facts.Envelope
	positionsByKind map[string][]int
}

// NewFactLookup indexes one immutable scope generation in two O(N) passes: a
// first pass counts facts per kind, and a second fills exactly-sized position
// slices. This avoids growth and copying for the skewed kind distribution real
// generations carry. The lookup borrows inputFacts, which projection keeps
// immutable for its lifetime, so it is safe to share read-only across every
// builder probe; callers must not mutate that slice while the lookup is in use.
func NewFactLookup(inputFacts []facts.Envelope) FactLookup {
	counts := make(map[string]int)
	for _, envelope := range inputFacts {
		counts[envelope.FactKind]++
	}

	positions := make(map[string][]int, len(counts))
	for kind, count := range counts {
		positions[kind] = make([]int, 0, count)
	}
	for index, envelope := range inputFacts {
		positions[envelope.FactKind] = append(positions[envelope.FactKind], index)
	}
	return FactLookup{inputFacts: inputFacts, positionsByKind: positions}
}

// FirstOfKind returns the earliest envelope of kind in original generation
// order.
func (l FactLookup) FirstOfKind(kind string) (facts.Envelope, bool) {
	positions := l.positionsByKind[kind]
	if len(positions) == 0 {
		return facts.Envelope{}, false
	}
	return l.inputFacts[positions[0]], true
}

// FirstOfKindMatching returns the earliest envelope of kind accepted by the
// predicate.
func (l FactLookup) FirstOfKindMatching(
	kind string,
	accept func(facts.Envelope) bool,
) (facts.Envelope, bool) {
	for _, position := range l.positionsByKind[kind] {
		candidate := l.inputFacts[position]
		if accept(candidate) {
			return candidate, true
		}
	}
	return facts.Envelope{}, false
}

// FirstAcrossKinds returns the earliest accepted envelope across kinds in
// original generation order. The order of kinds is not priority.
func (l FactLookup) FirstAcrossKinds(
	accept func(facts.Envelope) bool,
	kinds ...string,
) (facts.Envelope, bool) {
	lists := make([][]int, 0, len(kinds))
	for _, kind := range kinds {
		if positions := l.positionsByKind[kind]; len(positions) > 0 {
			lists = append(lists, positions)
		}
	}
	if len(lists) == 0 {
		return facts.Envelope{}, false
	}

	heads := make([]int, len(lists))
	for {
		bestPosition := -1
		bestList := -1
		for listIndex, list := range lists {
			if heads[listIndex] >= len(list) {
				continue
			}
			if position := list[heads[listIndex]]; bestPosition == -1 || position < bestPosition {
				bestPosition = position
				bestList = listIndex
			}
		}
		if bestPosition == -1 {
			return facts.Envelope{}, false
		}
		heads[bestList]++
		candidate := l.inputFacts[bestPosition]
		if accept(candidate) {
			return candidate, true
		}
	}
}

// FirstMatchingKindPredicate returns the earliest accepted envelope whose kind
// satisfies kindPredicate. It evaluates kindPredicate once per distinct kind
// in the generation, not once per fact, keeping open-registry lookup cost
// proportional to kind cardinality rather than fact count.
func (l FactLookup) FirstMatchingKindPredicate(
	kindPredicate func(string) bool,
	accept func(facts.Envelope) bool,
) (facts.Envelope, bool) {
	var kinds []string
	for kind := range l.positionsByKind {
		if kindPredicate(kind) {
			kinds = append(kinds, kind)
		}
	}
	return l.FirstAcrossKinds(accept, kinds...)
}
