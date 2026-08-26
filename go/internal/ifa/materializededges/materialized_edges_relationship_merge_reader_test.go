// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This file holds the relationship-merge READER split out of
// materialized_edges_direct_family_scan_test.go, which had grown past the
// 500-line cap CLAUDE.md sets for every file in this repository. The seam is
// the question each half answers: everything here decides whether a span of
// Cypher TEXT merges a relationship, while the scan file decides which Cypher a
// reducer port reaches. The reader is also what stripCypherComments in
// materialized_edges_cypher_comment_scan_test.go exists to serve, so the two
// belong next to each other in review. Merging either back restores the cap
// violation.

package materializededges

import (
	"regexp"
	"strings"
)

// relationshipMergeKeywordPattern finds where a Cypher clause that may CREATE a
// relationship begins: a MERGE or CREATE opening a node pattern. Whether that
// pattern continues into a `-[` or `<-[` relationship bracket is decided by
// mergesRelationship, which walks the pattern's parentheses instead of matching
// them.
//
// MATCH is deliberately excluded. Every family's retract template matches an
// existing relationship before deleting it (`MATCH (:Function)-[rel:TAINT_FLOWS_TO]->
// (:Function) ... DELETE rel`), and a writer's upsert template routinely
// MATCHes both endpoint nodes before merging the edge between them. Counting
// MATCH would classify every retract port, and several genuinely node-only
// writers, as edge writers — the loud-but-wrong direction, which buries the
// silent one.
//
// The relationship type itself is deliberately not captured. Four production
// templates interpolate it (`MERGE (sg)-[rel:%s]->(rule)`), so requiring a
// literal type would drop exactly the families whose type is chosen at
// runtime.
var relationshipMergeKeywordPattern = regexp.MustCompile(`(?i)\b(?:MERGE|CREATE)\s*\(`)

// mergesRelationship reports whether value contains a MERGE or CREATE whose
// node pattern continues into a relationship bracket.
//
// The node pattern is walked to its BALANCED closing parenthesis rather than
// matched with a `[^()]*` run. That run required the pattern to hold no
// parentheses of its own, so `MERGE (n:Label {id: coalesce($a, $b)})-[r:T]->(m)`
// — an ordinary merge keyed on a coalesced identity — ended its match at
// `coalesce(` and read as node-only. A port whose only write site is such a
// template would be classified node-only, its family would never enter the
// enumeration, and no ledger row would be missing for it: silent, and the
// direction this guard exists to prevent. An unbalanced pattern resolves to no
// match, as before.
//
// Adjacency after the closing parenthesis is kept exactly as the old pattern
// had it — whitespace, then an optional `<`, then `-[` — so a `-[` appearing
// anywhere later in the template still does not make an unrelated clause read
// as a relationship merge.
//
// stripCypherComments blanks comments before any of that runs, because all
// three readers misread one: the keyword pattern matches a MERGE written inside
// a comment, an unbalanced `)` in a comment moves the walk's depth, and a
// comment between the node pattern and its `-[` breaks the adjacency.
func mergesRelationship(value string) bool {
	value = stripCypherComments(value)
	for offset := 0; offset < len(value); {
		loc := relationshipMergeKeywordPattern.FindStringIndex(value[offset:])
		if loc == nil {
			return false
		}
		// The match ends on the "(" that opens the node pattern.
		open := offset + loc[1] - 1
		if closed, balanced := closingParen(value, open); balanced {
			rest := strings.TrimLeft(value[closed+1:], " \t\r\n")
			rest = strings.TrimPrefix(rest, "<")
			if strings.HasPrefix(rest, "-[") {
				return true
			}
		}
		offset = open + 1
	}
	return false
}

// closingParen returns the index of the parenthesis closing the one at open,
// and whether value holds one at all.
//
// Parentheses inside a quoted property value do not count. A template like
// `MERGE (n:Repo {path: "a/b)c"})-[r:CONTAINS]->(m)` closes its node pattern at
// the `)` inside the string literal if the walk is quote-unaware, the trailing
// `-[r:CONTAINS]->` is never seen, and the port is classified node-only -- the
// silent false-green this scan exists to prevent, one level below the
// function-call case (`coalesce($a, $b)`) the balanced walk already handles.
// No production template uses that shape today; the guard is for the day one
// does. A backslash escapes the next byte in a single- or double-quoted string
// so an escaped quote does not end it; backtick identifiers do NOT work that
// way and escape by doubling instead, which the walk handles separately.
//
// Comments are not this function's problem: mergesRelationship blanks them
// first. Not free — a `'` in `// the repo's id` used to open a quoted region
// here and swallow the rest of the template, a false-green the quote tracking
// below introduced. TestCypherCommentsDoNotHideARelationshipMerge holds it.
//
// Limit, measured rather than assumed: an UNTERMINATED quote makes the walk run
// to the end without ever closing the node pattern, so the port reads node-only
// — the false-GREEN direction. Ten adversarial inputs were tried (unterminated
// single and double quotes, a trailing backslash, a backtick label, mixed quote
// types, an escaped backslash before a closing quote, and the empty and
// lone-paren cases); none panicked or looped, and only
// the two unterminated-quote inputs misclassify. An unterminated quote is not
// valid Cypher, so no template that compiles can reach it — but that is the
// reason it is safe, and it is worth stating rather than implying the walk
// handles every shape.
func closingParen(value string, open int) (int, bool) {
	depth := 0
	var quote byte
	for i := open; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			switch {
			case quote == '`':
				// Backtick identifiers escape by DOUBLING, not with a
				// backslash. `a``b` is one identifier holding a backtick, and
				// `a\` is a legal identifier ending in one -- both compile.
				// Treating \ as an escape here ate the closing backtick, the
				// identifier never closed, the pattern read unbalanced, and the
				// port came back node-only: the false-green direction.
				if c == '`' {
					if i+1 < len(value) && value[i+1] == '`' {
						i++
						continue
					}
					quote = 0
				}
			case c == '\\':
				i++
			case c == quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// relationshipMergeLine returns the first line of value that merges a
// relationship.
//
// The search runs over comment-blanked text and the evidence quotes the RAW
// line at the same position: searching raw lines would read a MERGE inside a
// block comment as the write site, and quoting the stripped line would hand an
// operator a row of spaces. The blanking is in place, so the splits align.
func relationshipMergeLine(value string) (string, bool) {
	scannable := stripCypherComments(value)
	if !mergesRelationship(scannable) {
		return "", false
	}
	raw := strings.Split(value, "\n")
	for i, line := range strings.Split(scannable, "\n") {
		if mergesRelationship(line) {
			return strings.TrimSpace(raw[i]), true
		}
	}
	return strings.TrimSpace(value), true
}
