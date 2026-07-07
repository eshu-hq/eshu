// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// WorkItemEvidencePage is one bounded work-item evidence page: the
// typed-decoded, response-ready rows for the visible window, plus pagination
// facts derived from the RAW fetched fact count and fact_id sequence — never
// from len(Rows). A fact inside the visible window that fails typed decode is
// dropped from Rows (the accuracy guarantee Contract System v1 protects), but
// that drop must not also corrupt pagination: Truncated and NextCursorFactID
// are computed from how many facts were actually fetched, so a malformed fact
// in the middle of a page can never make a truncated page report itself
// complete and hide the evidence beyond it (#4733).
type WorkItemEvidencePage struct {
	// Rows is every fact in the visible window (the first requested-limit
	// facts of the fetch) that decoded successfully, in fetch order. It may be
	// shorter than the requested limit when one or more facts in the window
	// failed typed decode.
	Rows []WorkItemEvidenceRow
	// Truncated reports whether more facts exist beyond the visible window:
	// true when the store fetched more than the requested limit of facts (the
	// "+1" lookahead fact was present), independent of how many of the
	// visible window's facts decoded.
	Truncated bool
	// NextCursorFactID is the fact_id of the last FETCHED fact in the visible
	// window (not the last DECODED row), set only when Truncated is true. A
	// caller pages forward with after_fact_id=NextCursorFactID regardless of
	// whether that boundary fact itself decoded, so no evidence is ever
	// skipped or re-fetched because it happened to be malformed.
	NextCursorFactID string
}

// buildWorkItemEvidencePage decodes the visible window of a store fetch and
// derives pagination from the RAW fetched fact count, never from how many
// facts decoded (#4733). facts is the full fetch window in fetch order
// (fact_id ascending); fetchLimit is the store's "+1" lookahead fetch bound —
// the caller's requested page size plus one, the same convention
// PostgresWorkItemEvidenceStore.ListWorkItemEvidence has always used for its
// SQL LIMIT parameter.
//
// The visible window is the first fetchLimit-1 facts. Truncated is true when
// MORE than fetchLimit-1 facts were fetched (the lookahead fact is present),
// and NextCursorFactID is that window's last FETCHED fact_id — regardless of
// whether that fact itself decoded — so a fact dropped mid-window by a failed
// typed decode can never make a truncated page look complete or corrupt the
// forward cursor.
func buildWorkItemEvidencePage(facts []workItemEvidenceFactRow, fetchLimit int) WorkItemEvidencePage {
	visibleLimit := fetchLimit - 1
	if visibleLimit < 0 {
		visibleLimit = 0
	}
	truncated := len(facts) > visibleLimit
	window := facts
	if truncated {
		window = facts[:visibleLimit]
	}
	page := WorkItemEvidencePage{
		Rows:      buildWorkItemEvidenceRows(window),
		Truncated: truncated,
	}
	if truncated && len(window) > 0 {
		page.NextCursorFactID = window[len(window)-1].FactID
	}
	return page
}
