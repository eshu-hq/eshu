// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// SupersededGenerationReader is the optional bounded lookup port that lets
// SelectPartitionBatch drain readiness-BLOCKED intents whose scope generation
// was superseded (#7121). An IntentReader that also implements it opts in; a
// reader that does not keeps the pre-#7121 selection behavior byte-identical.
//
// The drain applies only to rows the readiness gate blocks, and only for a
// superseded generation with no in-flight producer: such a generation never
// publishes its phase row, so its blocked intents would wait forever. A producer
// already running when the successor activated (reducer claimed/running, a
// projector pending/retrying/claimed/running item, or a live
// graph_projection_phase_repair_queue row) can still publish, so the reader
// must NOT report that generation until the producer ends; a deferred row is
// re-checked on the next pass. The guard closes the in-flight producer window
// for those producers; it does not claim an edge can never be lost. Accepted
// residuals: a reducer claim whose snapshot predates the successor's activation
// committing after the lookup, admin projector replay, and Ack re-activation
// (the last two tracked in #7130). This holds for every gated domain the
// shared runner serves ([ReadinessPhase]), not only runs_in and handles_route. Ready rows (the phase row
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
	// generation is superseded and has no in-flight producer (work item or
	// phase-repair row). It must cost one bounded round trip per call.
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
// keeping rows. The returned drainable rows are candidates only: the caller
// must re-read readiness for them after this lookup (see
// drainSupersededBlockedRows) before it drains any.
func splitSupersededGenerationRows(
	ctx context.Context,
	reader IntentReader,
	rows []sharedintent.Row,
) (kept, drainable []sharedintent.Row, err error) {
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
			drainable = append(drainable, row)
			continue
		}
		kept = append(kept, row)
	}
	return kept, drainable, nil
}

// supersededDrainResult is the outcome of draining readiness-blocked rows whose
// generation is superseded.
type supersededDrainResult struct {
	// Blocked are the rows that stay blocked (their generation is not drainable).
	Blocked []sharedintent.Row
	// Ready and Terminal are rows the post-lookup readiness re-check found
	// published after the first read; they project instead of draining.
	Ready    []sharedintent.Row
	Terminal []sharedintent.Row
	// DrainedIDs are the intent ids of rows drained as superseded.
	DrainedIDs []string
}

// drainSupersededBlockedRows drains the readiness-blocked rows whose generation
// is superseded with no in-flight producer, without racing a producer that
// publishes in between.
//
// Readiness is read before the superseded lookup, so a producer that publishes
// the phase row and acks between the two reads would leave a now-ready row in
// the blocked set, and the lookup (which sees no in-flight producer any more)
// would drain it. A delta successor never re-emits that edge. The fix orders the
// reads the safe way round: after the lookup names the drainable rows, readiness
// is read again for ONLY those rows. A producer that is not in flight at the
// lookup has already published, so a read taken after the lookup is final for
// that producer; rows that turned ready (or terminal) project, and only rows still
// blocked drain. The re-check costs one extra bounded round trip and only runs
// when the lookup returned rows to drain.
func drainSupersededBlockedRows(
	ctx context.Context,
	reader IntentReader,
	domain string,
	blockedRows []sharedintent.Row,
	readinessLookup gpphase.ReadinessLookup,
	readinessPrefetch gpphase.ReadinessPrefetch,
	endpointPresence gpphase.EndpointPresenceLookup,
) (supersededDrainResult, error) {
	kept, drainable, err := splitSupersededGenerationRows(ctx, reader, blockedRows)
	if err != nil {
		return supersededDrainResult{}, err
	}
	if len(drainable) == 0 {
		return supersededDrainResult{Blocked: kept}, nil
	}

	ready, stillBlocked, terminal, err := FilterRowsByReadiness(
		ctx, domain, drainable, readinessLookup, readinessPrefetch, endpointPresence,
	)
	if err != nil {
		return supersededDrainResult{}, fmt.Errorf("re-check readiness before superseded drain: %w", err)
	}
	drained := make([]string, 0, len(stillBlocked))
	for _, row := range stillBlocked {
		drained = append(drained, row.IntentID)
	}
	return supersededDrainResult{Blocked: kept, Ready: ready, Terminal: terminal, DrainedIDs: drained}, nil
}
