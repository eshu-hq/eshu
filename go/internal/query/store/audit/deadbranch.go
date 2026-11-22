// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"errors"
	"sort"
	"strings"
)

// span is a half-open byte range of a scrubbed SQL text.
type span struct{ start, end int }

type truth int

const (
	truthUnknown truth = iota
	truthTrue
	truthFalse
)

// errUnbalanced reports parentheses that do not pair, which the dead-branch
// pass cannot evaluate; the classifier fails closed on it.
var errUnbalanced = errors.New("unbalanced parentheses")

// deadSpans returns the byte ranges of a masked statement that a boolean
// constant makes unreachable, so the tables and kinds they name are not counted
// as reads. It reads boolean groups only, with the SQL precedence AND over OR:
//
//   - an OR arm is dead when it has a top-level conjunct that is FALSE
//     (including NOT TRUE and a group that is itself always false);
//   - when some OR arm is always true (every conjunct is TRUE, NOT FALSE, or an
//     always-true group), every other arm is dead;
//   - a group that mixes both is evaluated arm by arm, so "(X OR TRUE AND FALSE)"
//     keeps X and "a AND (NOT FALSE) OR (live)" keeps the live arm.
//
// A parenthesized group is evaluated only in a boolean context (after AND, OR,
// NOT, WHERE, ON, WHEN, THEN, ELSE, HAVING, an operator, or at the start of
// another group) and only when it holds no SELECT, FROM, or WITH at its top
// level, so a function call, an IN list, or a subquery is never read as a
// boolean expression.
func deadSpans(masked string) ([]span, error) {
	pairs, err := parenPairs(masked)
	if err != nil {
		return nil, err
	}
	var dead []span
	var covered []span
	for _, pair := range pairs {
		if insideAny(covered, pair.start) {
			continue
		}
		if !booleanContext(masked, pair.start) || hasTopLevelClause(masked, pair.start+1, pair.end-1) {
			continue
		}
		(&evaluator{text: masked, dead: &dead}).expr(pair.start+1, pair.end-1)
		covered = append(covered, pair)
	}
	return dead, nil
}

// blankSpans replaces each span of text with spaces, keeping newlines and length.
func blankSpans(text string, spans []span) string {
	if len(spans) == 0 {
		return text
	}
	out := []byte(text)
	for _, s := range spans {
		for i := s.start; i < s.end && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

func parenPairs(text string) ([]span, error) {
	var stack []int
	var pairs []span
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(':
			stack = append(stack, i)
		case ')':
			if len(stack) == 0 {
				return nil, errUnbalanced
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			pairs = append(pairs, span{start: open, end: i + 1})
		}
	}
	if len(stack) != 0 {
		return nil, errUnbalanced
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].start < pairs[j].start })
	return pairs, nil
}

func insideAny(spans []span, at int) bool {
	for _, s := range spans {
		if at >= s.start && at < s.end {
			return true
		}
	}
	return false
}

var booleanKeywords = map[string]struct{}{
	"and": {}, "or": {}, "not": {}, "where": {}, "on": {}, "when": {}, "then": {}, "else": {}, "having": {},
}

// booleanContext reports whether the "(" at open starts a boolean group rather
// than a function call, an IN list, or a subquery.
func booleanContext(text string, open int) bool {
	i := open - 1
	for i >= 0 && isSpace(text[i]) {
		i--
	}
	if i < 0 || !isWordByte(text[i]) {
		return true
	}
	end := i + 1
	for i >= 0 && isWordByte(text[i]) {
		i--
	}
	_, ok := booleanKeywords[strings.ToLower(text[i+1:end])]
	return ok
}

// hasTopLevelClause reports a SELECT, FROM, or WITH keyword at depth zero of
// text[lo:hi], which marks a subquery rather than a boolean expression.
func hasTopLevelClause(text string, lo, hi int) bool {
	depth := 0
	for i := lo; i < hi; i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && isWordStart(text, i) {
				word := wordAt(text, i, hi)
				switch strings.ToLower(word) {
				case "select", "from", "with":
					return true
				}
				i += len(word) - 1
			}
		}
	}
	return false
}

type evaluator struct {
	text string
	dead *[]span
}

// expr evaluates text[lo:hi] as an OR of AND-conjunctions and records the
// spans its constants make dead.
func (e *evaluator) expr(lo, hi int) truth {
	arms := e.split(lo, hi, "or")
	armTruth := make([]truth, len(arms))
	anyTrue, allFalse := -1, true
	for i, arm := range arms {
		armTruth[i] = e.arm(arm.start, arm.end)
		if armTruth[i] == truthTrue && anyTrue < 0 {
			anyTrue = i
		}
		if armTruth[i] != truthFalse {
			allFalse = false
		}
	}
	for i, arm := range arms {
		if armTruth[i] == truthFalse || (anyTrue >= 0 && i != anyTrue) {
			*e.dead = append(*e.dead, arm)
		}
	}
	switch {
	case anyTrue >= 0:
		return truthTrue
	case allFalse && len(arms) > 0:
		return truthFalse
	default:
		return truthUnknown
	}
}

func (e *evaluator) arm(lo, hi int) truth {
	conjuncts := e.split(lo, hi, "and")
	allTrue := len(conjuncts) > 0
	for _, conjunct := range conjuncts {
		switch e.conjunct(conjunct.start, conjunct.end) {
		case truthFalse:
			return truthFalse
		case truthTrue:
		default:
			allTrue = false
		}
	}
	if allTrue {
		return truthTrue
	}
	return truthUnknown
}

func (e *evaluator) conjunct(lo, hi int) truth {
	for lo < hi && isSpace(e.text[lo]) {
		lo++
	}
	for hi > lo && isSpace(e.text[hi-1]) {
		hi--
	}
	body := strings.ToLower(strings.Join(strings.Fields(e.text[lo:hi]), " "))
	switch body {
	case "false", "not true":
		return truthFalse
	case "true", "not false":
		return truthTrue
	}
	if hi-lo >= 2 && e.text[lo] == '(' && closeOf(e.text, lo) == hi-1 &&
		!hasTopLevelClause(e.text, lo+1, hi-1) {
		return e.expr(lo+1, hi-1)
	}
	return truthUnknown
}

// split cuts text[lo:hi] at the depth-zero keyword sep ("or" or "and").
func (e *evaluator) split(lo, hi int, sep string) []span {
	var parts []span
	start, depth := lo, 0
	for i := lo; i < hi; i++ {
		switch e.text[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth != 0 || !isWordStart(e.text, i) {
				continue
			}
			word := wordAt(e.text, i, hi)
			if strings.EqualFold(word, sep) {
				parts = append(parts, span{start, i})
				start = i + len(word)
			}
			i += len(word) - 1
		}
	}
	return append(parts, span{start, hi})
}

// closeOf returns the index of the ")" that pairs with the "(" at open.
func closeOf(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isWordStart(text string, i int) bool {
	return isWordByte(text[i]) && (i == 0 || !isWordByte(text[i-1]))
}

func wordAt(text string, start, limit int) string {
	end := start
	for end < limit && isWordByte(text[end]) {
		end++
	}
	return text[start:end]
}
