// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
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
)

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

// flattenResidualMessage collapses every run of whitespace to a single space.
//
// Printing through %q is what stops a raw newline reaching the log, so this is
// not the guard against error text forging a "[FAIL] ..." line. What it buys is
// the message being readable at all: %q would otherwise render a multi-line Go
// error chain as a run of "\n" escapes, which is hard to read and spends the
// residualMessageMaxLen budget on punctuation rather than on the cause.
func flattenResidualMessage(message string) string {
	return strings.Join(strings.Fields(message), " ")
}

// truncateResidualMessage cuts to residualMessageMaxLen runes — runes, not
// bytes, so a cut never lands mid-character and produces mojibake — and says it
// cut, because a silently clipped error reads as a complete one.
func truncateResidualMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= residualMessageMaxLen {
		return message
	}
	return string(runes[:residualMessageMaxLen]) + residualMessageTruncationMarker
}
