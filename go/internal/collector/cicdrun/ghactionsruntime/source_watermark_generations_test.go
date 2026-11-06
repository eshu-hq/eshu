// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestWatermarkAdvancesPastARetriedGeneration is the activation hazard #10063
// found. The planner mints one work item per plan key, so each generation's
// fence restarts at 1. One retried claim stores fence 2, and a fence-only guard
// then refuses every first-attempt claim after it: the watermark wedges, and
// each truncated window reports a gap that was never there.
func TestWatermarkAdvancesPastARetriedGeneration(t *testing.T) {
	t.Parallel()

	window := func(newest, floor int) RunPage {
		page := RunPage{Truncated: true}
		for id := newest; id >= floor; id-- {
			page.Snapshots = append(page.Snapshots, minimalRunSnapshot(strconv.Itoa(id)))
		}
		return page
	}
	store := runwatermark.NewInMemoryStore()
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client: &sequencedClient{pages: []RunPage{
			window(130, 121),
			window(140, 131),
			window(149, 140),
		}},
		Watermarks: store,
		Targets:    []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	ctx := context.Background()

	retried := watermarkTestClaim("generation-1", 2)
	if _, _, err := source.NextClaimed(ctx, retried); err != nil {
		t.Fatalf("NextClaimed(retried) error = %v", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(ctx, retried); err != nil {
		t.Fatalf("Observe(retried) error = %v", err)
	}

	firstAttempt := watermarkTestClaim("generation-2", 1)
	if _, _, err := source.NextClaimed(ctx, firstAttempt); err != nil {
		t.Fatalf("NextClaimed(first attempt) error = %v", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(ctx, firstAttempt); err != nil {
		t.Fatalf("Observe(first attempt) error = %v, want nil: fences of different generations do not compare", err)
	}
	got, ok, err := store.Load(ctx, watermarkKey(watermarkTestTarget()))
	if err != nil || !ok {
		t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
	}
	if got.LastRunID != "140" {
		t.Fatalf("watermark LastRunID = %q, want 140", got.LastRunID)
	}

	next, ok, err := source.NextClaimed(ctx, watermarkTestClaim("generation-3", 1))
	if err != nil || !ok {
		t.Fatalf("NextClaimed(next) = %v, %v", ok, err)
	}
	for _, envelope := range drainFacts(t, next.Facts) {
		if envelope.FactKind != facts.CICDWarningFactKind {
			continue
		}
		if envelope.Payload["reason"] == "runs_backfill_gap" {
			t.Fatal("the window overlaps the last committed run, but reported runs_backfill_gap")
		}
	}
}
