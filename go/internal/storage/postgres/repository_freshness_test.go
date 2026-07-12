// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReadRepositoryFreshnessFullyBuiltGeneration verifies the happy path: a
// resolved scope/generation with every stage drained and no shared-pending
// domains scans into a snapshot ready to render "current".
func TestReadRepositoryFreshnessFullyBuiltGeneration(t *testing.T) {
	t.Parallel()

	activatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	observedAt := activatedAt.Add(-2 * time.Minute)

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, activatedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{{"reducer", "succeeded", int64(3)}, {"projector", "succeeded", int64(1)}}},
		{rows: [][]any{}},
		{rows: [][]any{}},
	}}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if !snapshot.Resolved || !snapshot.HasGeneration {
		t.Fatalf("snapshot = %+v, want Resolved=true HasGeneration=true", snapshot)
	}
	if snapshot.ScopeID != "scope-1" || snapshot.Generation.ID != "gen-1" {
		t.Fatalf("snapshot scope/generation = %q/%q, want scope-1/gen-1", snapshot.ScopeID, snapshot.Generation.ID)
	}
	if snapshot.ObservedCommit != "abc123" {
		t.Fatalf("ObservedCommit = %q, want abc123", snapshot.ObservedCommit)
	}
	if !snapshot.Generation.ActivatedAt.Equal(activatedAt) {
		t.Fatalf("ActivatedAt = %v, want %v", snapshot.Generation.ActivatedAt, activatedAt)
	}
	if !snapshot.Stages.Collected || !snapshot.Stages.Reduced || !snapshot.Stages.Projected || !snapshot.Stages.Materialized {
		t.Fatalf("Stages = %+v, want all true", snapshot.Stages)
	}
	if snapshot.SharedEnrichment.Pending {
		t.Fatalf("SharedEnrichment.Pending = true, want false")
	}
	if snapshot.UnobservedPush != nil {
		t.Fatalf("UnobservedPush = %+v, want nil", snapshot.UnobservedPush)
	}
}

// TestReadRepositoryFreshnessOutstandingReducerWork verifies a non-succeeded
// reducer row flips Stages.Reduced to false and is preserved in Outstanding
// for the caller's outstanding_by_stage rendering.
func TestReadRepositoryFreshnessOutstandingReducerWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", true, nil, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{{"reducer", "pending", int64(2)}}},
		{rows: [][]any{}},
		{rows: [][]any{}},
	}}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.Stages.Reduced {
		t.Fatal("Stages.Reduced = true, want false for an outstanding reducer row")
	}
	if !snapshot.Stages.Projected {
		t.Fatal("Stages.Projected = false, want true (no projector rows outstanding)")
	}
	if !snapshot.Generation.IsDelta {
		t.Fatal("IsDelta = false, want true")
	}
	if !snapshot.Generation.ActivatedAt.IsZero() {
		t.Fatalf("ActivatedAt = %v, want zero for a pending (never activated) generation", snapshot.Generation.ActivatedAt)
	}
	if len(snapshot.Outstanding) != 1 || snapshot.Outstanding[0].Stage != "reducer" || snapshot.Outstanding[0].Count != 2 {
		t.Fatalf("Outstanding = %+v, want one reducer/pending/2 row", snapshot.Outstanding)
	}
}

// TestReadRepositoryFreshnessSharedPendingOnly verifies shared-enrichment
// backlog is a separate axis from Stages: own stages fully drained but a
// cross-repo domain is pending sets SharedEnrichment.Pending and
// Stages.Materialized=false without touching Reduced/Projected.
func TestReadRepositoryFreshnessSharedPendingOnly(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, observedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{{"reducer", "succeeded", int64(1)}, {"projector", "succeeded", int64(1)}}},
		{rows: [][]any{{"deployment_mapping", int64(4)}}},
		{rows: [][]any{}},
	}}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if !snapshot.Stages.Reduced || !snapshot.Stages.Projected {
		t.Fatalf("Stages = %+v, want Reduced/Projected true", snapshot.Stages)
	}
	if snapshot.Stages.Materialized {
		t.Fatal("Stages.Materialized = true, want false when a shared domain is pending")
	}
	if !snapshot.SharedEnrichment.Pending {
		t.Fatal("SharedEnrichment.Pending = false, want true")
	}
	if len(snapshot.SharedEnrichment.PendingDomains) != 1 || snapshot.SharedEnrichment.PendingDomains[0].Domain != "deployment_mapping" {
		t.Fatalf("PendingDomains = %+v, want one deployment_mapping/4 row", snapshot.SharedEnrichment.PendingDomains)
	}
}

// TestReadRepositoryFreshnessUnobservedPush verifies a queued webhook trigger
// whose target_sha differs from the observed commit surfaces as
// UnobservedPush, and that a trigger matching the observed commit does not.
func TestReadRepositoryFreshnessUnobservedPush(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	receivedAt := observedAt.Add(30 * time.Second)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, observedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{{"reducer", "succeeded", int64(1)}, {"projector", "succeeded", int64(1)}}},
		{rows: [][]any{}},
		{rows: [][]any{{"def456", "refs/heads/main", receivedAt}}},
	}}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.UnobservedPush == nil {
		t.Fatal("UnobservedPush = nil, want a push evidencing a newer target_sha")
	}
	if snapshot.UnobservedPush.TargetSHA != "def456" || snapshot.UnobservedPush.Ref != "refs/heads/main" {
		t.Fatalf("UnobservedPush = %+v, want target_sha=def456 ref=refs/heads/main", snapshot.UnobservedPush)
	}
	if !snapshot.UnobservedPush.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("ReceivedAt = %v, want %v", snapshot.UnobservedPush.ReceivedAt, receivedAt)
	}
}

// TestReadRepositoryFreshnessWebhookMatchingObservedCommitIsNotUnobserved
// verifies a queued trigger whose target_sha already matches the observed
// commit (for example a redelivered webhook after the push was built) does
// not report a false unobserved push.
func TestReadRepositoryFreshnessWebhookMatchingObservedCommitIsNotUnobserved(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, observedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{{"reducer", "succeeded", int64(1)}, {"projector", "succeeded", int64(1)}}},
		{rows: [][]any{}},
		{rows: [][]any{{"abc123", "refs/heads/main", observedAt}}},
	}}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.UnobservedPush != nil {
		t.Fatalf("UnobservedPush = %+v, want nil when target_sha matches the observed commit", snapshot.UnobservedPush)
	}
}

// TestReadRepositoryFreshnessUnresolvedRepositoryReturnsNoRowsWithoutError
// verifies a repo_id that resolves to no scope produces an honest
// Resolved=false snapshot rather than an error, and short-circuits before any
// downstream query runs.
func TestReadRepositoryFreshnessUnresolvedRepositoryReturnsNoRowsWithoutError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	store := NewRepositoryFreshnessStore(queryer)

	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-unknown")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.Resolved {
		t.Fatal("Resolved = true, want false for an unresolvable repository")
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queryer received %d queries, want exactly 1 (short-circuit after resolve)", len(queryer.queries))
	}
}

// TestReadRepositoryFreshnessResolvedScopeWithNoGenerationRow verifies a
// resolved scope whose generation row is (unexpectedly) missing still
// returns an honest HasGeneration=false snapshot instead of an error, and
// stops before the stage/shared/webhook reads.
func TestReadRepositoryFreshnessResolvedScopeWithNoGenerationRow(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{}},
	}}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if !snapshot.Resolved {
		t.Fatal("Resolved = false, want true")
	}
	if snapshot.HasGeneration {
		t.Fatal("HasGeneration = true, want false when the generation row is missing")
	}
	if len(queryer.queries) != 2 {
		t.Fatalf("queryer received %d queries, want exactly 2 (short-circuit after generation lookup)", len(queryer.queries))
	}
}

// TestReadRepositoryFreshnessBlankRepoIDShortCircuits verifies an empty
// repository id never reaches Postgres.
func TestReadRepositoryFreshnessBlankRepoIDShortCircuits(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	store := NewRepositoryFreshnessStore(queryer)

	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "   ")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.Resolved {
		t.Fatal("Resolved = true, want false for a blank repo id")
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("queryer received %d queries, want 0 for a blank repo id", len(queryer.queries))
	}
}

// TestReadRepositoryFreshnessRequiresQueryer verifies a zero-value store (no
// queryer wired) fails loudly instead of silently returning an empty
// snapshot.
func TestReadRepositoryFreshnessRequiresQueryer(t *testing.T) {
	t.Parallel()

	var store RepositoryFreshnessStore
	if _, err := store.ReadRepositoryFreshness(context.Background(), "repo-1"); err == nil {
		t.Fatal("ReadRepositoryFreshness() error = nil, want error for a nil queryer")
	}
}

// TestReadRepositoryFreshnessPropagatesQueryError verifies a Postgres query
// failure surfaces as a wrapped error rather than an empty, misleadingly
// successful result.
func TestReadRepositoryFreshnessPropagatesQueryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection reset")
	queryer := &fakeQueryer{responses: []fakeRows{{err: wantErr}}}
	store := NewRepositoryFreshnessStore(queryer)

	_, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err == nil {
		t.Fatal("ReadRepositoryFreshness() error = nil, want wrapped queryer error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want wrapping %v", err, wantErr)
	}
}

// TestNewInstrumentedRepositoryFreshnessStoreWiresInstruments verifies the
// instrumented constructor assigns the caller's Instruments, matching
// NewInstrumentedLiveActivityStore's convention.
func TestNewInstrumentedRepositoryFreshnessStoreWiresInstruments(t *testing.T) {
	t.Parallel()

	instruments := &telemetry.Instruments{}
	store := NewInstrumentedRepositoryFreshnessStore(&fakeQueryer{}, instruments)
	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p)", store.Instruments, instruments)
	}
}

// TestReadRepositoryFreshnessRecordsDurationAndErrorMetrics verifies a
// successful read records
// eshu_dp_repository_freshness_query_duration_seconds and a failed read
// increments eshu_dp_repository_freshness_query_errors_total, so an operator
// dashboard actually observes this query's latency and error rate.
func TestReadRepositoryFreshnessRecordsDurationAndErrorMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	okQueryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	okStore := NewInstrumentedRepositoryFreshnessStore(okQueryer, instruments)
	if _, err := okStore.ReadRepositoryFreshness(context.Background(), "repo-1"); err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}

	failQueryer := &fakeQueryer{responses: []fakeRows{{err: errors.New("boom")}}}
	failStore := NewInstrumentedRepositoryFreshnessStore(failQueryer, instruments)
	if _, err := failStore.ReadRepositoryFreshness(context.Background(), "repo-1"); err == nil {
		t.Fatal("ReadRepositoryFreshness() error = nil, want the queryer failure")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	var sawDuration, sawErrorCount bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "eshu_dp_repository_freshness_query_duration_seconds":
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", m.Name, m.Data)
				}
				if len(histogram.DataPoints) == 0 {
					t.Fatalf("metric %s has no data points", m.Name)
				}
				if histogram.DataPoints[0].Count != 2 {
					t.Fatalf("metric %s count = %d, want 2 (one per ReadRepositoryFreshness call)", m.Name, histogram.DataPoints[0].Count)
				}
				sawDuration = true
			case "eshu_dp_repository_freshness_query_errors_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
				}
				if len(sum.DataPoints) == 0 || sum.DataPoints[0].Value != 1 {
					t.Fatalf("metric %s data points = %+v, want a single point with value 1", m.Name, sum.DataPoints)
				}
				sawErrorCount = true
			}
		}
	}
	if !sawDuration {
		t.Fatal("duration histogram eshu_dp_repository_freshness_query_duration_seconds not recorded")
	}
	if !sawErrorCount {
		t.Fatal("error counter eshu_dp_repository_freshness_query_errors_total not recorded")
	}
}
