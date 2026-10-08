// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// EmittedFullSuccessorReader is the optional bounded lookup port that lets the
// code_calls and repo_dependency lanes drain rows a newer emitted full
// generation covers (#7165). An IntentReader that also implements it opts in;
// a reader that does not keeps its pre-#7165 selection behavior byte-identical.
//
// This drain differs from the #7121 superseded-generation drain in
// [SelectPartitionBatch]: that drain is limited to readiness-BLOCKED rows,
// because a delta successor never re-emits a ready row's edge. This drain
// covers rows of any readiness whose generation is superseded AND has a newer
// full generation that already emitted the lane's domain, so the successor's
// own rows re-emit every unit's edges when they project. The newest full
// generation, and every generation after it, is never covered and keeps
// projecting. Do not use this port in SelectPartitionBatch: the shared
// runner's domains do not share the per-generation atomic-emission proof this
// drain rests on (see the store SQL).
//
// No post-lookup re-read is needed here (compare drainSupersededBlockedRows):
// the drain decision never depends on the drained generation's own phase, so
// a producer that publishes after the lookup changes nothing — the successor
// still re-emits. The in-flight-producer guard still defers the drain while a
// producer runs, per the #7121 safety conditions. The #7130 Ack-reactivation
// residual is shared with #7121 and documented on the store SQL.
type EmittedFullSuccessorReader interface {
	// CoveredByEmittedFullSuccessorIDs returns the subset of generationIDs
	// whose scope generation is superseded, has no in-flight producer, and
	// is covered by a newer full generation that emitted the domain. It must
	// cost one bounded round trip per call. Ids that are not scope
	// generations are simply absent from the result, which keeps them
	// selectable.
	CoveredByEmittedFullSuccessorIDs(ctx context.Context, domain string, generationIDs []string) (map[string]struct{}, error)
}

// SplitCoveredByFullSuccessorRows separates the rows whose generation is
// covered by a newer emitted full generation from the rest with ONE lookup
// over the distinct generation ids of the rows. The caller passes the rows
// that survived its acceptance filter, so a batch with no rows (an empty
// slice) costs no round trip. A reader without the port returns rows
// unchanged. A lookup error is returned to the caller so the selection fails
// instead of silently dropping or silently keeping rows.
func SplitCoveredByFullSuccessorRows(
	ctx context.Context,
	reader IntentReader,
	domain string,
	rows []sharedintent.Row,
) (kept, drainable []sharedintent.Row, err error) {
	lookup, ok := reader.(EmittedFullSuccessorReader)
	if !ok || len(rows) == 0 {
		return rows, nil, nil
	}

	seen := make(map[string]struct{}, len(rows))
	generationIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.GenerationID == "" {
			continue
		}
		if _, dup := seen[row.GenerationID]; dup {
			continue
		}
		seen[row.GenerationID] = struct{}{}
		generationIDs = append(generationIDs, row.GenerationID)
	}
	if len(generationIDs) == 0 {
		return rows, nil, nil
	}
	sort.Strings(generationIDs)

	covered, err := lookup.CoveredByEmittedFullSuccessorIDs(ctx, domain, generationIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("look up generations covered by emitted full successor: %w", err)
	}
	if len(covered) == 0 {
		return rows, nil, nil
	}

	kept = make([]sharedintent.Row, 0, len(rows))
	for _, row := range rows {
		if _, isCovered := covered[row.GenerationID]; isCovered {
			drainable = append(drainable, row)
			continue
		}
		kept = append(kept, row)
	}
	return kept, drainable, nil
}
