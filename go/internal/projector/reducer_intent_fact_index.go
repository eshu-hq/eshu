// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "github.com/eshu-hq/eshu/go/internal/facts"

// reducerIntentFactIndex is a shared, read-only, order-preserving index over
// one scope generation's inputFacts, built once per generation and passed to
// every build*ReducerIntent probe appendScopeGenerationReducerIntents calls
// (issue #4875). Before the index, the then-38 probes independently re-scanned
// the full inputFacts slice, so a generation with N facts paid O(38*N)
// comparisons. The current 40 probes all use the index: it groups fact
// positions by FactKind in one O(N) pass, then each probe looks up only the
// positions for the kind(s) it cares about.
//
// inputFacts is immutable once a scope generation is claimed for projection
// (buildProjection never mutates it — see TestBuildProjectionDoesNotMutateInputFactPayloads),
// so building this index once and sharing it read-only across all 40 probes
// is concurrency-safe: there is no writer to race against, and the index
// itself is never mutated after newReducerIntentFactIndex returns.
//
// The index stores original positions rather than copied envelopes so probes
// that need first-match-across-several-kinds semantics (firstAcrossKinds,
// firstMatchingKindPredicate) can recover the exact original relative order
// among facts of different kinds — required because several probes choose
// their anchor fact (FactID, SourceSystem, and sometimes Reason) based on
// whichever accepted-kind fact appears earliest in inputFacts, not on a
// per-kind order.
type reducerIntentFactIndex struct {
	inputFacts      []facts.Envelope
	positionsByKind map[string][]int
}

// newReducerIntentFactIndex builds a reducerIntentFactIndex over inputFacts
// in two O(N) passes: the first counts facts per kind, the second fills
// exactly-sized position slices. Two passes cost the same asymptotically as
// one but avoid the growth-and-copy overhead of repeated append calls into
// initially-empty per-kind slices, which matters here because a real
// generation's fact-kind distribution is skewed (a handful of kinds can each
// carry thousands of facts). The returned index borrows inputFacts (no
// copy); callers must not mutate inputFacts while the index is in use.
func newReducerIntentFactIndex(inputFacts []facts.Envelope) *reducerIntentFactIndex {
	counts := make(map[string]int)
	for _, envelope := range inputFacts {
		counts[envelope.FactKind]++
	}

	positions := make(map[string][]int, len(counts))
	for kind, count := range counts {
		positions[kind] = make([]int, 0, count)
	}
	for i, envelope := range inputFacts {
		positions[envelope.FactKind] = append(positions[envelope.FactKind], i)
	}
	return &reducerIntentFactIndex{inputFacts: inputFacts, positionsByKind: positions}
}

// firstOfKind returns the earliest envelope of the given FactKind in
// original inputFacts order, or ok=false when the generation has none. It
// replaces the common `for _, envelope := range envelopes { if
// envelope.FactKind != kind { continue }; return envelope, true }` loop with
// an O(1) lookup into the pre-grouped index.
func (idx *reducerIntentFactIndex) firstOfKind(kind string) (facts.Envelope, bool) {
	positions := idx.positionsByKind[kind]
	if len(positions) == 0 {
		return facts.Envelope{}, false
	}
	return idx.inputFacts[positions[0]], true
}

// firstOfKindMatching returns the earliest envelope of the given FactKind,
// in original order, for which accept returns true. It replaces a
// single-kind loop that applies an additional payload-derived predicate
// (e.g. "has a non-blank instance_profile_arn") after the FactKind check —
// the scan is bounded to that kind's facts instead of the full generation.
func (idx *reducerIntentFactIndex) firstOfKindMatching(kind string, accept func(facts.Envelope) bool) (facts.Envelope, bool) {
	for _, pos := range idx.positionsByKind[kind] {
		candidate := idx.inputFacts[pos]
		if accept(candidate) {
			return candidate, true
		}
	}
	return facts.Envelope{}, false
}

// firstAcrossKinds returns the earliest envelope, in original inputFacts
// order, among the given kinds for which accept returns true. It reproduces
// the exact selection a `for _, envelope := range inputFacts { if
// !isOneOfKinds(envelope.FactKind) { continue }; if !accept(envelope) {
// continue }; return envelope, true }` loop would make, but only visits
// facts whose kind is one of the requested kinds — it merges each kind's
// already-ordered position list by a small k-way walk instead of re-scanning
// every fact in the generation.
func (idx *reducerIntentFactIndex) firstAcrossKinds(accept func(facts.Envelope) bool, kinds ...string) (facts.Envelope, bool) {
	lists := make([][]int, 0, len(kinds))
	for _, kind := range kinds {
		if positions := idx.positionsByKind[kind]; len(positions) > 0 {
			lists = append(lists, positions)
		}
	}
	if len(lists) == 0 {
		return facts.Envelope{}, false
	}
	heads := make([]int, len(lists))
	for {
		bestPos := -1
		bestList := -1
		for li, list := range lists {
			if heads[li] >= len(list) {
				continue
			}
			if pos := list[heads[li]]; bestPos == -1 || pos < bestPos {
				bestPos = pos
				bestList = li
			}
		}
		if bestPos == -1 {
			return facts.Envelope{}, false
		}
		heads[bestList]++
		candidate := idx.inputFacts[bestPos]
		if accept(candidate) {
			return candidate, true
		}
	}
}

// firstMatchingKindPredicate returns the earliest envelope, in original
// order, whose FactKind satisfies kindPredicate AND whose envelope satisfies
// accept. It is for probes whose trigger kind set is not a small closed
// literal list but an open registry lookup (e.g. facts.SecretsIAMSchemaVersion,
// facts.ServiceCatalogSchemaVersion) — kindPredicate is evaluated once per
// DISTINCT kind present in the generation rather than once per fact, which
// keeps registry-lookup cost proportional to the generation's kind
// cardinality instead of its fact count.
func (idx *reducerIntentFactIndex) firstMatchingKindPredicate(
	kindPredicate func(string) bool,
	accept func(facts.Envelope) bool,
) (facts.Envelope, bool) {
	var kinds []string
	for kind := range idx.positionsByKind {
		if kindPredicate(kind) {
			kinds = append(kinds, kind)
		}
	}
	return idx.firstAcrossKinds(accept, kinds...)
}
