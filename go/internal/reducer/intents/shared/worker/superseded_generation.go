// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// SupersededGenerationReader is the optional bounded lookup port that lets
// SelectPartitionBatch drain intents whose scope generation was superseded
// (#7121). An IntentReader that also implements it opts in; a reader that does
// not keeps the pre-#7121 selection behavior byte-identical.
//
// Why the terminal status and not "generation is not the scope's active
// generation": a pending generation's intents are selectable before it
// activates, so "not active" would race with activation and drop live work.
// scope_generations.status = 'superseded' is terminal by the domain model
// (scope.allowedGenerationTransitions, go/internal/scope/scope.go:196): a
// generation only reaches it after a newer generation took over, and its
// workload_materialization phase is never published afterwards, so the
// readiness gate would block its intents forever. The SQL does not yet enforce
// that terminality on every writer; that gap is tracked in #TBD.
type SupersededGenerationReader interface {
	// SupersededGenerationIDs returns the subset of generationIDs whose scope
	// generation is superseded. It must cost one bounded round trip per call.
	// Ids that are not scope generations (for example relationship-generation
	// ids) are simply absent from the result, which keeps them selectable.
	SupersededGenerationIDs(ctx context.Context, generationIDs []string) (map[string]struct{}, error)
}

// splitSupersededGenerationRows separates rows whose generation is superseded
// from the rest with ONE lookup over the distinct generation ids of the batch.
// A reader without the port, or an empty batch, returns rows unchanged. A lookup
// error is returned to the caller so the selection fails instead of silently
// dropping or silently keeping rows.
func splitSupersededGenerationRows(
	ctx context.Context,
	reader IntentReader,
	rows []sharedintent.Row,
) (kept []sharedintent.Row, supersededIDs []string, err error) {
	lookup, ok := reader.(SupersededGenerationReader)
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

	superseded, err := lookup.SupersededGenerationIDs(ctx, generationIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("look up superseded generations: %w", err)
	}
	if len(superseded) == 0 {
		return rows, nil, nil
	}

	kept = make([]sharedintent.Row, 0, len(rows))
	for _, row := range rows {
		if _, isSuperseded := superseded[row.GenerationID]; isSuperseded {
			supersededIDs = append(supersededIDs, row.IntentID)
			continue
		}
		kept = append(kept, row)
	}
	return kept, supersededIDs, nil
}
