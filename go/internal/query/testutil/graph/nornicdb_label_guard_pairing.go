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

// pairedVarLengthRelationship matches a single variable-length relationship
// bracket such as `[*1..4]` or `[:R*1..%d]`.
var pairedVarLengthRelationship = regexp.MustCompile(`\[[^\[\]]*\*[^\[\]]*\]`)

// labelTestPairExempt reports whether a label test in the WHERE of a
// relationship MATCH is the one shape #7246 proved harmless: a defence-in-depth
// conjunct beside the `'Label' IN labels(x)` form NornicDB v1.3.3 evaluates.
// It is the exemption's position gate plus labelTestsPairedWithInLabels.
//
// The position is exactly the live-proven one, because the label test is only
// inert on NornicDB where it is ignored, and 6786-nornicdb-label-predicates.md
// shows it is ignored in some positions and evaluated in others:
//
//   - governing must be a plain MATCH. An OPTIONAL MATCH evaluates the label
//     test and nulls the row (row H05).
//   - the MATCH must open its frame (opensFrame): no MATCH, WITH or other
//     clause before it. A label test in the WHERE of a second MATCH returns
//     zero rows (row H02), and a negated one returns zero rows (row D04).
//   - the MATCH holds exactly one relationship, and it is variable-length, the
//     shape TestLiveChangeSurfaceLabelPredicate and
//     TestLiveChangeSurfaceLabelConjunctDeepTraversal run on NornicDB and Neo4j.
//
// Rows A02 through I01 measure a positive label test as ignored, or evaluated
// correctly, only in a relationship MATCH that opens its frame, which is why
// the rule above is not widened to any WHERE. The gate is stricter than those
// rows: the top-level frame, one relationship token and no second pattern.
func labelTestPairExempt(governing, governingBody, original string, opensFrame bool) bool {
	if governing != "MATCH" || !opensFrame {
		return false
	}
	if len(pairedVarLengthRelationship.FindAllString(governingBody, -1)) != 1 || strings.Count(governingBody, "[") != 1 {
		return false
	}
	// The bracket count misses a bracketless second relationship (`<--`, `--`)
	// and a comma pattern (`MATCH (b), (a)-[*..]->(x)`), neither of which has a
	// measured row, so each is outside the live-proven shape.
	if len(pairedRelationshipToken.FindAllString(governingBody, -1)) != 1 || hasTopLevelComma(governingBody) {
		return false
	}
	return labelTestsPairedWithInLabels(original)
}

// pairedRelationshipToken matches one relationship arrow with or without a
// bracketed detail: `-[*1..4]->`, `<-[:R]-`, `-->`, `--`.
var pairedRelationshipToken = regexp.MustCompile(`(?:<-|-)(?:\[[^\[\]]*\])?(?:->|-)`)

// hasTopLevelComma reports whether s has a comma outside every parenthesis,
// brace and bracket, which is how a MATCH lists a second pattern.
func hasTopLevelComma(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// labelTestsPairedWithInLabels reports whether the label tests in a WHERE are
// one `x:A OR x:B` defence-in-depth conjunct for a single variable, beside an
// `'A' IN labels(x) OR 'B' IN labels(x)` guard over the same label set (#7246).
// Position is decided by labelTestPairExempt, not here.
//
// On NornicDB v1.3.3 a positive label test in the WHERE of a single
// variable-length MATCH that opens its frame is ignored and the IN labels()
// terms are evaluated (probes D03 and J05 in
// docs/internal/evidence/6786-nornicdb-label-predicates.md, and the live tests
// named on labelTestPairExempt). That holds only for those rows: in the
// WHERE of a second MATCH (H02), of an OPTIONAL MATCH (H05) or with a negation
// (D04) the label test is evaluated and drops rows. So
// `(x:A OR x:B) AND ('A' IN labels(x) OR 'B' IN labels(x))` returns what the
// IN labels() terms alone return on NornicDB, while Neo4j runs the cheap label
// test before the labels() calls. The shape is accepted only when all of this
// holds, because each clause the guard cannot read is a way the label test
// could stop being a no-op:
//
//   - the WHERE has no top-level OR, so the conjunct cannot widen the filter;
//   - exactly one variable carries a label test: exactly one conjunct is a
//     positive `x:A OR x:B` disjunction and exactly one is an
//     `'A' IN labels(x) OR ...` disjunction, over the same label set, so Neo4j
//     and NornicDB admit the same rows;
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
	if len(tests) != 1 || len(guards) != 1 {
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
