// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import (
	"regexp/syntax"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// maxAlternatives bounds how many exact strings one node may stand for
	// before the analysis stops multiplying them and falls back to requiring
	// each part on its own (still sound, only less selective).
	maxAlternatives = 16
	// maxSequenceRunes bounds one literal. A longer run is cut into two
	// requirements; both stay necessary conditions.
	maxSequenceRunes = 128
	// maxClassRunes bounds how large a character class may be and still be
	// treated as a small set of exact alternatives.
	maxClassRunes = 4
)

// seq is one exact string a node can match, as per-position orbits.
type seq [][]rune

// node is the analysis result for one syntax node. A node is either exact:
// it matches precisely one of the strings in exact and nothing else, so
// neighbouring exact nodes can be joined into longer literals. Or it only
// carries need, a necessary condition on any text it matches.
type node struct {
	isExact bool
	exact   []seq
	need    *expr
}

func exactNode(alternatives ...seq) node { return node{isExact: true, exact: alternatives} }

func needNode(e *expr) node { return node{need: e} }

// unknownNode is a node that requires nothing.
func unknownNode() node { return needNode(trueExpr()) }

// derive returns the necessary condition of the regular expression source.
// It never returns nil; an expression with nothing to require yields the
// always-true condition.
func derive(source string) (*expr, error) {
	re, err := syntax.Parse(source, syntax.Perl)
	if err != nil {
		return nil, err
	}
	return toNeed(analyze(re.Simplify())).simplify(), nil
}

// toNeed turns any node into a necessary condition. An exact node that can
// match the empty string requires nothing.
func toNeed(n node) *expr {
	if !n.isExact {
		return n.need
	}
	return alternativesExpr(n.exact, false, false)
}

// alternativesExpr is the disjunction of the literals for alternatives, or
// the always-true condition when one of them is empty.
func alternativesExpr(alternatives []seq, before, after bool) *expr {
	subs := make([]*expr, 0, len(alternatives))
	for _, s := range alternatives {
		if len(s) == 0 {
			return trueExpr()
		}
		subs = append(subs, litExpr(literal{elems: s, before: before, after: after}))
	}
	return orExpr(subs...)
}

func analyze(re *syntax.Regexp) node {
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText, syntax.OpNoWordBoundary,
		syntax.OpWordBoundary:
		// Zero-width assertions only restrict where a match may sit. Treating
		// them as empty keeps the neighbours joinable and the result sound.
		return exactNode(seq{})
	case syntax.OpLiteral:
		return analyzeLiteral(re)
	case syntax.OpCharClass:
		return analyzeClass(re)
	case syntax.OpCapture:
		return analyze(re.Sub[0])
	case syntax.OpPlus:
		return needNode(toNeed(analyze(re.Sub[0])))
	case syntax.OpConcat:
		return analyzeConcat(re.Sub)
	case syntax.OpAlternate:
		return analyzeAlternate(re.Sub)
	default:
		// Star, quest, any-char and anything else: no required text.
		return unknownNode()
	}
}

func analyzeLiteral(re *syntax.Regexp) node {
	fold := re.Flags&syntax.FoldCase != 0
	s := make(seq, 0, len(re.Rune))
	for _, r := range re.Rune {
		if r == utf8.RuneError || !utf8.ValidRune(r) {
			// Invalid input bytes decode to U+FFFD, so a byte search for the
			// encoding would not be the same condition. Require nothing.
			return unknownNode()
		}
		s = append(s, orbit(r, fold))
	}
	return exactNode(s)
}

func analyzeClass(re *syntax.Regexp) node {
	total := 0
	for i := 0; i+1 < len(re.Rune); i += 2 {
		total += int(re.Rune[i+1]-re.Rune[i]) + 1
		if total > maxClassRunes {
			return unknownNode()
		}
	}
	if total == 0 {
		return unknownNode()
	}
	alternatives := make([]seq, 0, total)
	for i := 0; i+1 < len(re.Rune); i += 2 {
		for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
			if r == utf8.RuneError || !utf8.ValidRune(r) {
				return unknownNode()
			}
			alternatives = append(alternatives, seq{{r}})
		}
	}
	return exactNode(alternatives...)
}

// analyzeAlternate joins branches: exact when every branch is exact and the
// union stays small, otherwise the disjunction of what each branch needs.
func analyzeAlternate(subs []*syntax.Regexp) node {
	nodes := make([]node, len(subs))
	allExact := true
	total := 0
	for i, sub := range subs {
		nodes[i] = analyze(sub)
		if !nodes[i].isExact {
			allExact = false
		} else {
			total += len(nodes[i].exact)
		}
	}
	if allExact && total <= maxAlternatives {
		var union []seq
		for _, n := range nodes {
			union = append(union, n.exact...)
		}
		return exactNode(dedupeSeqs(union)...)
	}
	needs := make([]*expr, len(nodes))
	for i, n := range nodes {
		needs[i] = toNeed(n)
	}
	return needNode(orExpr(needs...))
}

// concatState accumulates the run of exact children currently being joined
// into one literal, plus the requirements already emitted.
type concatState struct {
	needs   []*expr
	run     []seq // exact alternatives of the pending run; {""} when idle
	before  bool  // a \b sits directly before the pending run
	touched bool  // a non-exact child or a boundary was seen
}

func (c *concatState) idle() bool {
	return len(c.run) == 1 && len(c.run[0]) == 0
}

// flush emits the pending run as a requirement and starts a new empty run.
func (c *concatState) flush(after bool) {
	if !c.idle() {
		c.needs = append(c.needs, alternativesExpr(c.run, c.before, after))
	}
	c.run = []seq{{}}
	c.before = false
}

func analyzeConcat(subs []*syntax.Regexp) node {
	c := &concatState{run: []seq{{}}}
	for _, sub := range subs {
		if sub.Op == syntax.OpWordBoundary {
			c.touched = true
			if !c.idle() {
				c.flush(true)
			}
			c.before = true
			continue
		}
		child := analyze(sub)
		if !child.isExact {
			c.touched = true
			c.flush(false)
			c.needs = append(c.needs, child.need)
			continue
		}
		next, ok := crossProduct(c.run, child.exact)
		if !ok {
			c.touched = true
			c.flush(false)
			c.run = child.exact
			continue
		}
		c.run = next
	}
	if !c.touched {
		return exactNode(c.run...)
	}
	c.flush(false)
	return needNode(andExpr(c.needs...))
}

// crossProduct appends every alternative of right to every alternative of
// left. It reports false when the result would exceed the size bounds.
func crossProduct(left, right []seq) ([]seq, bool) {
	if len(left)*len(right) > maxAlternatives {
		return nil, false
	}
	out := make([]seq, 0, len(left)*len(right))
	for _, l := range left {
		for _, r := range right {
			if len(l)+len(r) > maxSequenceRunes {
				return nil, false
			}
			joined := make(seq, 0, len(l)+len(r))
			joined = append(joined, l...)
			joined = append(joined, r...)
			out = append(out, joined)
		}
	}
	return dedupeSeqs(out), true
}

func dedupeSeqs(in []seq) []seq {
	seen := make(map[string]struct{}, len(in))
	out := make([]seq, 0, len(in))
	for _, s := range in {
		k := literal{elems: s}.key()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

// exprKind is the shape of one condition node.
type exprKind uint8

const (
	kindTrue exprKind = iota
	kindLit
	kindAnd
	kindOr
)

// expr is a boolean condition over literals. The always-true condition means
// the regular expression could match without any particular text.
type expr struct {
	kind exprKind
	lit  literal
	subs []*expr
	idx  int // literal table index, set when a Set is built
}

func trueExpr() *expr { return &expr{kind: kindTrue} }

func litExpr(l literal) *expr { return &expr{kind: kindLit, lit: l} }

// andExpr returns the conjunction of subs, dropping always-true members.
func andExpr(subs ...*expr) *expr {
	flat := make([]*expr, 0, len(subs))
	seen := map[string]struct{}{}
	for _, s := range subs {
		switch s.kind {
		case kindTrue:
		case kindAnd:
			for _, inner := range s.subs {
				flat = appendDistinct(flat, seen, inner)
			}
		default:
			flat = appendDistinct(flat, seen, s)
		}
	}
	switch len(flat) {
	case 0:
		return trueExpr()
	case 1:
		return flat[0]
	}
	return &expr{kind: kindAnd, subs: flat}
}

// orExpr returns the disjunction of subs; one always-true member makes the
// whole condition always true.
func orExpr(subs ...*expr) *expr {
	flat := make([]*expr, 0, len(subs))
	seen := map[string]struct{}{}
	for _, s := range subs {
		switch s.kind {
		case kindTrue:
			return trueExpr()
		case kindOr:
			for _, inner := range s.subs {
				flat = appendDistinct(flat, seen, inner)
			}
		default:
			flat = appendDistinct(flat, seen, s)
		}
	}
	switch len(flat) {
	case 0:
		return trueExpr()
	case 1:
		return flat[0]
	}
	return &expr{kind: kindOr, subs: flat}
}

func appendDistinct(list []*expr, seen map[string]struct{}, e *expr) []*expr {
	k := e.String()
	if _, ok := seen[k]; ok {
		return list
	}
	seen[k] = struct{}{}
	return append(list, e)
}

// simplify orders conjunction members so longer, rarer literals run first.
// Ordering never changes the truth value; it only lets evaluation stop early.
func (e *expr) simplify() *expr {
	for _, s := range e.subs {
		s.simplify()
	}
	if e.kind == kindAnd {
		sort.SliceStable(e.subs, func(i, j int) bool {
			return e.subs[i].weight() > e.subs[j].weight()
		})
	}
	return e
}

// weight estimates how selective a condition is: the shortest literal it
// could be satisfied by.
func (e *expr) weight() int {
	switch e.kind {
	case kindLit:
		return len(e.lit.elems)
	case kindAnd:
		w := 0
		for _, s := range e.subs {
			if sw := s.weight(); sw > w {
				w = sw
			}
		}
		return w
	case kindOr:
		w := -1
		for _, s := range e.subs {
			if sw := s.weight(); w < 0 || sw < w {
				w = sw
			}
		}
		return w
	case kindTrue:
		return 0
	}
	return 0
}

// String renders the condition for diagnostics and pinned expectations.
func (e *expr) String() string {
	switch e.kind {
	case kindLit:
		return e.lit.String()
	case kindAnd, kindOr:
		sep := " AND "
		if e.kind == kindOr {
			sep = " OR "
		}
		parts := make([]string, len(e.subs))
		for i, s := range e.subs {
			parts[i] = s.String()
		}
		return "(" + strings.Join(parts, sep) + ")"
	case kindTrue:
		return "TRUE"
	}
	return "TRUE"
}
