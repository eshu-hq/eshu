// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// MaxLimit is the largest page a caller may request, and therefore the largest
// window one read can return.
const MaxLimit = 200

// MaxRefillReads caps how many tag-history windows ONE scoped request may read
// while refilling its page (#6564 review finding 1).
//
// Why a cap at all: refilling means reading further windows until `limit`
// visible rows are collected, and a caller whose grant covers a small slice of
// a heavily shared repository:tag could otherwise drive an unbounded scan from
// a single request.
//
// Why four: each window costs one Cypher read plus one BuiltFromCypher lookup,
// and the lookup's measured worst case is the pathological one the refill loop
// actually triggers. A page whose digests have no ContainerImage node is
// exactly the fully-withheld page that refills, and 400 such missing keys cost
// 1135-1175 ms cold on a 5,000-image store and 2247 ms at 10,000
// (docs/internal/evidence/6564-tag-history-grant-binding.md). Four windows
// bound that at roughly 4.7 s and 9 s. The first still sits inside the handler
// histogram's 5 s top bucket; the second is honestly an outlier above it, and a
// larger cap multiplies it further, which is why this is four and not a round
// ten. Four windows also let a page step over up to 4*limit withheld rows --
// 800 at MaxLimit -- before it has to report truncation.
//
// Hitting the cap is never served as a complete page: Truncated stays true and
// the cursor resumes at the exact row the scan stopped on, so following it
// continues the same walk rather than repeating or skipping history.
const MaxRefillReads = 4

// Cypher lists one image_ref's captured ContainerImageTagObservation history
// over the authoritative graph.
//
// Anchor: the existing container_image_tag_observation_ref index over
// image_ref (see go/internal/storage/cypher), so this is an indexed
// equality lookup, not a label scan. The result is fully deterministic because
// the trailing t.uid key is unique per observation node and breaks every tie,
// independent of backend null-ordering.
// first_observed_at may be empty or null for an observation whose envelope
// carried a zero ObservedAt (ON CREATE SET stores "" via ociTagObservedAtValue
// in go/internal/storage/cypher) or one created before #5459 shipped
// first_observed_at. Such rows are retained (never dropped); their
// position relative to timestamped rows follows the backend's native
// null-ordering (NornicDB and Neo4j sort nulls last on ascending ORDER BY) and
// is not relied upon for correctness -- the uid tiebreak fixes the total order.
const Cypher = `
	MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
	RETURN t.tag AS tag,
	       t.resolved_digest AS resolved_digest,
	       t.previous_digest AS previous_digest,
	       t.mutated AS mutated,
	       t.first_observed_at AS first_observed_at,
	       t.repository_id AS repository_id,
	       t.identity_strength AS identity_strength,
	       t.uid AS uid
	ORDER BY t.first_observed_at, t.uid
	SKIP $offset
	LIMIT $limit
`

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

// rowFromGraph projects one graph row into a Row.
func rowFromGraph(row map[string]any) Row {
	return Row{
		Tag:              querycontract.StringVal(row, "tag"),
		ResolvedDigest:   querycontract.StringVal(row, "resolved_digest"),
		PreviousDigest:   querycontract.StringVal(row, "previous_digest"),
		Mutated:          querycontract.BoolVal(row, "mutated"),
		FirstObservedAt:  querycontract.StringVal(row, "first_observed_at"),
		RepositoryID:     querycontract.StringVal(row, "repository_id"),
		IdentityStrength: querycontract.StringVal(row, "identity_strength"),
	}
}

// ReadWindow runs Cypher for one window of at most limit rows starting at
// offset. It reads limit+1 rows so more reports whether raw history continues
// past the window without a second statement, and trims the sentinel row off
// before returning so it is never filtered or looked up.
func ReadWindow(
	ctx context.Context,
	graph querycontract.GraphQuery,
	imageRef string,
	offset, limit int,
) (window []Row, more bool, err error) {
	rows, err := graph.Run(ctx, Cypher, map[string]any{
		"image_ref": imageRef,
		"offset":    offset,
		"limit":     limit + 1,
	})
	if err != nil {
		return nil, false, err
	}
	more = len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	window = make([]Row, 0, len(rows))
	for _, row := range rows {
		window = append(window, rowFromGraph(row))
	}
	return window, more, nil
}

// ScopedPage is the outcome of refilling one grant-filtered page.
//
// NextOffset is the raw row position the scan stopped on. It never reaches the
// wire as an integer: the handler encodes it as the cursor token
// (EncodeCursor), because the distance it advanced past Rows is the count of
// withheld rows.
type ScopedPage struct {
	Rows       []Row
	NextOffset int
	Truncated  bool
	CapReached bool
	Reads      int
	Counts     GrantCounts
}

// RefillScopedPage collects up to limit VISIBLE rows for a scoped caller,
// reading successive windows from offset until the page is full, the history
// ends, or MaxRefillReads windows have been read.
//
// Every window uses the same two single-clause statements the unscoped path
// uses -- Cypher then BuiltFromCypher -- because the pinned NornicDB build
// returns zero rows for the multi-clause grant join this would otherwise be
// written as (docs/internal/evidence/6564-tag-history-grant-binding.md).
//
// Truncated is true exactly when raw history remains beyond NextOffset, so a
// short page is either the genuine end of the history or an honest "there is
// more, keep following the cursor"; it is never a filtered-short page presented
// as complete. A window that keeps nothing still yields a usable page: count 0,
// Truncated true, and a cursor that resumes past what was read.
func RefillScopedPage(
	ctx context.Context,
	graph querycontract.GraphQuery,
	imageRef string,
	offset, limit int,
	access querycontract.RepositoryAccessFilter,
) (ScopedPage, error) {
	page := ScopedPage{
		Rows:       make([]Row, 0, limit),
		NextOffset: offset,
	}
	for page.Reads < MaxRefillReads {
		window, more, err := ReadWindow(ctx, graph, imageRef, page.NextOffset, limit)
		if err != nil {
			return page, err
		}
		page.Reads++
		if len(window) == 0 {
			return page, nil
		}

		edges := map[string][]string{}
		if digests := grantDigests(window); len(digests) > 0 {
			edges, err = LookupBuiltFromRepositories(ctx, graph, digests)
			if err != nil {
				// Fail closed: never fall back to the unfiltered window.
				return page, err
			}
		}

		for i, row := range window {
			kept, visible := grantDecision(row, edges, access, &page.Counts)
			if !visible {
				continue
			}
			page.Rows = append(page.Rows, kept)
			if len(page.Rows) < limit {
				continue
			}
			page.NextOffset += i + 1
			page.Truncated = more || i+1 < len(window)
			return page, nil
		}

		page.NextOffset += len(window)
		if !more {
			return page, nil
		}
	}

	// The cap stopped a scan that still has raw history ahead of it.
	page.Truncated = true
	page.CapReached = true
	return page, nil
}
