// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import "regexp"

// Set holds one derived prefilter for each regular expression it was built
// from. Literals shared between the expressions are scanned once per text.
// A Set is immutable and safe for concurrent use.
type Set struct {
	roots    []*expr
	matchers []*matcher
}

// NewSet derives a prefilter for each regular expression, in order. The
// source text of each expression (Regexp.String) is the only input, so the
// condition cannot drift from the pattern it guards. An expression with no
// derivable requirement is not gated: its prefilter always reports a possible
// match.
func NewSet(res ...*regexp.Regexp) *Set {
	roots := make([]*expr, len(res))
	for i, re := range res {
		root, err := derive(re.String())
		if err != nil {
			root = trueExpr()
		}
		roots[i] = root
	}
	return newSetFromRoots(roots)
}

// newSetFromRoots builds a Set over already derived conditions.
func newSetFromRoots(roots []*expr) *Set {
	s := &Set{roots: roots}
	index := map[string]int{}
	for _, root := range roots {
		s.assign(root, index)
	}
	return s
}

// assign gives every literal in e a slot in the shared matcher table.
func (s *Set) assign(e *expr, index map[string]int) {
	if e.kind == kindLit {
		k := e.lit.key()
		slot, ok := index[k]
		if !ok {
			slot = len(s.matchers)
			index[k] = slot
			s.matchers = append(s.matchers, newMatcher(e.lit))
		}
		e.idx = slot
		return
	}
	for _, sub := range e.subs {
		s.assign(sub, index)
	}
}

// Len returns the number of expressions in the set.
func (s *Set) Len() int { return len(s.roots) }

// Gated reports whether expression i has a derived requirement. When it is
// false the prefilter for i always reports a possible match.
func (s *Set) Gated(i int) bool { return s.roots[i].kind != kindTrue }

// Describe renders the derived requirement of expression i, for diagnostics
// and for pinning the derivation in tests. An ungated expression reads TRUE.
func (s *Set) Describe(i int) string { return s.roots[i].String() }

// Evaluate reports, for each expression, whether text may contain a match.
// A false value is a proof that the expression has no match in text; a true
// value only means the expression must still be run.
func (s *Set) Evaluate(text string) []bool {
	memo := make([]int8, len(s.matchers))
	out := make([]bool, len(s.roots))
	for i, root := range s.roots {
		out[i] = s.holds(root, text, memo)
	}
	return out
}

const (
	memoUnknown int8 = iota
	memoFound
	memoAbsent
)

func (s *Set) holds(e *expr, text string, memo []int8) bool {
	switch e.kind {
	case kindLit:
		if memo[e.idx] == memoUnknown {
			if s.matchers[e.idx].found(text) {
				memo[e.idx] = memoFound
			} else {
				memo[e.idx] = memoAbsent
			}
		}
		return memo[e.idx] == memoFound
	case kindAnd:
		for _, sub := range e.subs {
			if !s.holds(sub, text, memo) {
				return false
			}
		}
		return true
	case kindOr:
		for _, sub := range e.subs {
			if s.holds(sub, text, memo) {
				return true
			}
		}
		return false
	case kindTrue:
		return true
	}
	return true
}
