// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds the #5429 end-to-end NextClaimed wiring tests: real
// multi-cycle claims against an in-memory runwatermark.Store, proving the
// cross-cycle gap is detected (not silently lost), the watermark advances
// and is fenced, and a stale/duplicate claim cannot regress it. See
// run_watermark_test.go for the pure detectRunBackfillGap unit tests.

func watermarkTestClaim(generationID string, fencingToken int64) workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        generationID,
		CurrentFencingToken: fencingToken,
	}
}

func watermarkTestTarget() TargetConfig {
	return TargetConfig{
		ScopeID:             "ci-cd:github-actions:example/repo",
		Repository:          "example/repo",
		Token:               "token",
		AllowedRepositories: []string{"example/repo"},
		MaxRuns:             10,
		MaxJobs:             10,
		MaxArtifacts:        10,
	}
}

// TestClaimedSourceDetectsCrossCycleBackfillGap is the RED->GREEN proof for
// #5429 itself: cycle 1 fetches a 10-run window (newest run 109, not
// truncated -- that is every run that exists at that point). Before cycle
// 2's claim, MORE than max_runs (21) new runs land, so cycle 2's 10-run
// window only covers runs 121-130 and the provider reports more runs exist
// beyond it (Truncated=true). Runs 110-120 were fetched by NEITHER cycle.
// Before this change that loss was completely silent; after this change
// NextClaimed must emit a ci.warning fact with reason=="runs_backfill_gap"
// and record the matching partial-generation metric on cycle 2.
func TestClaimedSourceDetectsCrossCycleBackfillGap(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-backfill-gap-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	client := &sequencedClient{pages: []RunPage{
		{
			Snapshots: []RunSnapshot{
				minimalRunSnapshot("109"), minimalRunSnapshot("108"), minimalRunSnapshot("107"),
				minimalRunSnapshot("106"), minimalRunSnapshot("105"), minimalRunSnapshot("104"),
				minimalRunSnapshot("103"), minimalRunSnapshot("102"), minimalRunSnapshot("101"),
				minimalRunSnapshot("100"),
			},
			Truncated: false,
		},
		{
			// 21 new runs (110-130) landed between cycles; MaxRuns=10 only
			// fetches the newest 10 (121-130). Runs 110-120 are the gap.
			Snapshots: []RunSnapshot{
				minimalRunSnapshot("130"), minimalRunSnapshot("129"), minimalRunSnapshot("128"),
				minimalRunSnapshot("127"), minimalRunSnapshot("126"), minimalRunSnapshot("125"),
				minimalRunSnapshot("124"), minimalRunSnapshot("123"), minimalRunSnapshot("122"),
				minimalRunSnapshot("121"),
			},
			Truncated: true,
		},
	}}

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Instruments:         instruments,
		Watermarks:          runwatermark.NewInMemoryStore(),
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	firstCollected, ok, err := source.NextClaimed(context.Background(), watermarkTestClaim("generation-1", 1))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() [cycle 1] = %v, %v, %v, want nil error and ok=true", firstCollected, ok, err)
	}
	firstEnvelopes := drainFacts(t, firstCollected.Facts)
	if hasFactKind(firstEnvelopes, facts.CICDWarningFactKind) {
		t.Fatal("cycle 1 must not emit a ci.warning fact: it is the first-ever claim with no watermark to compare against")
	}

	secondCollected, ok, err := source.NextClaimed(context.Background(), watermarkTestClaim("generation-2", 2))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() [cycle 2] = %v, %v, %v, want nil error and ok=true", secondCollected, ok, err)
	}
	secondEnvelopes := drainFacts(t, secondCollected.Facts)
	// Cycle 2's window is ALSO full (Truncated=true), so it independently
	// carries a runs_truncated warning in addition to the cross-cycle
	// runs_backfill_gap warning this test proves. Require the specific
	// reason rather than just any ci.warning fact.
	requireWarningReason(t, secondEnvelopes, "runs_backfill_gap")

	rm := collectCICDRunMetrics(t, reader)
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_partial_generations_total", map[string]string{
		telemetry.MetricDimensionProvider: "github_actions",
		telemetry.MetricDimensionReason:   "runs_backfill_gap",
	})
}

// TestClaimedSourceSteadyStateDoesNotFalselyReportGap proves the common,
// non-buggy case does NOT regress: consecutive cycles whose windows overlap
// or abut (fewer than max_runs new runs between them) must never emit a
// runs_backfill_gap warning, even though a watermark is now tracked.
func TestClaimedSourceSteadyStateDoesNotFalselyReportGap(t *testing.T) {
	t.Parallel()

	client := &sequencedClient{pages: []RunPage{
		{Snapshots: []RunSnapshot{minimalRunSnapshot("102"), minimalRunSnapshot("101"), minimalRunSnapshot("100")}},
		// Only 2 new runs since cycle 1 (103, 104); window still covers 100.
		{Snapshots: []RunSnapshot{
			minimalRunSnapshot("104"), minimalRunSnapshot("103"), minimalRunSnapshot("102"),
			minimalRunSnapshot("101"), minimalRunSnapshot("100"),
		}},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Watermarks:          runwatermark.NewInMemoryStore(),
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	if _, _, err := source.NextClaimed(context.Background(), watermarkTestClaim("generation-1", 1)); err != nil {
		t.Fatalf("NextClaimed() [cycle 1] error = %v, want nil", err)
	}
	second, ok, err := source.NextClaimed(context.Background(), watermarkTestClaim("generation-2", 2))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() [cycle 2] = %v, %v, want nil error and ok=true", ok, err)
	}
	if hasFactKind(drainFacts(t, second.Facts), facts.CICDWarningFactKind) {
		t.Fatal("cycle 2 must not emit a ci.warning fact: no gap exists in the steady-state case")
	}
}

// TestClaimedSourcePropagatesStaleWatermarkFence proves the fencing half of
// the concurrency matrix: a claim retried with an OLDER fencing token than
// one the store already recorded (e.g. a superseded worker retrying after a
// newer claim already advanced the watermark) must fail the claim rather
// than silently regress the watermark. NextClaimed must surface the store's
// ErrStaleFence, and the stored watermark must remain unchanged.
func TestClaimedSourcePropagatesStaleWatermarkFence(t *testing.T) {
	t.Parallel()

	store := runwatermark.NewInMemoryStore()
	client := &sequencedClient{pages: []RunPage{
		{Snapshots: []RunSnapshot{minimalRunSnapshot("200")}},
		{Snapshots: []RunSnapshot{minimalRunSnapshot("199")}},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Watermarks:          store,
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	if _, _, err := source.NextClaimed(context.Background(), watermarkTestClaim("generation-2", 5)); err != nil {
		t.Fatalf("NextClaimed() [higher fence first] error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), watermarkTestClaim("generation-1", 3))
	if !errors.Is(err, runwatermark.ErrStaleFence) {
		t.Fatalf("NextClaimed() [stale fence retry] error = %v, want ErrStaleFence", err)
	}

	got, ok, loadErr := store.Load(context.Background(), runwatermark.Key{
		ScopeID: "ci-cd:github-actions:example/repo", Repository: "example/repo",
	})
	if loadErr != nil || !ok {
		t.Fatalf("Load() = %+v, %v, %v", got, ok, loadErr)
	}
	if got.LastRunID != "200" || got.FencingToken != 5 {
		t.Fatalf("stored watermark = %+v, want unchanged at LastRunID=200 FencingToken=5", got)
	}
}

// TestClaimedSourceIdempotentRetryReusesSameFenceWithoutError proves the
// duplicate-delivery case: a retried claim carrying the SAME generation and
// fencing token as the one that already wrote the watermark (e.g. the
// commit succeeded but the claim runner redelivered before acking) must
// succeed, not be treated as a stale fence.
func TestClaimedSourceIdempotentRetryReusesSameFenceWithoutError(t *testing.T) {
	t.Parallel()

	client := &sequencedClient{pages: []RunPage{
		{Snapshots: []RunSnapshot{minimalRunSnapshot("50")}},
		{Snapshots: []RunSnapshot{minimalRunSnapshot("50")}},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Watermarks:          runwatermark.NewInMemoryStore(),
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	claim := watermarkTestClaim("generation-1", 4)
	if _, _, err := source.NextClaimed(context.Background(), claim); err != nil {
		t.Fatalf("NextClaimed() [first delivery] error = %v, want nil", err)
	}
	if _, _, err := source.NextClaimed(context.Background(), claim); err != nil {
		t.Fatalf("NextClaimed() [redelivery, same fence] error = %v, want nil", err)
	}
}

// TestClaimedSourceSkipsGapDetectionWithoutWatermarkStore proves the
// nil-safety contract: SourceConfig.Watermarks left unset must not error or
// change any existing behavior (no gap detection, no ci.warning fact, no
// runs_backfill_gap metric point) -- matching how a nil Checkpoints store
// behaves in awsruntime.
func TestClaimedSourceSkipsGapDetectionWithoutWatermarkStore(t *testing.T) {
	t.Parallel()

	client := &sequencedClient{pages: []RunPage{
		// Truncated=false here so the ONLY way a ci.warning fact could
		// appear is via gap detection (which needs a watermark store, and
		// none is wired) -- isolating this test from the unrelated
		// runs_truncated warning path.
		{Snapshots: []RunSnapshot{minimalRunSnapshot("300")}, Truncated: false},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), watermarkTestClaim("generation-1", 1))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = %v, %v, want nil error and ok=true", ok, err)
	}
	envelopes := drainFacts(t, collected.Facts)
	if hasFactKind(envelopes, facts.CICDWarningFactKind) {
		t.Fatal("must not emit a ci.warning fact when no watermark store is wired")
	}
}

// requireWarningReason fails the test unless one of the envelopes is a
// facts.CICDWarningFactKind whose reason payload equals want. It exists
// because a single generation can legitimately carry more than one
// ci.warning fact (e.g. runs_truncated AND runs_backfill_gap together),
// unlike requireFactKind's first-match semantics.
func requireWarningReason(t *testing.T, envelopes []facts.Envelope, want string) {
	t.Helper()
	var reasons []any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDWarningFactKind {
			continue
		}
		reason := envelope.Payload["reason"]
		if reason == want {
			return
		}
		reasons = append(reasons, reason)
	}
	t.Fatalf("no ci.warning fact with reason %q found; observed reasons = %v", want, reasons)
}

// sequencedClient returns one RunPage per call to FetchRuns, in order,
// simulating successive claim cycles against a live-changing repository.
type sequencedClient struct {
	pages []RunPage
	calls int
}

func (c *sequencedClient) FetchRuns(context.Context, TargetConfig) (RunPage, error) {
	if c.calls >= len(c.pages) {
		return RunPage{}, errors.New("sequencedClient: no more pages configured")
	}
	page := c.pages[c.calls]
	c.calls++
	return page, nil
}

func hasFactKind(envelopes []facts.Envelope, factKind string) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			return true
		}
	}
	return false
}
