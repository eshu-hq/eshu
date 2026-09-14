// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// MaxLimit is the largest page a caller may request, and therefore the largest
// window one read can return. It is ALSO the fixed size of every window the
// scoped refill loop reads, independent of the caller's limit -- see
// RefillScopedPage.
const MaxLimit = 200

// Key is one observation's position in the tag-history total order
// (first_observed_at, uid). It is the continuation point a Cursor carries.
//
// NullAt distinguishes the two states a bare string cannot: At == "" is a real
// stored empty timestamp that sorts FIRST, while NullAt marks a row with no
// first_observed_at property at all (created before #5459 shipped it), which
// sorts LAST. UID is unique per observation node and breaks every tie, so the
// order is total on both backends regardless of how they rank equal timestamps.
type Key struct {
	At     string
	NullAt bool
	UID    string
}

// WindowRow pairs one projected observation with its keyset position.
//
// The Key is deliberately not a field on Row: Row is the wire shape, and there
// StringVal renders both a stored "" and an absent first_observed_at as the
// same empty string. They are different positions in the order, so the key is
// read from the raw graph row where the difference still exists.
type WindowRow struct {
	Row Row
	Key Key
}

// tagObservationProjection is the RETURN every statement below shares. It is
// one shared constant so a projection change cannot reach three statements and
// miss the fourth; the statements themselves stay whole, single-clause reads.
const tagObservationProjection = `RETURN t.tag AS tag,
	       t.resolved_digest AS resolved_digest,
	       t.previous_digest AS previous_digest,
	       t.mutated AS mutated,
	       t.first_observed_at AS first_observed_at,
	       t.repository_id AS repository_id,
	       t.identity_strength AS identity_strength,
	       t.uid AS uid`

// The four statements below all anchor on the existing
// container_image_tag_observation_ref index over image_ref (see
// go/internal/storage/cypher), so each is an indexed equality lookup, not a
// label scan, and all four are SINGLE-CLAUSE: nothing sits between the
// anchoring MATCH and the RETURN. The pinned NornicDB build routes a read with
// any clause in between into a string-slicing interpreter that returns literal
// alias text instead of values
// (docs/public/reference/nornicdb-query-pitfalls.md).
//
// The keyset predicates are built by CASE in Go rather than as one guarded
// statement on purpose. The obvious single statement would guard its optional
// disjunct with an empty-string test on the parameter, the shape the pitfalls
// page records as collapsing the whole predicate to zero rows.
//
// That citation was re-measured on the currently pinned build rather than taken
// on faith, and the result is narrower than the page states: the guard collapses
// a RELATIONSHIP-anchored read, and evaluates CORRECTLY in the single-node
// anchored shape these statements use. So the case split is not a workaround for
// a live defect in this shape. It is kept because each case then binds only the
// parameters it needs, and because making correctness depend on a guard whose
// behaviour varies by anchor shape on one build -- and is changing again
// upstream -- is a bet with no upside here. The measurements are in
// docs/internal/evidence/6564-tag-history-keyset-pagination.md.
//
// What IS live on this build and does constrain every statement here: a WHERE
// comparing a property of one node to a property of ANOTHER node is not
// evaluated (equality returns nothing, inequality returns everything). Every
// predicate below compares a node property to a PARAMETER or tests IS NULL,
// which is the form that was measured correct. Do not introduce a cross-node
// comparison here.
const (
	// FirstPageCypher reads the first page of one image_ref's history.
	FirstPageCypher = `
	MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
	` + tagObservationProjection + `
	ORDER BY t.first_observed_at, t.uid
	LIMIT $limit
`

	// AfterKeyCypher reads the rows after a TIMESTAMPED key. $after_at may be
	// the empty string, which is a real stored value rather than a sentinel:
	// t.first_observed_at compares greater than it for every non-empty string,
	// so the empty-timestamp rows that sort first need no special case.
	//
	// The third disjunct carries the null tail. Rows with no first_observed_at
	// sort last, so they are always ahead of any timestamped key and must be
	// reachable from one; a strict comparison alone would skip them entirely
	// and silently end the history early. That disjunct was measured on the pin
	// NOT to collapse the predicate the way the empty-string guard does: with
	// it the statement returns the timestamped rows after the key PLUS the tail,
	// without it exactly the timestamped rows
	// (docs/internal/evidence/6564-tag-history-keyset-pagination.md).
	AfterKeyCypher = `
	MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
	WHERE t.first_observed_at > $after_at
	   OR (t.first_observed_at = $after_at AND t.uid > $after_uid)
	   OR t.first_observed_at IS NULL
	` + tagObservationProjection + `
	ORDER BY t.first_observed_at, t.uid
	LIMIT $limit
`

	// NullTailCypher pages INSIDE the null tail, where every row compares equal
	// on first_observed_at and uid alone is the order.
	NullTailCypher = `
	MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
	WHERE t.first_observed_at IS NULL AND t.uid > $after_uid
	` + tagObservationProjection + `
	ORDER BY t.uid
	LIMIT $limit
`

	// OffsetCypher is the SKIP form, kept for unscoped and all-scope callers
	// who page by the offset parameter (their existing contract; a scoped
	// caller cannot reach it). It is retained rather than emulated because it
	// is the shape that route has always run, and it is the only statement here
	// whose continuation point is a position rather than a key.
	OffsetCypher = `
	MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
	` + tagObservationProjection + `
	ORDER BY t.first_observed_at, t.uid
	SKIP $offset
	LIMIT $limit
`
)

// Row is one captured tag observation for the selected image_ref. The query
// package exposes it as query.TagHistoryRow.
type Row struct {
	Tag              string `json:"tag"`
	ResolvedDigest   string `json:"resolved_digest"`
	PreviousDigest   string `json:"previous_digest,omitempty"`
	Mutated          bool   `json:"mutated"`
	FirstObservedAt  string `json:"first_observed_at,omitempty"`
	RepositoryID     string `json:"repository_id"`
	IdentityStrength string `json:"identity_strength,omitempty"`
}

// windowRowFromGraph projects one graph row into its wire Row and its key.
func windowRowFromGraph(row map[string]any) WindowRow {
	observedAt, present := row["first_observed_at"]
	key := Key{
		NullAt: !present || observedAt == nil,
		UID:    querycontract.StringVal(row, "uid"),
	}
	if !key.NullAt {
		key.At = querycontract.StringVal(row, "first_observed_at")
	}
	return WindowRow{
		Row: Row{
			Tag:              querycontract.StringVal(row, "tag"),
			ResolvedDigest:   querycontract.StringVal(row, "resolved_digest"),
			PreviousDigest:   querycontract.StringVal(row, "previous_digest"),
			Mutated:          querycontract.BoolVal(row, "mutated"),
			FirstObservedAt:  querycontract.StringVal(row, "first_observed_at"),
			RepositoryID:     querycontract.StringVal(row, "repository_id"),
			IdentityStrength: querycontract.StringVal(row, "identity_strength"),
		},
		Key: key,
	}
}

// Rows drops the keys from a window, for a caller that only serializes rows.
func Rows(window []WindowRow) []Row {
	rows := make([]Row, 0, len(window))
	for _, entry := range window {
		rows = append(rows, entry.Row)
	}
	return rows
}

// keysetStatement selects the statement and the parameters for a keyset read.
// after is nil for the first page. Only the parameters the chosen statement
// names are bound, so no statement carries an unused or empty-string parameter
// the pinned build could misread.
func keysetStatement(imageRef string, after *Key, limit int) (string, map[string]any) {
	params := map[string]any{"image_ref": imageRef, "limit": limit}
	switch {
	case after == nil:
		return FirstPageCypher, params
	case after.NullAt:
		params["after_uid"] = after.UID
		return NullTailCypher, params
	default:
		params["after_at"] = after.At
		params["after_uid"] = after.UID
		return AfterKeyCypher, params
	}
}

// ReadWindow runs one keyset window of at most limit rows after the given key,
// or the first page when after is nil. It reads limit+1 rows so more reports
// whether raw history continues past the window without a second statement,
// and trims the sentinel row off before returning so it is never filtered,
// looked up, or named by a cursor.
func ReadWindow(
	ctx context.Context,
	graph querycontract.GraphQuery,
	imageRef string,
	after *Key,
	limit int,
) (window []WindowRow, more bool, err error) {
	statement, params := keysetStatement(imageRef, after, limit+1)
	rows, err := graph.Run(ctx, statement, params)
	if err != nil {
		return nil, false, err
	}
	return trimWindow(rows, limit)
}

// ReadOffsetWindow runs the SKIP form for an unscoped or all-scope caller that
// paged by the offset parameter. It is the same limit+1 sentinel contract as
// ReadWindow; only the continuation point differs.
func ReadOffsetWindow(
	ctx context.Context,
	graph querycontract.GraphQuery,
	imageRef string,
	offset, limit int,
) (window []WindowRow, more bool, err error) {
	rows, err := graph.Run(ctx, OffsetCypher, map[string]any{
		"image_ref": imageRef,
		"offset":    offset,
		"limit":     limit + 1,
	})
	if err != nil {
		return nil, false, err
	}
	return trimWindow(rows, limit)
}

// trimWindow applies the shared limit+1 sentinel contract to a raw result set.
func trimWindow(rows []map[string]any, limit int) ([]WindowRow, bool, error) {
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	window := make([]WindowRow, 0, len(rows))
	for _, row := range rows {
		window = append(window, windowRowFromGraph(row))
	}
	return window, more, nil
}
