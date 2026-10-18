// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// AbsentGenerationReader is the optional bounded lookup port that lets the
// shared projection selections drain absent-acceptance intents whose scope
// generation is gone (#7853). An IntentReader that also implements it opts
// in; a reader that does not keeps its pre-#7853 selection behavior
// byte-identical, except that absent-acceptance rows are now reported to the
// caller for counting.
//
// The drain applies only to rows whose acceptance row is absent AND whose
// scope_generations row is absent: a cascade-deleted generation (which takes
// its acceptance row with it) or an intent that references a generation that
// was never committed. A retry re-upserts the same intent_id and the intent
// upsert never reopens completed_at, so a generation-present row must never
// drain: its acceptance row can still arrive and its edge would be lost
// permanently. Those torn-write rows are returned as kept so the caller
// counts and WARNs on them.
//
// The lookup keys on scope_generations row absence, never on "not the
// scope's active generation" (that races activation) and never on status.
// Every lane's intents carry the scope generation that emitted them,
// including the repo_dependency lane (the cross-repo resolver emits with its
// work item's scope generation), so the probe is meaningful for all three
// selections. Ids that are not scope generations are absent by definition
// and drain when their acceptance is absent.
type AbsentGenerationReader interface {
	// AbsentGenerationIDs returns the subset of generationIDs that have no
	// scope_generations row. It must cost one bounded round trip per call.
	AbsentGenerationIDs(ctx context.Context, generationIDs []string) (map[string]struct{}, error)
}

// AbsentAcceptanceRows recovers the rows FilterAuthoritativeIntents dropped
// because their acceptance row is absent: the rows of the input that are in
// neither the active set nor the stale-id set. Rows without an acceptance
// key are excluded: they can never be selected under any generation state,
// so generation-gone draining must not claim them. The result preserves the
// input order. It performs no I/O, so selections can call it on every widen
// round and only pay for the generation probe on the round that returns.
func AbsentAcceptanceRows(
	all []sharedintent.Row,
	active []sharedintent.Row,
	staleIDs []string,
) []sharedintent.Row {
	if len(all) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(active)+len(staleIDs))
	for _, row := range active {
		seen[row.IntentID] = struct{}{}
	}
	for _, id := range staleIDs {
		seen[id] = struct{}{}
	}
	var absent []sharedintent.Row
	for _, row := range all {
		if _, ok := seen[row.IntentID]; ok {
			continue
		}
		if _, ok := row.AcceptanceKey(); !ok {
			continue
		}
		absent = append(absent, row)
	}
	return absent
}

// SplitAbsentAcceptanceRows separates the absent-acceptance rows whose scope
// generation is gone (drainable) from the torn-write rows whose generation
// still exists (kept) with ONE lookup over the distinct generation ids. A
// batch with no absent rows costs no round trip. A reader without the port
// returns every row as kept, so the caller still counts torn writes but
// drains nothing. Rows with an empty generation id are always kept: nothing
// proves their generation is gone. A lookup error is returned to the caller
// so the selection fails instead of silently dropping or silently keeping
// rows.
func SplitAbsentAcceptanceRows(
	ctx context.Context,
	reader IntentReader,
	absent []sharedintent.Row,
) (drainable, kept []sharedintent.Row, err error) {
	if len(absent) == 0 {
		return nil, nil, nil
	}
	lookup, ok := reader.(AbsentGenerationReader)
	if !ok {
		return nil, absent, nil
	}

	seen := make(map[string]struct{}, len(absent))
	generationIDs := make([]string, 0, len(absent))
	for _, row := range absent {
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
		return nil, absent, nil
	}
	sort.Strings(generationIDs)

	gone, err := lookup.AbsentGenerationIDs(ctx, generationIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("look up absent generations: %w", err)
	}
	if len(gone) == 0 {
		return nil, absent, nil
	}

	kept = make([]sharedintent.Row, 0, len(absent))
	for _, row := range absent {
		if _, isGone := gone[row.GenerationID]; isGone {
			drainable = append(drainable, row)
			continue
		}
		kept = append(kept, row)
	}
	return drainable, kept, nil
}

// DrainAbsentAcceptance marks the generation-gone subset of absent rows
// completed inline and reports what it did. It is the marking path for the
// selections that cannot return drain ids to a downstream mark: the
// code_call and repo_dependency selectors scan repo-wide windows and must
// make progress on their scan-cap error path, where no caller survives to
// mark. (The shared worker's SelectPartitionBatch returns a partial batch
// at the cap instead of erroring, so its drain ids ride the normal
// downstream mark and never use this.)
//
// Marked ids are deleted from absent, so a caller that rescans after a drain
// (cap-continue) never re-marks or double-counts them. Drain ids are sorted
// before the update so concurrent drainers take row locks in one global
// order. A short mark (affected < requested) is a WARN, not an error, for
// the same benign-concurrency reason as the shared worker's batch mark. The
// drained count is metered under acceptance_absent_generation_gone exactly
// once, here; callers must not record it again. Per-unit callers join the
// returned ids into their stale set so the rows count as processed.
//
// alreadyReported carries the torn-write ids an earlier drain this cycle
// already WARNed and counted (#8509): the hit-path drain reports every torn
// row in the scanned window, including older same-unit siblings the
// unit-level drain reloads. Reported rows are excluded from this drain's
// WARN and kept ids so each torn row WARNs and counts once per cycle. The
// exclusion never skips draining: generation-gone rows cannot reappear
// (completed rows are not reloaded), so only the torn side overlaps. A nil
// map reports every kept row.
func DrainAbsentAcceptance(
	ctx context.Context,
	now time.Time,
	tel AcceptanceTelemetry,
	runner string,
	domain string,
	reader IntentReader,
	absent map[string]sharedintent.Row,
	alreadyReported map[string]struct{},
) (drained []string, keptIDs []string, err error) {
	if len(absent) == 0 {
		return nil, nil, nil
	}
	rows := make([]sharedintent.Row, 0, len(absent))
	for _, row := range absent {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].IntentID < rows[j].IntentID })

	drainable, kept, err := SplitAbsentAcceptanceRows(ctx, reader, rows)
	if err != nil {
		return nil, nil, err
	}
	fresh := kept[:0]
	for _, row := range kept {
		if _, ok := alreadyReported[row.IntentID]; !ok {
			fresh = append(fresh, row)
		}
	}
	LogAbsentAcceptanceTornWrites(ctx, tel.Logger, runner, domain, fresh)
	keptIDs = make([]string, 0, len(fresh))
	for _, row := range fresh {
		keptIDs = append(keptIDs, row.IntentID)
	}
	sort.Strings(keptIDs)
	if len(drainable) == 0 {
		return nil, keptIDs, nil
	}
	ids := make([]string, 0, len(drainable))
	for _, row := range drainable {
		ids = append(ids, row.IntentID)
	}
	sort.Strings(ids)
	affected, err := reader.MarkIntentsCompleted(ctx, ids, now)
	if err != nil {
		return nil, nil, fmt.Errorf("mark absent-acceptance drain completed: %w", err)
	}
	if tel.Logger != nil && affected < int64(len(ids)) {
		tel.Logger.WarnContext(
			ctx,
			"shared projection absent-acceptance drain marked fewer intents completed than requested; rows were deleted or completed concurrently",
			slog.String("runner", runner),
			log.Domain(domain),
			slog.Int("requested_intents", len(ids)),
			slog.Int64("completed_intents", affected),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}
	for _, id := range ids {
		delete(absent, id)
	}
	tel.RecordAbsentGenerationGoneIntents(ctx, runner, domain, len(ids))
	return ids, keptIDs, nil
}

// LogAbsentAcceptanceTornWrites emits the one bounded WARN per cycle a
// selection owes when absent-acceptance rows survive because their scope
// generation still exists. These rows are never drained: a retry re-upserts
// the same intent_id without reopening completed_at, so completing one now
// would lose its edge the moment the acceptance row arrives. A steady count
// is the 3 AM signal that a writer is emitting intents without their
// acceptance rows (torn non-transactional write) rather than a generation
// being cascade-deleted. Zero rows log nothing. Only the first row's key is
// sampled so a 10,000-row backlog stays one line.
func LogAbsentAcceptanceTornWrites(
	ctx context.Context,
	logger *slog.Logger,
	runner string,
	domain string,
	kept []sharedintent.Row,
) {
	if len(kept) == 0 || logger == nil {
		return
	}
	key, _ := kept[0].AcceptanceKey()
	logger.WarnContext(
		ctx,
		"shared projection intents have no acceptance row but their scope generation still exists; leaving pending, never draining",
		slog.String("runner", runner),
		log.Domain(domain),
		slog.Int("torn_write_count", len(kept)),
		slog.String("sample_scope_id", key.ScopeID),
		slog.String("sample_acceptance_unit_id", key.AcceptanceUnitID),
		slog.String("sample_source_run_id", key.SourceRunID),
		slog.String("sample_generation_id", kept[0].GenerationID),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
}
