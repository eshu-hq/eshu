// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// packageRegistryCorrelationFactRow is one raw (undecoded) fact row scanned
// from fact_records, before the typed factschema decode seam runs. Keeping
// the raw row separate from PackageRegistryCorrelationRow lets
// buildPackageRegistryCorrelationPage compute pagination from the RAW fetched
// fact count and fact_id sequence before any row is dropped by a failed
// decode.
type packageRegistryCorrelationFactRow struct {
	FactID        string
	FactKind      string
	SchemaVersion string
	Payload       []byte
}

// PackageRegistryCorrelationPage is one bounded page of package ownership,
// publication, or consumption correlation facts, with Truncated and
// NextCursorCorrelationID derived from the RAW fetched fact count/fact_id
// sequence -- never from len(Rows) -- so a malformed or unsupported-version
// fact inside the visible window cannot make a truncated page report itself
// complete or corrupt the forward cursor. Mirrors WorkItemEvidencePage's
// #4733 fix for the same failure class.
type PackageRegistryCorrelationPage struct {
	// Rows is every fact in the visible window (the caller's requested Limit
	// facts of the fetch) that decoded successfully, in fetch order. It may be
	// shorter than the requested limit when one or more facts in the window
	// failed typed decode.
	Rows []PackageRegistryCorrelationRow
	// Truncated reports whether more facts exist beyond the visible window:
	// true when the store's internal "+1" lookahead fetch found a fact beyond
	// the caller's requested Limit, independent of how many of the visible
	// window's facts decoded.
	Truncated bool
	// NextCursorCorrelationID is the fact_id of the last FETCHED fact in the
	// visible window (not the last DECODED row), set only when Truncated is
	// true. A caller pages forward with after_correlation_id=
	// NextCursorCorrelationID regardless of whether that boundary fact itself
	// decoded, so no evidence is ever skipped or re-fetched because it
	// happened to be malformed.
	NextCursorCorrelationID string
}

// buildPackageRegistryCorrelationPage decodes the visible window of a store
// fetch and derives pagination from the RAW fetched fact count, never from
// how many facts decoded (mirrors #4733's buildWorkItemEvidencePage). facts
// is the full fetch window in fetch order (fact_id ascending); fetchLimit is
// the store's "+1" lookahead fetch bound -- the caller's requested Limit plus
// one.
//
// The visible window is the first fetchLimit-1 facts. Truncated is true when
// MORE than fetchLimit-1 facts were fetched (the lookahead fact is present),
// and NextCursorCorrelationID is that window's last FETCHED fact_id --
// regardless of whether that fact itself decoded -- so a fact dropped
// mid-window by a failed typed decode can never make a truncated page look
// complete or corrupt the forward cursor, and the lookahead fact itself is
// never decoded or leaked into Rows.
func buildPackageRegistryCorrelationPage(
	facts []packageRegistryCorrelationFactRow,
	fetchLimit int,
) (PackageRegistryCorrelationPage, error) {
	visibleLimit := fetchLimit - 1
	if visibleLimit < 0 {
		visibleLimit = 0
	}
	truncated := len(facts) > visibleLimit
	window := facts
	if truncated {
		window = facts[:visibleLimit]
	}
	rows := make([]PackageRegistryCorrelationRow, 0, len(window))
	for _, fact := range window {
		row, ok, err := decodePackageRegistryCorrelationRow(fact.FactID, fact.FactKind, fact.SchemaVersion, fact.Payload)
		if err != nil {
			return PackageRegistryCorrelationPage{}, err
		}
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	page := PackageRegistryCorrelationPage{Rows: rows, Truncated: truncated}
	if truncated && len(window) > 0 {
		page.NextCursorCorrelationID = window[len(window)-1].FactID
	}
	return page, nil
}
