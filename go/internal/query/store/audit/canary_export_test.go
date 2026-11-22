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
	// factRecordsRef finds a fact_records FROM or JOIN item and its optional alias.
	factRecordsRef = regexp.MustCompile(`(?i)(?:\bfrom|\bjoin|,)\s+(?:only\s+)?(?:public\.)?fact_records\b(?:\s+as\b)?\s*([a-z_][a-z0-9_]*)?`)

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
	return regexp.MustCompile(`(?i)` + column + `\s*(?:=\s*` + kindValue + `|=\s*any\s*\(\s*\$\d+|in\s*\(\s*` + kindValue + `(?:\s*,\s*` + kindValue + `)*\s*\))`)
}

var negatedPrefix = regexp.MustCompile(`(?i)\bnot\s*\(?\s*$`)

// hasPositiveKindPredicate reports a recognized kind predicate in text that is
// not the operand of a NOT.
func hasPositiveKindPredicate(qualifier, text string) bool {
	for _, loc := range kindPredicate(qualifier).FindAllStringIndex(text, -1) {
		if !negatedPrefix.MatchString(text[:loc[0]]) {
			return true
		}
	}
	return false
}

// KindlessFactRecordsAliases returns the live fact_records aliases of sql that
// carry no recognized positive kind predicate of their own. A statement the
// scrub or the dead-branch pass cannot read returns an error.
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
	locs := factRecordsRef.FindAllStringSubmatchIndex(masked, -1)
	var kindless []string
	for _, loc := range locs {
		alias := ""
		if loc[2] >= 0 {
			alias = strings.ToLower(masked[loc[2]:loc[3]])
		}
		if _, stop := aliasStopWords[alias]; stop {
			alias = ""
		}
		qualifier := alias
		if qualifier == "" {
			qualifier = "fact_records"
		}
		if hasPositiveKindPredicate(qualifier, clean) {
			continue
		}
		// A bare fact_kind belongs to an unaliased ref when it sits in the same
		// parenthesized block (a CTE body or subquery), or the statement when the
		// ref is outside any.
		if alias == "" {
			lo, hi := enclosingBlock(masked, loc[0])
			if hasPositiveKindPredicate("", clean[lo:hi]) {
				continue
			}
		}
		kindless = append(kindless, qualifier)
	}
	sort.Strings(kindless)
	return kindless, nil
}

// enclosingBlock returns the bounds of the innermost parenthesized block that
// holds at, or the whole text when none does.
func enclosingBlock(text string, at int) (int, int) {
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
			if end := closeOf(text, i); end > i {
				return i + 1, end
			}
			return i + 1, len(text)
		}
	}
	return 0, len(text)
}
