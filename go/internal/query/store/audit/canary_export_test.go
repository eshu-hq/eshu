// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"regexp"
	"sort"
	"strings"
)

// The kind canary (#10880 PR-B0, the disposition of the per-relation attribution
// gap). The classifier attributes fact kinds per statement and recognizes a kind
// predicate only as =, IN, or = ANY($n). A fact_records alias whose own kind
// predicate takes any other form (a cast, a COLLATE, ||, position(), ANY(ARRAY),
// IN (SELECT ..), NOT (=), IS DISTINCT FROM, a row compare, a range) is read under
// the kinds of a sibling literal, so a reducer-kind read can hide. The canary
// closes the class for every probed statement: after the classifier's own scrub
// and dead-branch blanking, each live fact_records alias must carry a recognized
// positive kind predicate of its own, or be one of a pinned list of fact_id
// join-backs.

var (
	// factRecordsRef finds a fact_records FROM item, JOIN item, comma-list item, or
	// parenthesized join-tree item, and its optional alias. Group 1 is the name, so
	// a qualified column such as fact_records.fact_kind can be told from a relation.
	factRecordsRef = regexp.MustCompile(`(?i)(?:\bfrom|\bjoin|,|\()\s*(?:only\s+)?(?:public\.)?(fact_records)\b(?:\s+as\b)?\s*([a-z_][a-z0-9_]*)?`)

	// aliasStopWords are words that follow a relation and are not its alias.
	aliasStopWords = map[string]struct{}{
		"where": {}, "join": {}, "on": {}, "left": {}, "right": {}, "inner": {}, "cross": {}, "full": {}, "natural": {},
		"using": {}, "group": {}, "order": {}, "limit": {}, "offset": {}, "union": {}, "intersect": {}, "except": {},
		"window": {}, "having": {}, "for": {}, "lateral": {}, "returning": {}, "set": {}, "fetch": {},
	}

	kindValue = `(?:'[^']+'|\$\d+)`
)

// kindPredicate returns the recognized positive kind predicates of one alias:
// = literal, = $n, = ANY($n), and IN (literals and binds). An empty qualifier
// matches the bare column name.
func kindPredicate(qualifier string) *regexp.Regexp {
	column := `(?:^|[^.\w])fact_kind`
	if qualifier != "" {
		column = `\b` + regexp.QuoteMeta(qualifier) + `\.fact_kind`
	}
	return regexp.MustCompile(`(?i)` + column + `\s*(?:=\s*` + kindValue + `|=\s*any\s*\(\s*\$\d+(?:\s*::\s*[a-z_]+(?:\[\])?)?\s*\)|in\s*\(\s*` + kindValue + `(?:\s*,\s*` + kindValue + `)*\s*\))`)
}

var negatedPrefix = regexp.MustCompile(`(?i)\bnot\s*\(?\s*$`)

// hasPositiveKindPredicate reports a recognized kind predicate in text that is a
// conjunct of a WHERE or ON clause: not the operand of a NOT, not an arm of an OR,
// not inside a function call or a CASE, and not the operand of IS, <>, a cast or a
// concatenation. The predicate then restricts the relation's rows, rather than
// only appearing in the statement.
func hasPositiveKindPredicate(qualifier, text string) bool {
	for _, loc := range kindPredicate(qualifier).FindAllStringIndex(text, -1) {
		if !negatedPrefix.MatchString(text[:loc[0]]) && restrictsRows(text, loc[0], loc[1]) {
			return true
		}
	}
	return false
}

// clauseEndWords end a WHERE or ON clause at its own depth.
var clauseEndWords = map[string]struct{}{
	"group": {}, "order": {}, "limit": {}, "offset": {}, "having": {}, "window": {}, "union": {}, "intersect": {},
	"except": {}, "join": {}, "left": {}, "right": {}, "inner": {}, "cross": {}, "full": {}, "natural": {},
	"where": {}, "on": {}, "returning": {}, "for": {}, "fetch": {}, "using": {},
}

// restrictsRows reports whether the predicate at text[start:end] is a depth-zero
// AND conjunct of the WHERE or ON clause that holds it, climbing out of boolean
// groups that are themselves conjuncts.
func restrictsRows(text string, start, end int) bool {
	for {
		// What follows the predicate, or the group that holds it, must not test or
		// transform its truth value.
		if next := skipSpaces(text, end); next < len(text) {
			rest := strings.ToLower(text[next:min(len(text), next+4)])
			if strings.HasPrefix(rest, "is ") || strings.HasPrefix(rest, "<>") || strings.HasPrefix(rest, "!=") ||
				strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "::") || strings.HasPrefix(rest, "||") {
				return false
			}
		}
		left, openAt, ok := clauseStart(text, start)
		if !ok {
			return false
		}
		right, closeAt := clauseEnd(text, end)
		if hasDepthZeroOr(text, left, right) {
			return false
		}
		if openAt < 0 {
			return true
		}
		// The predicate sits in a parenthesized group: it must be a boolean group
		// (after AND, WHERE, ON, and the like) that is itself a conjunct.
		if !booleanContext(text, openAt) || closeAt < 0 {
			return false
		}
		if negatedPrefix.MatchString(text[:openAt]) {
			return false
		}
		start, end = openAt, closeAt+1
	}
}

// clauseStart scans left from start at the predicate's own depth. It returns the
// start of the clause text, the "(" that opens an enclosing group (or -1 when the
// clause keyword WHERE or ON was reached), and false when a word that is not a
// WHERE or ON clause (SELECT, FROM, WHEN, CASE, and the like) comes first.
func clauseStart(text string, start int) (left, open int, ok bool) {
	depth := 0
	for i := start - 1; i >= 0; i-- {
		switch text[i] {
		case ')':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			return i + 1, i, true
		default:
			if depth != 0 || !isWordByte(text[i]) || (i > 0 && isWordByte(text[i-1])) {
				continue
			}
			word := strings.ToLower(wordAt(text, i, len(text)))
			switch word {
			case "where", "on":
				return i + len(word), -1, true
			case "and", "or", "not":
			case "select", "from", "join", "when", "then", "else", "case", "having", "group", "order", "limit", "union", "with":
				return 0, -1, false
			}
		}
	}
	return 0, -1, false
}

// clauseEnd scans right from end at the predicate's own depth to the word that
// ends the clause, the ")" that closes the enclosing group, or the end of text.
func clauseEnd(text string, end int) (right, closeAt int) {
	depth := 0
	for i := end; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i, i
			}
			depth--
		default:
			if depth == 0 && isWordStart(text, i) {
				word := wordAt(text, i, len(text))
				if _, ends := clauseEndWords[strings.ToLower(word)]; ends {
					return i, -1
				}
				i += len(word) - 1
			}
		}
	}
	return len(text), -1
}

// hasDepthZeroOr reports an OR at depth zero of text[lo:hi].
func hasDepthZeroOr(text string, lo, hi int) bool {
	return len((&evaluator{text: text, dead: &[]span{}}).split(lo, hi, "or")) > 1
}

// KindlessFactRecordsAliases returns the live fact_records aliases of sql that
// carry no recognized positive kind predicate of their own. The predicate is
// searched in the ref's own scope: the innermost parenthesized subquery that holds
// it (a parenthesized join tree is skipped, since its WHERE sits outside), cut to
// the set-operator arm that holds the ref. A reused alias name elsewhere in the
// statement therefore satisfies nothing. A statement the scrub or the dead-branch
// pass cannot read returns an error.
func KindlessFactRecordsAliases(sql string) ([]string, error) {
	clean, masked, err := scrub(sql)
	if err != nil {
		return nil, err
	}
	dead, err := deadSpans(masked)
	if err != nil {
		return nil, err
	}
	clean, masked = blankSpans(clean, dead), blankSpans(masked, dead)
	var kindless []string
	for _, loc := range factRecordsRef.FindAllStringSubmatchIndex(masked, -1) {
		if loc[3] < len(masked) && masked[loc[3]] == '.' {
			continue // a qualified column, not a relation
		}
		alias := ""
		if loc[4] >= 0 {
			alias = strings.ToLower(masked[loc[4]:loc[5]])
		}
		if _, stop := aliasStopWords[alias]; stop {
			alias = ""
		}
		lo, hi := scopeArm(masked, loc[0])
		qualifier := alias
		if qualifier == "" {
			qualifier = "fact_records"
		}
		if hasPositiveKindPredicate(qualifier, clean[lo:hi]) {
			continue
		}
		// An unaliased ref may also carry a bare fact_kind in its scope.
		if alias == "" && hasPositiveKindPredicate("", clean[lo:hi]) {
			continue
		}
		kindless = append(kindless, qualifier)
	}
	sort.Strings(kindless)
	return kindless, nil
}

// scopeArm returns the span a ref at pos is judged in: the innermost
// parenthesized subquery around it (or the whole text), narrowed to the arm
// between depth-zero UNION, INTERSECT, and EXCEPT words that holds pos.
func scopeArm(text string, pos int) (int, int) {
	lo, hi := 0, len(text)
	for at := pos; ; {
		open := openingParen(text, at)
		if open < 0 {
			break
		}
		end := closeOf(text, open)
		if end < 0 {
			end = len(text)
		}
		if hasTopLevelClause(text, open+1, end) {
			lo, hi = open+1, end
			break
		}
		at = open // a join tree: look outward for the subquery that holds it
	}
	arms, _ := splitSetOperators(text, lo, hi)
	for _, arm := range arms {
		if pos >= arm.start && pos <= arm.end {
			return arm.start, arm.end
		}
	}
	return lo, hi
}

// openingParen returns the index of the innermost "(" before at that is still
// open at at, or -1.
func openingParen(text string, at int) int {
	depth := 0
	for i := at - 1; i >= 0; i-- {
		switch text[i] {
		case ')':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			return i
		}
	}
	return -1
}
