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

// recordingClient is a fakeClient variant that captures the exact
// TargetConfig FetchRuns received, so a test can observe the value
// validateTarget resolved (e.g. a defaulted MaxRuns) without re-implementing
// validateTarget's own logic.
type recordingClient struct {
	page      RunPage
	err       error
	gotTarget TargetConfig
}

func (c *recordingClient) FetchRuns(_ context.Context, target TargetConfig) (RunPage, error) {
	c.gotTarget = target
	return c.page, c.err
}

// minimalRunSnapshot builds the smallest RunSnapshot the cicdrun fixture
// normalizer accepts: a run ID plus the repository/commit anchors that keep
// GitHubActionsFixtureEnvelopes from also emitting a
// run_missing_repository_or_commit warning envelope, which would otherwise
// pollute run-count/warning assertions in the multi-run tests above.
func minimalRunSnapshot(runID string) RunSnapshot {
	return RunSnapshot{
		Run: map[string]any{
			"id":       runID,
			"head_sha": "0123456789abcdef0123456789abcdef01234567",
			"repository": map[string]any{
				"full_name": "example/repo",
			},
		},
	}
}

// cicdRunFactRunIDs collects the distinct run_id payload values across every
// facts.CICDRunFactKind envelope, so a test can assert exactly which runs a
// claim cycle emitted independently of envelope ordering.
func cicdRunFactRunIDs(envelopes []facts.Envelope) map[string]bool {
	out := map[string]bool{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		if runID, ok := envelope.Payload["run_id"].(string); ok {
			out[runID] = true
		}
	}
	return out
}

// cicdRunFactStableKeysByRunID maps each facts.CICDRunFactKind envelope's
// run_id payload value to its StableFactKey, so a test can assert the key
// stays identical across two separate claim cycles for the same run.
func cicdRunFactStableKeysByRunID(envelopes []facts.Envelope) map[string]string {
	out := map[string]string{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		if runID, ok := envelope.Payload["run_id"].(string); ok {
			out[runID] = envelope.StableFactKey
		}
	}
	return out
}
