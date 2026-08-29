// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Rendering half of the drain's residual breakdown: the group label both lists
// share, and the bounded failure-message tail. Split from drains.go, which owns
// the queries and the poll loop, to keep both under the repo's file cap.

// residualGroupLabel names one residual group. Shared by the count list and the
// message list so a reader can match a message to its count by string equality,
// and so the two cannot drift into different spellings of the same group.
func residualGroupLabel(row residualRow) string {
	label := row.Domain + "/" + row.Status
	if row.FailureClass != "" {
		label += "/" + row.FailureClass
	}
	return label
}

// Bounds on the printed error text. These are the whole reason this is safe to
// print: a residual of 624 rows collapses to a handful of groups, and only the
// largest few of those get a message, each cut to one short line.
const (
	// residualMessageMaxLen is the printed budget for one group's message, in
	// runes. Long enough for the leading frames of a Go error chain, which is
	// where the cause is ("reduce intent: graph write: ..."); short enough that
	// the full set stays under a kilobyte and cannot bury the counts beside it.
	residualMessageMaxLen = 200

	// maxResidualMessageGroups bounds how many groups get a message at all.
	// residualBreakdownSQL orders by count descending, so these are the groups
	// that actually explain the residual. Every group keeps its count either way.
	maxResidualMessageGroups = 4

	// residualMessageTruncationMarker is deliberately ASCII: the message is
	// printed through %q, which would escape a non-ASCII marker into a \u
	// sequence and stop a reader (or a grep) from recognising it.
	residualMessageTruncationMarker = "...(truncated)"

	// residualMessageFetchLen is how many characters the QUERY returns for one
	// group: exactly one more than the printed budget. That extra character is
	// what lets truncateResidualMessage tell a message that ENDED at the budget
	// from one the database CUT at it. Fetching exactly the budget instead
	// makes every long error arrive as a complete-looking short one, which is
	// worse than no message: the reader stops at a cause that was never the
	// whole cause.
	residualMessageFetchLen = residualMessageMaxLen + 1
)

// residualMessageColumnSQL is one stored failure_message, normalized and cut to
// the fetch bound, as the query sees it.
//
// The whitespace collapse happens in SQL, BEFORE the cut, and that order is
// load-bearing. flattenResidualMessage collapses whitespace again on the way
// out; if the database cut a raw message at 201 characters and Go then
// collapsed a run of newlines inside it, the result could land back under the
// 200-rune budget and print with no truncation marker — the very defect the
// extra character exists to prevent. Normalizing first means what Go receives
// is already flat, so its own flatten is a no-op and the length it measures is
// the length the database measured.
//
// regexp_replace with '\s+' is Postgres's spelling of what strings.Fields does
// in Go, and btrim is the leading/trailing half of it.
var residualMessageColumnSQL = fmt.Sprintf(
	`left(btrim(regexp_replace(failure_message, '\s+', ' ', 'g')), %d)`,
	residualMessageFetchLen,
)

// residualMessageAggregateSQL is the failure-message column of
// residualBreakdownSQL: the distinct messages of one group, collapsed to a
// single bounded value so a 624-row residual never crosses the wire in full.
//
// The ORDER BY is not decoration. Without it the concatenation order of a group
// holding several distinct causes is unspecified, so which cause survives the
// printed budget can differ between two runs over identical data — a red run
// that blames a contention timeout one time and a reducer defect the next is
// worse than one that blames nothing. Postgres requires the sort expression to
// be an argument of a DISTINCT aggregate verbatim (an ordinal "ORDER BY 1" is
// rejected: "in an aggregate with DISTINCT, ORDER BY expressions must appear in
// argument list"), which is why the column expression appears twice.
func residualMessageAggregateSQL() string {
	return fmt.Sprintf(
		"COALESCE(left(string_agg(DISTINCT %[1]s, ' | ' ORDER BY %[1]s), %[2]d), '')",
		residualMessageColumnSQL, residualMessageFetchLen,
	)
}

// formatResidualMessages appends the error text behind the largest residual
// groups. Returns "" when no group recorded one, so a residual of purely
// pending work prints exactly what it printed before.
//
// On content: failure_message is err.Error() from a reducer handler, stored via
// sanitizeFailureText, which only strips invalid UTF-8 and NUL bytes — it does
// no redaction. In practice that text is domain names, entity keys, file paths,
// and Postgres/Cypher error strings. This does NOT scan it for secrets; a
// claim to detect secrets is not one anyone can falsify (the reasoning is in
// go/internal/urlredact/doc.go). What it does instead is bound the exposure to
// a short prefix of a handful of groups, which is also what makes the output
// readable. Two sibling gates already print this column unelided
// (scripts/verify-ifa-fault-injection.sh, scripts/lib/golden-corpus-local-backend.sh).
func formatResidualMessages(rows []residualRow) string {
	printed := make([]string, 0, maxResidualMessageGroups)
	withMessage := 0
	for _, row := range rows {
		message := flattenResidualMessage(row.FailureMessage)
		if message == "" {
			continue
		}
		withMessage++
		if len(printed) >= maxResidualMessageGroups {
			continue
		}
		printed = append(printed, fmt.Sprintf("%s=%q", residualGroupLabel(row), truncateResidualMessage(message)))
	}
	if len(printed) == 0 {
		return ""
	}
	out := " messages: " + strings.Join(printed, " ")
	if dropped := withMessage - len(printed); dropped > 0 {
		// Saying nothing here would read as "those groups had no message",
		// which is the opposite of true and sends the reader to the wrong rows.
		out += fmt.Sprintf(" (+%d more groups with messages, not shown)", dropped)
	}
	return out
}

// residualWhitespace is the whitespace alphabet residualMessageColumnSQL's
// regexp_replace collapses, spelled for Go.
//
// It must match the DATABASE's set, not Go's. Postgres `\s` is the
// locale-sensitive `[[:space:]]`, which under the C/musl locale of the
// postgres:18-alpine image this runs against is exactly these six ASCII bytes.
// strings.Fields would be wrong here: it splits on unicode.IsSpace, which also
// matches U+00A0, U+0085, U+2000-U+200A, U+2028, U+2029 and U+3000. A message
// the database cut at residualMessageFetchLen whose text contains one of those
// would then be collapsed FURTHER in Go, could land back under
// residualMessageMaxLen, and would print with no truncation marker -- an
// incomplete error presented as a complete one, which is the failure the
// fetch-one-extra sentinel exists to prevent. Keeping the two alphabets equal
// is what makes this flatten a genuine no-op on already-collapsed input.
var residualWhitespace = regexp.MustCompile(`[\t\n\v\f\r ]+`)

// flattenResidualMessage collapses every run of whitespace to a single space.
//
// Printing through %q is what stops a raw newline reaching the log, so this is
// not the guard against error text forging a "[FAIL] ..." line. What it buys is
// the message being readable at all: %q would otherwise render a multi-line Go
// error chain as a run of "\n" escapes, which is hard to read and spends the
// residualMessageMaxLen budget on punctuation rather than on the cause.
func flattenResidualMessage(message string) string {
	return strings.Trim(residualWhitespace.ReplaceAllString(message, " "), " ")
}

// truncateResidualMessage cuts to residualMessageMaxLen runes — runes, not
// bytes, so a cut never lands mid-character and produces mojibake — and says it
// cut, because a silently clipped error reads as a complete one.
//
// It can only say so because residualBreakdownSQL hands over
// residualMessageFetchLen runes, one more than the budget. A message the
// database already cut therefore arrives one rune over and is marked here; one
// that genuinely ended at the budget arrives at the budget and is not. Shrink
// the query's bound to the printed budget and this function silently stops
// marking anything.
func truncateResidualMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= residualMessageMaxLen {
		return message
	}
	return string(runes[:residualMessageMaxLen]) + residualMessageTruncationMarker
}
