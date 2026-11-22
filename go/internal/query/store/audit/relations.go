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

// fromListEnd holds the words that end a FROM list at depth zero. Join words
// (JOIN, ON, USING, LEFT, CROSS, and the like) do not: a comma after a join
// item starts the next item of the same FROM list.
var fromListEnd = map[string]struct{}{
	"where": {}, "group": {}, "order": {}, "limit": {}, "offset": {}, "having": {}, "union": {},
	"intersect": {}, "except": {}, "window": {}, "for": {}, "returning": {}, "set": {}, "values": {},
	"fetch": {},
}

// relationsOf returns the relations a masked statement reads, including every
// item of a comma-separated FROM list, and a problem for each FROM or JOIN item
// it cannot parse. The caller treats a problem as a failure.
func relationsOf(masked string) (refs []relationRef, problems []string) {
	text := blankNonClauseFrom(masked)
	for _, loc := range fromOrJoin.FindAllStringIndex(text, -1) {
		isFrom := strings.EqualFold(text[loc[0]:loc[1]], "from")
		got, problem := parseItems(text, loc[1], isFrom)
		refs = append(refs, got...)
		if problem != "" {
			problems = append(problems, problem)
		}
	}
	return refs, problems
}

// parseItems reads the item at pos and, for a FROM list, every comma-separated
// item after it, past any join items between them.
func parseItems(text string, pos int, list bool) ([]relationRef, string) {
	var refs []relationRef
	for {
		got, end, problem := parseRelationItem(text, pos)
		if problem != "" {
			return refs, problem
		}
		refs = append(refs, got...)
		if !list {
			return refs, ""
		}
		next, ok := nextFromItem(text, end)
		if !ok {
			return refs, ""
		}
		pos = next
	}
}

// parseRelationItem reads one FROM or JOIN item starting at pos and returns the
// relations it names, the index after the item, and a problem when the item is
// neither a name, a function, a parenthesized subquery, nor a parenthesized
// join tree. A parenthesized item that holds no SELECT, VALUES, or WITH is a
// join tree and is read as a nested FROM list.
func parseRelationItem(text string, pos int) ([]relationRef, int, string) {
	i := skipSpaces(text, pos)
	for _, word := range []string{"only", "lateral"} {
		if hasWordAt(text, i, word) {
			i = skipSpaces(text, i+len(word))
		}
	}
	if i >= len(text) {
		return nil, i, "FROM or JOIN with no item"
	}
	if text[i] == '(' {
		closeAt := closeOf(text, i)
		if closeAt < 0 {
			return nil, i, "unbalanced parentheses in a FROM item"
		}
		if hasTopLevelClause(text, i+1, closeAt) {
			return nil, closeAt + 1, ""
		}
		inner, problem := parseItems(text[:closeAt], i+1, true)
		return inner, closeAt + 1, problem
	}
	name := identifier.FindString(text[i:])
	if name == "" {
		return nil, i, "unparseable FROM or JOIN item: " + firstLine(text[i:])
	}
	end := i + len(name)
	isFunction := skipSpaces(text, end) < len(text) && text[skipSpaces(text, end)] == '('
	return []relationRef{{name: strings.ToLower(name), isFunction: isFunction}}, end, ""
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

// blankNonClauseFrom blanks the FROM keyword of IS DISTINCT FROM, EXTRACT, TRIM,
// SUBSTRING, and OVERLAY forms and keeps everything else, parentheses included,
// so depth counting stays right.
func blankNonClauseFrom(masked string) string {
	return notFromClause.ReplaceAllStringFunc(masked, func(match string) string {
		at := strings.LastIndex(strings.ToLower(match), "from")
		if at < 0 {
			return match
		}
		return match[:at] + "    " + match[at+4:]
	})
}
