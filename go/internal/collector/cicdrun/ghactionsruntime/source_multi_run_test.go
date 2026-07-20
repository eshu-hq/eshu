// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds the #5338 PR B multi-run collection tests: real multi-run
// windows, MaxRuns bounding plus the runs_truncated warning/metric, cross-cycle
// idempotency, and the default MaxRuns fill. Split from source_test.go to keep
// both files under the repo's 500-line cap.

// TestClaimedSourceCollectsEveryRunInTheWindow is the RED->GREEN proof for
// the #5338 bug: FetchLatestRun used to keep only runs[0] and silently
// discard runs[1:MaxRuns] every claim cycle even though the provider page was
// already fetched over the wire. With three runs in the fetched window and
// MaxRuns=10, NextClaimed must now emit three independently keyed
// facts.CICDRunFactKind envelopes (one per run_id), not one.
func TestClaimedSourceCollectsEveryRunInTheWindow(t *testing.T) {
	t.Parallel()

	client := fakeClient{page: RunPage{Snapshots: []RunSnapshot{
		minimalRunSnapshot("3003"),
		minimalRunSnapshot("2002"),
		minimalRunSnapshot("1001"),
	}}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             10,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envelopes := drainFacts(t, collected.Facts)
	runIDs := cicdRunFactRunIDs(envelopes)
	wantRunIDs := map[string]bool{"3003": true, "2002": true, "1001": true}
	if len(runIDs) != len(wantRunIDs) {
		t.Fatalf("ci.run run_ids = %v, want exactly %v (one fact-set per run, not just runs[0])", runIDs, wantRunIDs)
	}
	for id := range wantRunIDs {
		if !runIDs[id] {
			t.Fatalf("ci.run run_ids = %v, missing run_id %q", runIDs, id)
		}
	}
}

// TestClaimedSourceBoundsToMaxRunsAndEmitsRunsTruncatedWarning covers the
// boundary case: 3 runs are available but MaxRuns=2 bounds the window, and
// the fakeClient signals Truncated=true (mirroring what FetchRuns computes
// from GitHub's total_count/full-page heuristic). NextClaimed must emit
// exactly the 2 windowed runs plus a ci.warning fact carrying
// reason=="runs_truncated", and record the matching
// eshu_dp_ci_cd_run_partial_generations_total{reason="runs_truncated"} point.
func TestClaimedSourceBoundsToMaxRunsAndEmitsRunsTruncatedWarning(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-truncated-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	client := fakeClient{page: RunPage{
		Snapshots: []RunSnapshot{
			minimalRunSnapshot("2002"),
			minimalRunSnapshot("1001"),
		},
		Truncated: true,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Instruments:         instruments,
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             2,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envelopes := drainFacts(t, collected.Facts)
	runIDs := cicdRunFactRunIDs(envelopes)
	if len(runIDs) != 2 {
		t.Fatalf("ci.run run_ids = %v, want exactly 2 (bounded by MaxRuns=2)", runIDs)
	}
	warning := requireFactKind(t, envelopes, facts.CICDWarningFactKind)
	if got, want := warning.Payload["reason"], "runs_truncated"; got != want {
		t.Fatalf("ci.warning reason = %#v, want %#v", got, want)
	}

	rm := collectCICDRunMetrics(t, reader)
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_partial_generations_total", map[string]string{
		telemetry.MetricDimensionProvider: "github_actions",
		telemetry.MetricDimensionReason:   "runs_truncated",
	})
}

// TestClaimedSourceReemittingTheSameRunsWindowIsIdempotent proves the
// stateless-idempotent design (#5338 PR B): with no persistent
// watermark/cursor, re-fetching the same run window on a later claim cycle
// (a different generation_id, as production always assigns) must still
// produce the same StableFactKey per run, since that key is derived from
// provider+run_id+run_attempt, not generation_id. That StableFactKey
// stability is what makes re-emitting the same window every cycle an
// idempotent upsert at projection instead of duplicating graph truth.
func TestClaimedSourceReemittingTheSameRunsWindowIsIdempotent(t *testing.T) {
	t.Parallel()

	client := fakeClient{page: RunPage{Snapshots: []RunSnapshot{
		minimalRunSnapshot("2002"),
		minimalRunSnapshot("1001"),
	}}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             10,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	first, _, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() [cycle 1] error = %v, want nil", err)
	}
	second, _, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-2",
		CurrentFencingToken: 8,
	})
	if err != nil {
		t.Fatalf("NextClaimed() [cycle 2] error = %v, want nil", err)
	}

	firstKeys := cicdRunFactStableKeysByRunID(drainFacts(t, first.Facts))
	secondKeys := cicdRunFactStableKeysByRunID(drainFacts(t, second.Facts))
	if len(firstKeys) != 2 || len(secondKeys) != 2 {
		t.Fatalf("stable keys per cycle = %d, %d, want 2 and 2", len(firstKeys), len(secondKeys))
	}
	for runID, wantKey := range firstKeys {
		gotKey, ok := secondKeys[runID]
		if !ok {
			t.Fatalf("cycle 2 is missing run_id %q present in cycle 1", runID)
		}
		if gotKey != wantKey {
			t.Fatalf("StableFactKey for run_id %q changed across cycles: cycle1=%q cycle2=%q (not upsert-stable)", runID, wantKey, gotKey)
		}
	}
}

// TestNewClaimedSourceDefaultsMaxRunsToTenWhenUnset proves the #5338 default:
// an omitted/zero max_runs must resolve to 10 (the DEFAULT bound), not fail
// validation or silently stay at the old effective single-run behavior. The
// recordingClient captures the exact TargetConfig FetchRuns receives so the
// resolved MaxRuns is observable.
func TestNewClaimedSourceDefaultsMaxRunsToTenWhenUnset(t *testing.T) {
	t.Parallel()

	client := &recordingClient{page: RunPage{Snapshots: []RunSnapshot{minimalRunSnapshot("1001")}}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	if _, _, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	}); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if got, want := client.gotTarget.MaxRuns, defaultMaxRuns; got != want {
		t.Fatalf("resolved MaxRuns = %d, want default %d", got, want)
	}
	if got, want := defaultMaxRuns, 10; got != want {
		t.Fatalf("defaultMaxRuns = %d, want %d", got, want)
	}
}

// TestNewClaimedSourceRejectsNegativeMaxRuns proves the default-fill only
// applies to an omitted (zero) max_runs; an explicit negative value must
// still be rejected rather than silently defaulted.
func TestNewClaimedSourceRejectsNegativeMaxRuns(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{},
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             -1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want negative max_runs rejection")
	}
}

// TestClaimedSourceDeduplicatesWorkflowIdentityFactsAcrossRuns is the
// RED->GREEN proof for the codex P2 review finding: with multi-run
// collection, WORKFLOW-level facts (ci.pipeline_definition, whose FactID is
// keyed by provider+repository+workflow identity, NOT run id) were emitted
// once per run, so two runs of the SAME workflow in the window streamed the
// identical ci.pipeline_definition FactID twice per generation. The dedup in
// buildRunEnvelopes must collapse that workflow-identity fact to exactly one
// while every run-level fact (keyed by run_id:run_attempt) still appears per
// run. The emitted-fact count metric must reflect the deduped total.
func TestClaimedSourceDeduplicatesWorkflowIdentityFactsAcrossRuns(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-dedup-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	// Two runs of the SAME workflow (identical workflow id/name/path and
	// repository); only the run id differs.
	client := fakeClient{page: RunPage{Snapshots: []RunSnapshot{
		sameWorkflowRunSnapshot("2002"),
		sameWorkflowRunSnapshot("1001"),
	}}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Instruments:         instruments,
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             10,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envelopes := drainFacts(t, collected.Facts)

	// The workflow-identity fact collapses to exactly one across both runs.
	if got := factKindCount(envelopes, facts.CICDPipelineDefinitionFactKind); got != 1 {
		t.Fatalf("ci.pipeline_definition count = %d, want exactly 1 (deduped across 2 same-workflow runs)", got)
	}
	// No duplicate FactIDs remain anywhere in the emitted slice.
	if dupe := firstDuplicateFactID(envelopes); dupe != "" {
		t.Fatalf("duplicate FactID %q survived dedup", dupe)
	}
	// Run-level facts are untouched: one ci.run per run id.
	runIDs := cicdRunFactRunIDs(envelopes)
	if len(runIDs) != 2 || !runIDs["2002"] || !runIDs["1001"] {
		t.Fatalf("ci.run run_ids = %v, want exactly {2002, 1001} (run-level facts unaffected by dedup)", runIDs)
	}

	// The emitted-fact count metric reflects the deduped total, not the
	// pre-dedup inflated count.
	rm := collectCICDRunMetrics(t, reader)
	if got := cicdRunFactsEmittedTotal(t, rm); got != len(envelopes) {
		t.Fatalf("eshu_dp_ci_cd_run_facts_emitted_total = %d, want deduped emitted-fact count %d", got, len(envelopes))
	}
}
