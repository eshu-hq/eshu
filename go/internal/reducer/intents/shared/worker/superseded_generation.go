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
// SelectPartitionBatch drain readiness-BLOCKED intents whose scope generation
// was superseded (#7121). An IntentReader that also implements it opts in; a
// reader that does not keeps the pre-#7121 selection behavior byte-identical.
//
// The drain applies only to rows the readiness gate blocks: a generation that
// was superseded before workload materialization ran never publishes its phase
// row, so its blocked intents would wait forever. Ready rows (the phase row
// published) and terminal rows on a superseded generation are NOT drained and
// still project. A delta successor (scope_generations.is_delta) carries only
// changed-file facts and its retract is file-scoped, so it never re-emits the
// edge of an untouched file; draining a ready row would lose that edge
// permanently. A full successor would re-emit it, but the reader cannot know
// which kind of successor exists, so ready rows are left alone.
//
// Why the terminal status and not "generation is not the scope's active
// generation": a pending generation's intents are selectable before it
// activates, so "not active" would race with activation and drop live work.
// scope_generations.status = 'superseded' is terminal by the domain model
// (scope.allowedGenerationTransitions, go/internal/scope/scope.go:196). The SQL
// does not yet enforce that terminality on every writer; that gap is tracked in
// #7130.
type SupersededGenerationReader interface {
	// SupersededGenerationIDs returns the subset of generationIDs whose scope
	// generation is superseded. It must cost one bounded round trip per call.
	// Ids that are not scope generations (for example relationship-generation
	// ids) are simply absent from the result, which keeps them selectable.
	SupersededGenerationIDs(ctx context.Context, generationIDs []string) (map[string]struct{}, error)
}

// splitSupersededGenerationRows separates the readiness-blocked rows whose
// generation is superseded from the rest with ONE lookup over the distinct
// generation ids of the blocked rows. The caller passes only blocked rows, so a
// batch with nothing blocked (an empty slice) costs no round trip. A reader
// without the port returns rows unchanged. A lookup error is returned to the
// caller so the selection fails instead of silently dropping or silently
// keeping rows.
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
