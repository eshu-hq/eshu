// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// writeEdgesResult holds what [writeEdgesAndUnroutable] measured and
// selected: the retract/write durations and the upsert-filtered rows the
// caller reports in its [PartitionProcessResult].
type writeEdgesResult struct {
	retractDuration float64
	writeDuration   float64
	upsertRows      []sharedintent.Row
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
func writeEdgesAndUnroutable(
	ctx context.Context,
	cfg PartitionProcessorConfig,
	edgeWriter sharedintent.EdgeWriter,
	unroutableWriter UnroutableWriter,
	retractRows, writeRows []sharedintent.Row,
	evidenceSource string,
) (writeEdgesResult, error) {
	retractStart := time.Now()
	if err := edgeWriter.RetractEdges(ctx, cfg.Domain, retractRows, evidenceSource); err != nil {
		return writeEdgesResult{}, fmt.Errorf("retract edges: %w", err)
	}
	result := writeEdgesResult{retractDuration: time.Since(retractStart).Seconds()}

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
