// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"regexp"
	"strings"
)

// nornicdbBrokenAndOrPattern matches an AND or OR keyword whose immediately
// preceding character is a newline or a tab rather than a space -- the exact
// shape proven live to break NornicDB v1.3.3 (#6786,
// docs/internal/evidence/6786-scoped-grant-nornicdb-read-predicates.md): on
// that pinned image, `WHERE x = $a\n\tAND y = $b` (or `\nAND`, with no space
// immediately before AND/OR) can silently drop the WHOLE WHERE clause, not
// just the AND/OR term itself. `\n  AND` and `\n\t AND` (a space directly
// before AND/OR, however the line got there) are unaffected.
var nornicdbBrokenAndOrPattern = regexp.MustCompile(`[\n\t](AND|OR)\b`)

// cypherAssertionT is the minimal test-reporting contract
// AssertCypherHasNoBrokenAndOr needs: `Helper` plus `Fatalf`. `*testing.T`
// satisfies it, so every production call site is unaffected; the sibling
// nornicdb_and_or_whitespace_test.go's recordingT double also satisfies it,
// which is what lets that file's seeded-violation test observe a RED case
// fail without failing the test binary itself.
//
// The standard library's testing.TB carries an unexported method
// specifically to block implementations outside package testing, so a
// double for it cannot exist at all; this narrower interface is the
// alternative that keeps the guard genuinely testable.
type cypherAssertionT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// AssertCypherHasNoBrokenAndOr fails t if cypher contains an AND/OR keyword
// immediately preceded by a newline or tab instead of a space. Call it with
// the exact production-rendered Cypher text a test's graph-reader double
// captured, on every scoped-grant call site: a caller that reintroduces a
// multi-line `AND (...)`/`OR (...)` group without a leading space is
// reintroducing the #6786 NornicDB v1.3.3 defect, and this assertion catches
// it at the Go-string level, before a live backend has to.
func AssertCypherHasNoBrokenAndOr(t cypherAssertionT, cypher string) {
	t.Helper()
	if loc := nornicdbBrokenAndOrPattern.FindStringIndex(cypher); loc != nil {
		t.Fatalf(
			"cypher has %q directly after a newline/tab with no leading space -- NornicDB v1.3.3 can silently drop the WHOLE WHERE clause this sits in (#6786):\n%s",
			cypher[loc[0]:loc[1]], cypher,
		)
	}
}

// The X11 shapes (#6786). On NornicDB v1.3.3 a label predicate's clause
// position decides whether it is evaluated, and the two usable forms are
// mirror images:
//
//   - In a WHERE attached to a MATCH whose pattern has a relationship, a label
//     test (`t:Repository`, `t:A OR t:B`, `NOT t:A`) is ignored or drops every
//     row, while `'Repository' IN labels(t)` is evaluated correctly.
//   - In a WHERE attached to a WITH, a positive label test is evaluated
//     correctly, while `IN labels(x)` and `NOT x:L` are ignored.
//   - A quantifier (`any|all|none|single`) or list comprehension that reads
//     labels(x) is wrong in every position: ignored after a pattern, zero rows
//     on a single-node MATCH.
//
// Evidence: docs/internal/evidence/6786-nornicdb-label-predicates.md.
var (
	labelPredicateClauseKeyword = regexp.MustCompile(`\b(OPTIONAL\s+MATCH|MATCH|WITH|WHERE|RETURN|UNWIND|CALL|UNION|ORDER\s+BY|SKIP|LIMIT|CREATE|MERGE|SET|DETACH\s+DELETE|DELETE|REMOVE|FOREACH)\b`)
	labelPredicateRelationship  = regexp.MustCompile(`-\[|\]-|--|->|<-`)
	labelPredicateLabelTest     = regexp.MustCompile(`\b[A-Za-z_]\w*:[A-Z]\w*`)
	labelPredicateNegatedLabel  = regexp.MustCompile(`\bNOT\s+\(?\s*[A-Za-z_]\w*:[A-Z]\w*`)
	labelPredicateInLabels      = regexp.MustCompile(`\bIN\s+labels\s*\(`)
	labelPredicateQuantifier    = regexp.MustCompile(`(?i)\b(any|all|none|single)\s*\(\s*\w+\s+IN\b`)
	labelPredicateComprehension = regexp.MustCompile(`\[\s*\w+\s+IN\s+labels\s*\(`)
	labelPredicateLabelsCall    = regexp.MustCompile(`\blabels\s*\(`)
)

// AssertCypherHasNoIgnoredLabelPredicate fails t when cypher filters on a node
// label in a clause position NornicDB v1.3.3 does not evaluate (the #6786 X11
// shapes). Use `'Label' IN labels(x)` after a relationship pattern and a
// positive `x:Label` after a WITH; never quantify over labels().
func AssertCypherHasNoIgnoredLabelPredicate(t cypherAssertionT, cypher string) {
	t.Helper()
	if problem := IgnoredLabelPredicate(cypher); problem != "" {
		t.Fatalf("cypher filters on a label in a position NornicDB v1.3.3 ignores (#6786 X11): %s\n%s", problem, cypher)
	}
}

// IgnoredLabelPredicate returns a description of the first X11 label
// predicate in cypher, or "" when there is none. The source-scan guard calls
// it directly so it can report every offending literal in one run.
func IgnoredLabelPredicate(cypher string) string {
	text := blankStringLiterals(cypher)
	frame := innermostBraceOpen(text)
	parenDepth := parenDepthWithinFrame(text)

	type clause struct {
		keyword    string
		start, end int
	}
	byFrame := map[int][]clause{}
	order := []int{}
	for _, loc := range labelPredicateClauseKeyword.FindAllStringIndex(text, -1) {
		if parenDepth[loc[0]] != 0 || precededByStartsOrEnds(text, loc[0]) {
			continue
		}
		id := frame[loc[0]]
		if _, seen := byFrame[id]; !seen {
			order = append(order, id)
		}
		keyword := strings.Join(strings.Fields(text[loc[0]:loc[1]]), " ")
		byFrame[id] = append(byFrame[id], clause{keyword: keyword, start: loc[0], end: loc[1]})
	}

	for _, id := range order {
		clauses := byFrame[id]
		governing, governingBody := "", ""
		for i, c := range clauses {
			bodyEnd := frameEnd(frame, id, c.end)
			if i+1 < len(clauses) && clauses[i+1].start < bodyEnd {
				bodyEnd = clauses[i+1].start
			}
			body := sameFrameText(text, frame, id, c.end, bodyEnd)
			if c.keyword != "WHERE" {
				governing, governingBody = c.keyword, body
				continue
			}
			if problem := labelPredicateProblem(governing, governingBody, body); problem != "" {
				return problem + ": " + strings.TrimSpace(cypher[c.start:bodyEnd])
			}
		}
	}
	return ""
}

func labelPredicateProblem(governing, governingBody, where string) string {
	if quantifiesOverLabels(where) {
		return "quantifier or list comprehension over labels() is never evaluated correctly"
	}
	switch governing {
	case "MATCH", "OPTIONAL MATCH":
		if !labelPredicateRelationship.MatchString(governingBody) {
			return ""
		}
		if labelPredicateLabelTest.MatchString(blankRelationshipNodePatterns(where)) {
			return "label test in a WHERE attached to a relationship MATCH is ignored; use 'Label' IN labels(x)"
		}
	case "WITH":
		if labelPredicateInLabels.MatchString(where) {
			return "IN labels() in a WHERE attached to WITH is ignored; use a positive x:Label test"
		}
		if labelPredicateNegatedLabel.MatchString(where) {
			return "NOT x:Label in a WHERE attached to WITH is ignored"
		}
	}
	return ""
}

// quantifiesOverLabels reports whether where holds a list comprehension over
// labels(x), or an any/all/none/single quantifier whose parenthesised body
// calls labels(x) (either as the iterated list or inside its predicate).
func quantifiesOverLabels(where string) bool {
	if labelPredicateComprehension.MatchString(where) {
		return true
	}
	for _, loc := range labelPredicateQuantifier.FindAllStringIndex(where, -1) {
		open := strings.IndexByte(where[loc[0]:loc[1]], '(') + loc[0]
		depth, end := 0, len(where)
		for i := open; i < len(where); i++ {
			if where[i] == '(' {
				depth++
			} else if where[i] == ')' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if labelPredicateLabelsCall.MatchString(where[open:end]) {
			return true
		}
	}
	return false
}

// blankStringLiterals replaces the contents of quoted string literals with
// spaces so a label-like or keyword-like token inside a literal is not read
// as Cypher. Offsets are preserved.
func blankStringLiterals(cypher string) string {
	out := []byte(cypher)
	var quote byte
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote != 0 && c == '\\':
			out[i] = ' '
			if i+1 < len(out) {
				i++
				out[i] = ' '
			}
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			out[i] = ' '
		}
	}
	return string(out)
}

// innermostBraceOpen maps every offset to the offset of its innermost
// enclosing '{' (-1 at top level). A CALL or EXISTS subquery body is its own
// clause frame.
func innermostBraceOpen(text string) []int {
	frame := make([]int, len(text))
	stack := []int{-1}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '{':
			frame[i] = stack[len(stack)-1]
			stack = append(stack, i)
			continue
		case '}':
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
		frame[i] = stack[len(stack)-1]
	}
	return frame
}

// parenDepthWithinFrame maps every offset to its '(' / '[' nesting depth,
// reset at each '{' so a subquery's clauses sit at depth zero.
func parenDepthWithinFrame(text string) []int {
	depth := make([]int, len(text))
	stack := []int{0}
	for i := 0; i < len(text); i++ {
		top := len(stack) - 1
		switch text[i] {
		case '(', '[':
			depth[i] = stack[top]
			stack[top]++
			continue
		case ')', ']':
			if stack[top] > 0 {
				stack[top]--
			}
		case '{':
			depth[i] = stack[top]
			stack = append(stack, 0)
			continue
		case '}':
			if len(stack) > 1 {
				stack = stack[:top]
			}
		}
		depth[i] = stack[len(stack)-1]
	}
	return depth
}

func precededByStartsOrEnds(text string, at int) bool {
	prefix := strings.TrimRight(text[:at], " \t\r\n")
	return strings.HasSuffix(prefix, "STARTS") || strings.HasSuffix(prefix, "ENDS")
}

// frameEnd returns the offset where frame id closes at or after from: the
// first offset that is neither in frame id nor inside a brace nested in it.
func frameEnd(frame []int, id, from int) int {
	for i := from; i < len(frame); i++ {
		if !nestedIn(frame, i, id) {
			return i
		}
	}
	return len(frame)
}

// nestedIn reports whether offset i sits in frame id or in a brace opened
// inside it.
func nestedIn(frame []int, i, id int) bool {
	open := frame[i]
	for {
		if open == id {
			return true
		}
		if open < 0 {
			return false
		}
		open = frame[open]
	}
}

// sameFrameText returns text[from:to] with every character of a nested frame
// blanked, so a subquery's own clauses do not leak into the enclosing WHERE.
func sameFrameText(text string, frame []int, id, from, to int) string {
	out := []byte(text[from:to])
	for i := range out {
		if frame[from+i] != id {
			out[i] = ' '
		}
	}
	return string(out)
}

// blankRelationshipNodePatterns blanks node patterns that sit next to a
// relationship arrow inside a WHERE (a pattern predicate), because a label
// there is part of the pattern, not a label test.
func blankRelationshipNodePatterns(where string) string {
	out := []byte(where)
	var opens []int
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '(':
			opens = append(opens, i)
		case ')':
			if len(opens) == 0 {
				continue
			}
			open := opens[len(opens)-1]
			opens = opens[:len(opens)-1]
			if nextNonSpace(where, i+1) == '-' || nextNonSpace(where, i+1) == '<' ||
				prevNonSpace(where, open-1) == '-' || prevNonSpace(where, open-1) == '>' {
				for j := open + 1; j < i; j++ {
					out[j] = ' '
				}
			}
		}
	}
	return string(out)
}

func nextNonSpace(s string, from int) byte {
	for i := from; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			return s[i]
		}
	}
	return 0
}

func prevNonSpace(s string, from int) byte {
	for i := from; i >= 0; i-- {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			return s[i]
		}
	}
	return 0
}
