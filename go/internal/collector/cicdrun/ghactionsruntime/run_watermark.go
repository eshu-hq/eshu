// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"fmt"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds the #5429 cross-cycle run-watermark gap detection: the
// window every claim fetches is bounded and stateless (source.go:84-90's
// design comment), so when more than max_runs runs land between two claim
// cycles, only the newest max_runs are fetched and the older new-runs
// between the last-observed run and the window floor are never fetched --
// silently, before this change. The watermark stored via s.watermarks turns
// that silent loss into a detected, reported gap.

// watermarkKey returns the run-watermark identity for target. One target
// (one configured repository under one scope) maps to exactly one watermark
// row.
func watermarkKey(target TargetConfig) runwatermark.Key {
	return runwatermark.Key{ScopeID: target.ScopeID, Repository: target.Repository}
}

// loadWatermark returns the stored watermark for target. It is nil-safe:
// when s.watermarks is not wired (Watermarks was left unset in SourceConfig),
// it returns hasWatermark=false without error, so gap detection is simply
// skipped rather than failing every claim -- the same optionality
// awsruntime.ClaimedSource.Checkpoints uses.
func (s ClaimedSource) loadWatermark(ctx context.Context, target TargetConfig) (runwatermark.Watermark, bool, error) {
	if s.watermarks == nil {
		return runwatermark.Watermark{}, false, nil
	}
	value, ok, err := s.watermarks.Load(ctx, watermarkKey(target))
	if err != nil {
		return runwatermark.Watermark{}, false, fmt.Errorf("load ci/cd run watermark: %w", err)
	}
	return value, ok, nil
}

// saveWatermark durably persists the newest run ID one claim cycle observed
// at the source, fenced by the claim's generation and fencing token so a
// superseded claim retry cannot regress the watermark past a newer claim's
// progress. It is nil-safe (see loadWatermark) and a no-op when the fetched
// window was empty.
//
// saveWatermark itself does not know or care whether the commit describing
// that window has happened -- its ONLY caller,
// ClaimedSource.ObserveClaimedGenerationCommitted
// (source_commit_observer.go), is what guarantees this only runs once that
// commit is durable. collector.ClaimedService invokes that observer exactly
// once per claim cycle, immediately after the commit succeeds (#5429).
//
// NextClaimed must NEVER call saveWatermark directly. Before #5429 it did:
// saving here on NextClaimed's own success path advanced the watermark
// before the commit it was meant to describe was known to have landed, so a
// retryable commit failure followed by a retry of the SAME work item
// compared the retry's re-fetched window against an ALREADY-ADVANCED
// watermark and silently stopped re-detecting the very gap the watermark
// exists to catch. NextClaimed now only stashes the observed newest run ID
// (pending_watermark.go's pendingWatermarks) for this method to pick up
// later.
func (s ClaimedSource) saveWatermark(
	ctx context.Context,
	item workflow.WorkItem,
	target TargetConfig,
	newestRunID string,
) error {
	if s.watermarks == nil || newestRunID == "" {
		return nil
	}
	err := s.watermarks.Save(ctx, runwatermark.Watermark{
		Key:          watermarkKey(target),
		LastRunID:    newestRunID,
		GenerationID: item.GenerationID,
		FencingToken: item.CurrentFencingToken,
	})
	if err != nil {
		return fmt.Errorf("save ci/cd run watermark: %w", err)
	}
	return nil
}

// detectRunBackfillGap reports whether runs may have been silently skipped
// between a prior claim cycle's watermark and this cycle's fetched window.
//
// It fires only when ALL of the following hold:
//   - a prior watermark exists (hasWatermark) -- nothing to compare against
//     on a target's first-ever claim;
//   - the fetched page indicates more runs exist beyond the window
//     (page.Truncated, the same total_count/full-page signal
//     runsPageTruncated already computes for the within-cycle
//     runs_truncated warning); and
//   - the window's OLDEST fetched run is strictly newer than the watermark,
//     meaning every run between them was never fetched by either cycle.
//
// An untruncated page can never be a gap: it means every run that currently
// exists was fetched, regardless of how the window floor compares to a
// stale watermark.
func detectRunBackfillGap(page RunPage, watermark runwatermark.Watermark, hasWatermark bool) (bool, error) {
	if !hasWatermark || !page.Truncated || len(page.Snapshots) == 0 {
		return false, nil
	}
	floorID, err := windowFloorRunID(page)
	if err != nil {
		return false, fmt.Errorf("determine ci/cd run window floor: %w", err)
	}
	floor, err := parseRunID(floorID)
	if err != nil {
		return false, fmt.Errorf("parse ci/cd run window floor %q: %w", floorID, err)
	}
	watermarkRunID, err := parseRunID(watermark.LastRunID)
	if err != nil {
		return false, fmt.Errorf("parse ci/cd run watermark last_run_id %q: %w", watermark.LastRunID, err)
	}
	return floor > watermarkRunID, nil
}

// windowFloorRunID returns the run ID of the OLDEST snapshot in the fetched
// window. GitHub returns runs newest-first, so the oldest run is the last
// snapshot.
func windowFloorRunID(page RunPage) (string, error) {
	return snapshotRunID(page.Snapshots[len(page.Snapshots)-1])
}

// windowNewestRunID returns the run ID of the NEWEST snapshot in the fetched
// window (the first snapshot, since GitHub returns runs newest-first), or
// "" when the window is empty.
func windowNewestRunID(page RunPage) (string, error) {
	if len(page.Snapshots) == 0 {
		return "", nil
	}
	return snapshotRunID(page.Snapshots[0])
}

func snapshotRunID(snapshot RunSnapshot) (string, error) {
	id, err := numericProviderID(snapshot.Run["id"])
	if err != nil {
		return "", fmt.Errorf("github actions run.id: %w", err)
	}
	return id, nil
}

func parseRunID(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
}

// attachBackfillGapWarning returns snapshots unchanged when no gap was
// detected. When a gap was detected, it returns a copy with a
// runs_backfill_gap warning appended to the newest run's Warnings,
// mirroring attachRunsTruncatedWarning's non-mutating copy-and-append shape
// so the two warnings compose safely regardless of call order.
func attachBackfillGapWarning(snapshots []RunSnapshot, gapDetected bool) []RunSnapshot {
	if !gapDetected || len(snapshots) == 0 {
		return snapshots
	}
	out := append([]RunSnapshot(nil), snapshots...)
	latest := out[0]
	latest.Warnings = append(append([]map[string]any(nil), latest.Warnings...), map[string]any{
		"reason": "runs_backfill_gap",
		"message": "a prior claim cycle's watermark is older than this cycle's fetched run window, " +
			"and the provider reports more runs exist beyond the window; runs between them were not " +
			"fetched by either cycle",
	})
	out[0] = latest
	return out
}
