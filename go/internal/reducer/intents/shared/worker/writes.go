// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// writeEdgesResult holds what [writeEdgesAndUnroutable] measured and
// selected: the retract/write durations, the upsert-filtered rows the caller
// reports in its [PartitionProcessResult], and the poisoned rows it
// dead-lettered instead of retrying.
type writeEdgesResult struct {
	retractDuration float64
	writeDuration   float64
	upsertRows      []sharedintent.Row
	// deadLettered holds the retract rows quarantined as unusable-delta this
	// cycle. The caller marks them completed alongside the projected batch.
	deadLettered []sharedintent.Row
}

// writeEdgesAndUnroutable retracts, writes, and persists the unroutable
// report for one partition cycle.
//
// Persisting the rows that produced no edge happens BEFORE
// [ProcessPartitionOnce] completes anything. The order is the whole point:
// completion is permanent (the durable upsert never reopens a completed
// row), so after MarkIntentsCompleted nothing else records that these rows
// produced nothing. Failing the cycle here is safe -- the retract, the write
// and this upsert are all idempotent, so the batch simply re-runs -- and it
// is the only way to avoid reintroducing the silent loss in the
// persist-failure window (#5984).
//
// A retract that fails closed on an unusable delta scope (#10069) takes the
// quarantine branch instead of failing the cycle: the poisoned rows are
// recorded as unroutable and completed, and the usable remainder retracts in
// the same cycle. The rejection is payload-deterministic (the same row fails
// identically on every poll), so retrying it is head-of-line blocking, not
// recovery. Any other retract error still fails the cycle and retries.
func writeEdgesAndUnroutable(
	ctx context.Context,
	cfg PartitionProcessorConfig,
	edgeWriter sharedintent.EdgeWriter,
	unroutableWriter UnroutableWriter,
	retractRows, writeRows []sharedintent.Row,
	evidenceSource string,
	now time.Time,
) (writeEdgesResult, error) {
	result := writeEdgesResult{}
	retractStart := time.Now()
	if err := edgeWriter.RetractEdges(ctx, cfg.Domain, retractRows, evidenceSource); err != nil {
		// A fail-closed unusable-delta rejection quarantines the poisoned
		// rows and retracts the usable remainder in the same cycle (#10069);
		// any other error fails the cycle and retries.
		deadLettered, quarantineErr := quarantineUnusableDeltaScope(
			ctx, cfg, edgeWriter, unroutableWriter,
			retractRows, evidenceSource, now, err,
		)
		if quarantineErr != nil {
			return writeEdgesResult{}, quarantineErr
		}
		result.deadLettered = deadLettered
	}
	result.retractDuration = time.Since(retractStart).Seconds()

	result.upsertRows = sharedintent.FilterUpsertRows(writeRows)
	writeStart := time.Now()
	writeReport, err := edgeWriter.WriteEdges(ctx, cfg.Domain, result.upsertRows, evidenceSource)
	if err != nil {
		return writeEdgesResult{}, fmt.Errorf("write edges: %w", err)
	}
	result.writeDuration = time.Since(writeStart).Seconds()

	if len(writeReport.UnroutableRows) > 0 && unroutableWriter != nil {
		if err := unroutableWriter.WriteUnroutableIntents(ctx, writeReport.UnroutableRows); err != nil {
			return writeEdgesResult{}, fmt.Errorf("record unroutable intents: %w", err)
		}
	}
	return result, nil
}

// quarantineUnusableDeltaScope handles a retract that failed closed on an
// unusable delta scope (#10069). The poisoned rows are recorded as unroutable
// and returned for completion, and the usable remainder retracts before the
// caller proceeds to the write phase, so one poisoned refresh can neither
// fail its partition forever nor fence its siblings' rows behind it.
//
// Every exit preserves the fail-closed property the rejection exists for:
//   - a non-deterministic error (anything but the typed rejection) fails the
//     cycle and retries, exactly as before;
//   - a typed rejection with no poisoned row under the shared predicate fails
//     the cycle: the split cannot explain the failure, so completing anything
//     would be silent loss;
//   - a nil unroutable sink fails the cycle: completing the poisoned row
//     without the durable record is the silent loss #5984 fixed;
//   - a failing unroutable write fails the cycle before anything completes,
//     so the batch re-runs and re-records idempotently.
//
// The unroutable insert is ON CONFLICT DO NOTHING keyed on the intent id, and
// completion is permanent, so a crash between the record and the mark
// re-runs the whole branch and converges: re-record is a no-op, re-mark is
// idempotent, and the usable rows' retract and write are idempotent.
func quarantineUnusableDeltaScope(
	ctx context.Context,
	cfg PartitionProcessorConfig,
	edgeWriter sharedintent.EdgeWriter,
	unroutableWriter UnroutableWriter,
	retractRows []sharedintent.Row,
	evidenceSource string,
	now time.Time,
	retractErr error,
) (deadLettered []sharedintent.Row, err error) {
	if !sharedintent.IsUnusableDeltaScopeError(retractErr) {
		return nil, fmt.Errorf("retract edges: %w", retractErr)
	}
	usable, unusable := sharedintent.SplitUnusableDeltaScopeRows(retractRows)
	if len(unusable) == 0 {
		return nil, fmt.Errorf("retract edges: %w", retractErr)
	}
	if unroutableWriter == nil {
		return nil, fmt.Errorf("retract edges: %w", retractErr)
	}
	unroutable := make([]sharedintent.UnroutableRow, 0, len(unusable))
	for _, row := range unusable {
		unroutable = append(unroutable, sharedintent.UnroutableRowForUnusableDeltaScope(row, evidenceSource, now))
	}
	if err := unroutableWriter.WriteUnroutableIntents(ctx, unroutable); err != nil {
		return nil, fmt.Errorf("record unusable delta scope intents: %w", err)
	}
	if err := edgeWriter.RetractEdges(ctx, cfg.Domain, usable, evidenceSource); err != nil {
		return nil, fmt.Errorf("retract edges after quarantining unusable delta scope: %w", err)
	}
	if cfg.Logger != nil {
		sample := unusable[0]
		cfg.Logger.WarnContext(
			ctx,
			"shared projection quarantined refresh intents with an unusable delta scope; completed without retracting",
			log.Domain(cfg.Domain),
			slog.Int("quarantined_intents", len(unusable)),
			slog.String("sample_intent_id", sample.IntentID),
			slog.String("sample_repository_id", sharedintent.RowRepoID(sample)),
			slog.String("unroutable_reason", sharedintent.UnusableDeltaScopeReason),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}
	return unusable, nil
}

// retractWriteRows is the row split [resolveRetractWriteRows] computes for
// one partition cycle: the rows to retract, the rows to write, the rows to
// mark completed, and the count of per-edge rows the repo-wide-retract fence
// deferred this cycle.
type retractWriteRows struct {
	retractRows         []sharedintent.Row
	writeRows           []sharedintent.Row
	completedLatestRows []sharedintent.Row
	deferred            int
}

// resolveRetractWriteRows splits one partition batch into the rows
// [ProcessPartitionOnce] retracts, writes, and marks completed.
//
// Retract runs over the ready AND terminal rows: retraction is repo-scoped,
// so a repo whose only handles_route rows are terminal (every endpoint
// absent) must still contribute its repo_id to clear a stale edge from a
// prior generation when the endpoint has since vanished. Writes re-add the
// ready rows only.
//
// Repo-wide-retract domains (#2898/#2910): when a fence is wired, the single
// repo-wide retract is owned by the per-repo refresh intent and per-edge rows
// write only after that retract has committed. This removes the
// per-partition repo-wide retract that wipes sibling partitions' edges.
// Other domains keep the retract-then-write-everything behavior
// byte-identical.
//
// The nil-fence path does NOT, for the four domains #6166 narrowed
// (inheritance, rationale, sql_relationships, shell_exec). With no fence
// wired this block is skipped and every latest row -- including an unmarked
// per-edge row -- goes straight to RetractEdges, which now binds only the
// refresh-marked rows. A per-edge-only partition therefore issues no
// whole-repo DELETE where it previously issued one. Production always wires
// the fence (cmd/reducer/main.go), so this is a test-only shape; it is
// pinned by TestRetractEdgesNilFenceShapeSkipsWholeScopeDelete in
// go/internal/storage/cypher so the divergence stays deliberate, and
// EdgeWriter.logWholeScopeRetractSkipped warns when it happens.
func resolveRetractWriteRows(
	ctx context.Context,
	cfg PartitionProcessorConfig,
	batch PartitionBatchResult,
	refreshFence RefreshFenceLookup,
	firstProjection FirstProjectionLookup,
) (retractWriteRows, error) {
	retractRows := batch.LatestRows
	if len(batch.TerminalRows) > 0 {
		retractRows = make([]sharedintent.Row, 0, len(batch.LatestRows)+len(batch.TerminalRows))
		retractRows = append(retractRows, batch.LatestRows...)
		retractRows = append(retractRows, batch.TerminalRows...)
	}
	result := retractWriteRows{
		retractRows:         retractRows,
		writeRows:           batch.LatestRows,
		completedLatestRows: batch.LatestRows,
	}

	if refreshFence != nil && sharedintent.DomainHasRepoWideRetract(cfg.Domain) {
		plan, planErr := PlanRepoWideRetractWork(ctx, cfg.Domain, batch.LatestRows, refreshFence, firstProjection, cfg.Logger)
		if planErr != nil {
			return retractWriteRows{}, planErr
		}
		result.retractRows = plan.RetractRows
		result.writeRows = plan.WriteRows
		result.completedLatestRows = plan.CompletedRows
		result.deferred = plan.Deferred
	}
	return result, nil
}
