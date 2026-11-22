// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"regexp"
	"strings"
)

// relationRef is one table, view, or set-returning function a FROM or JOIN
// clause names.
type relationRef struct {
	name       string
	isFunction bool
}

var (
	fromOrJoin = regexp.MustCompile(`(?i)\b(from|join)\b`)
	identifier = regexp.MustCompile(`^(?i:[a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)`)
	// notFromClause matches a FROM that is not a table list: IS DISTINCT FROM,
	// EXTRACT(field FROM expr), and TRIM, SUBSTRING, and OVERLAY forms.
	notFromClause = regexp.MustCompile(`(?i)\bis\s+(?:not\s+)?distinct\s+from\b|\bextract\s*\(\s*[a-z_]+\s+from\b|\b(?:trim|substring|overlay)\s*\([^)]*\bfrom\b`)
)

// fromListEnd holds the words that end a FROM list at depth zero.
var fromListEnd = map[string]struct{}{
	"where": {}, "group": {}, "order": {}, "limit": {}, "offset": {}, "having": {}, "union": {},
	"intersect": {}, "except": {}, "window": {}, "for": {}, "returning": {}, "on": {}, "using": {},
	"join": {}, "left": {}, "right": {}, "inner": {}, "outer": {}, "cross": {}, "full": {},
	"natural": {}, "set": {}, "values": {}, "fetch": {},
}

// relationsOf returns the relations a masked statement reads, including every
// item of a comma-separated FROM list, and a problem for each FROM or JOIN item
// it cannot parse. The caller treats a problem as a failure.
func relationsOf(masked string) (refs []relationRef, problems []string) {
	text := notFromClause.ReplaceAllStringFunc(masked, func(match string) string {
		return strings.Repeat(" ", len(match))
	})
	for _, loc := range fromOrJoin.FindAllStringIndex(text, -1) {
		isFrom := strings.EqualFold(text[loc[0]:loc[1]], "from")
		pos := loc[1]
		for {
			ref, end, problem := parseRelationItem(text, pos)
			if problem != "" {
				problems = append(problems, problem)
				break
			}
			if ref.name != "" {
				refs = append(refs, ref)
			}
			if !isFrom {
				break
			}
			next, ok := nextFromItem(text, end)
			if !ok {
				break
			}
			pos = next
		}
	}
	return refs, problems
}

// parseRelationItem reads one FROM or JOIN item starting at pos and returns the
// relation it names, the index after the name, and a problem when the item is
// neither a name, a function, nor a parenthesized subquery.
func parseRelationItem(text string, pos int) (relationRef, int, string) {
	i := skipSpaces(text, pos)
	for _, word := range []string{"only", "lateral"} {
		if hasWordAt(text, i, word) {
			i = skipSpaces(text, i+len(word))
		}
	}
	if i >= len(text) {
		return relationRef{}, i, "FROM or JOIN with no item"
	}
	if text[i] == '(' {
		closeAt := closeOf(text, i)
		if closeAt < 0 {
			return relationRef{}, i, "unbalanced parentheses in a FROM item"
		}
		return relationRef{}, closeAt + 1, ""
	}
	name := identifier.FindString(text[i:])
	if name == "" {
		return relationRef{}, i, "unparseable FROM or JOIN item: " + firstLine(text[i:])
	}
	end := i + len(name)
	isFunction := skipSpaces(text, end) < len(text) && text[skipSpaces(text, end)] == '('
	return relationRef{name: strings.ToLower(name), isFunction: isFunction}, end, ""
}

// nextFromItem scans forward from end, past any alias, to the comma that starts
// the next item of the same FROM list. It returns false at the first
// depth-zero word or closing parenthesis that ends the list.
func nextFromItem(text string, end int) (int, bool) {
	depth := 0
	for i := end; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return 0, false
			}
			depth--
		case ';':
			if depth == 0 {
				return 0, false
			}
		case ',':
			if depth == 0 {
				return i + 1, true
			}
		default:
			if depth == 0 && isWordStart(text, i) {
				word := wordAt(text, i, len(text))
				if _, ok := fromListEnd[strings.ToLower(word)]; ok {
					return 0, false
				}
				i += len(word) - 1
			}
		}
	}
	return 0, false
}

func skipSpaces(text string, i int) int {
	for i < len(text) && isSpace(text[i]) {
		i++
	}
	return i
}

func hasWordAt(text string, i int, word string) bool {
	return i+len(word) <= len(text) && strings.EqualFold(text[i:i+len(word)], word) &&
		(i+len(word) == len(text) || !isWordByte(text[i+len(word)]))
}
