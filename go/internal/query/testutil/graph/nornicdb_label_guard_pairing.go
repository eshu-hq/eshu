// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"regexp"
	"strings"
)

var (
	pairedLabelTestTerm = regexp.MustCompile(`^(\w+):([A-Z]\w*)$`)
	pairedInLabelsTerm  = regexp.MustCompile(`^'(\w+)'\s+IN\s+labels\(\s*(\w+)\s*\)$`)
	pairedOrSeparator   = regexp.MustCompile(`\s+OR\s+`)
	pairedAndSeparator  = regexp.MustCompile(`\s+AND\s+`)
	pairedFormatVerb    = regexp.MustCompile(`%[sd]`)
)

// labelTestsPairedWithInLabels reports whether every label test in a WHERE
// attached to a relationship MATCH is a defence-in-depth conjunct that sits
// beside the `'Label' IN labels(x)` form NornicDB v1.3.3 does evaluate there
// (#6786 X11, #7246).
//
// A positive label test in that position is ignored by NornicDB, not
// evaluated, and the rest of the WHERE still is (probes D03 and J05 in
// docs/internal/evidence/6786-nornicdb-label-predicates.md). So
// `(x:A OR x:B) AND ('A' IN labels(x) OR 'B' IN labels(x))` returns what the
// IN labels() terms alone return on NornicDB, while Neo4j runs the cheap label
// test before the labels() calls. The shape is accepted only when all of this
// holds, because each clause the guard cannot read is a way the label test
// could stop being a no-op:
//
//   - the WHERE has no top-level OR, so the conjunct cannot widen the filter;
//   - for each variable it tests, exactly one conjunct is a positive
//     `x:A OR x:B` disjunction and exactly one is an `'A' IN labels(x) OR ...`
//     disjunction, over the same label set, so Neo4j and NornicDB admit the
//     same rows;
//   - no other conjunct carries a label test.
//
// where must be the original clause text, string literals included. A Sprintf
// verb such as the trailing environment-clause `%s` is read as a space, the
// same way the production scan renders a non-literal operand; the rendered
// statement is what the per-builder unit tests check.
func labelTestsPairedWithInLabels(where string) bool {
	where = pairedFormatVerb.ReplaceAllString(where, " ")
	clauses, ok := splitTopLevelAnd(where)
	if !ok {
		return false
	}
	tests := map[string]map[string]struct{}{}
	guards := map[string]map[string]struct{}{}
	for _, clause := range clauses {
		clause = stripOuterParens(strings.TrimSpace(clause))
		if variable, labels, ok := parseDisjunction(clause, pairedLabelTestTerm, 1, 2); ok {
			if tests[variable] != nil {
				return false
			}
			tests[variable] = labels
			continue
		}
		if variable, labels, ok := parseDisjunction(clause, pairedInLabelsTerm, 2, 1); ok {
			if guards[variable] != nil {
				return false
			}
			guards[variable] = labels
			continue
		}
		if labelPredicateLabelTest.MatchString(blankRelationshipNodePatterns(blankLiteralsAndComments(clause))) {
			return false
		}
	}
	if len(tests) == 0 || len(tests) != len(guards) {
		return false
	}
	for variable, labels := range tests {
		guard, ok := guards[variable]
		if !ok || len(guard) != len(labels) {
			return false
		}
		for label := range labels {
			if _, ok := guard[label]; !ok {
				return false
			}
		}
	}
	return true
}

// parseDisjunction reads clause as `term OR term ...` where every term matches
// re and names the same variable. It returns the variable and the label set.
// varGroup and labelGroup are the capture groups of re holding each.
func parseDisjunction(clause string, re *regexp.Regexp, varGroup, labelGroup int) (string, map[string]struct{}, bool) {
	variable := ""
	labels := map[string]struct{}{}
	for _, term := range pairedOrSeparator.Split(clause, -1) {
		match := re.FindStringSubmatch(strings.TrimSpace(stripOuterParens(strings.TrimSpace(term))))
		if match == nil {
			return "", nil, false
		}
		if variable != "" && match[varGroup] != variable {
			return "", nil, false
		}
		variable = match[varGroup]
		labels[match[labelGroup]] = struct{}{}
	}
	return variable, labels, variable != ""
}

// splitTopLevelAnd splits where at AND separators that sit at parenthesis and
// bracket depth zero. It reports false when a depth-zero OR is present, since
// that changes how the conjuncts bind.
func splitTopLevelAnd(where string) ([]string, bool) {
	text := blankLiteralsAndComments(where)
	var clauses []string
	depth, from := 0, 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
		if depth != 0 {
			continue
		}
		if loc := pairedAndSeparator.FindStringIndex(text[i:]); loc != nil && loc[0] == 0 {
			clauses = append(clauses, where[from:i])
			from = i + loc[1]
			i = from - 1
			continue
		}
		if loc := pairedOrSeparator.FindStringIndex(text[i:]); loc != nil && loc[0] == 0 {
			return nil, false
		}
	}
	return append(clauses, where[from:]), depth == 0
}

// stripOuterParens removes parentheses that wrap the whole of s.
func stripOuterParens(s string) string {
	for strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		depth := 0
		wraps := true
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 && i < len(s)-1 {
				wraps = false
				break
			}
		}
		if !wraps {
			return s
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}
