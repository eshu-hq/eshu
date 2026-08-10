// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"strings"
	"testing"
	"time"
)

var fixedLiveCreatedAt = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// TestBuildLiveBundleEmptyState proves a stack with zero repositories and
// empty queues renders that state explicitly instead of crashing or
// producing an empty-looking bundle that hides the truth.
func TestBuildLiveBundleEmptyState(t *testing.T) {
	snapshot := LiveSnapshot{
		RepositoryCount: 0,
		HealthState:     "healthy",
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})

	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate(BuildLiveBundle(empty state)) error = %v", err)
	}
	if bundle.Contents.PipelineState == nil {
		t.Fatal("Contents.PipelineState = nil, want populated snapshot for empty state")
	}
	if bundle.Contents.PipelineState.RepositoryCount != 0 {
		t.Fatalf("RepositoryCount = %d, want 0", bundle.Contents.PipelineState.RepositoryCount)
	}
	if bundle.Contents.PipelineState.Queue != (PipelineQueueSnapshot{}) {
		t.Fatalf("Queue = %+v, want zero-value queue explicitly reported", bundle.Contents.PipelineState.Queue)
	}
}

// TestBuildLiveBundlePartialReducerQueue proves pending/in-flight/retrying/
// dead-letter rows are reported rather than rounded to "healthy".
func TestBuildLiveBundlePartialReducerQueue(t *testing.T) {
	snapshot := LiveSnapshot{
		RepositoryCount: 3,
		HealthState:     "degraded",
		HealthReasons:   []string{"queue backlog"},
		Queue: LiveQueueSnapshot{
			Pending:    4,
			InFlight:   2,
			Retrying:   1,
			DeadLetter: 1,
		},
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})

	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate(BuildLiveBundle(partial queue)) error = %v", err)
	}
	got := bundle.Contents.PipelineState.Queue
	want := PipelineQueueSnapshot{Pending: 4, InFlight: 2, Retrying: 1, DeadLetter: 1}
	if got != want {
		t.Fatalf("Queue = %+v, want %+v", got, want)
	}
	if bundle.Contents.PipelineState.HealthState != "degraded" {
		t.Fatalf("HealthState = %q, want %q", bundle.Contents.PipelineState.HealthState, "degraded")
	}
}

// TestBuildLiveBundleProviderDisabled proves a no-provider stack is reported
// as a distinct "disabled/not configured" state, not folded into "failed".
func TestBuildLiveBundleProviderDisabled(t *testing.T) {
	snapshot := LiveSnapshot{
		SemanticExtraction: LiveSemanticExtractionSnapshot{
			State:              "unavailable",
			Reason:             "provider_not_configured",
			ProviderConfigured: false,
		},
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})

	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate(BuildLiveBundle(provider disabled)) error = %v", err)
	}
	state := bundle.Contents.SemanticProviderState
	if state == nil {
		t.Fatal("Contents.SemanticProviderState = nil, want populated snapshot")
	}
	if state.ProviderConfigured {
		t.Fatal("ProviderConfigured = true, want false for a no-provider stack")
	}
	if state.State != "unavailable" || state.Reason != "provider_not_configured" {
		t.Fatalf("state=%q reason=%q, want unavailable/provider_not_configured (distinct from a failed/unhealthy provider)", state.State, state.Reason)
	}
}

// TestBuildLiveBundleRecordsMissingFactCounts proves per-kind fact counts are
// recorded as an explicit, honest gap rather than silently absent or faked.
func TestBuildLiveBundleRecordsMissingFactCounts(t *testing.T) {
	bundle := BuildLiveBundle(LiveSnapshot{}, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})

	found := false
	for _, missing := range bundle.Missing {
		if missing.Family == "fact_counts" {
			found = true
			if missing.Reason != "fact_counts_not_exposed_by_status_api" {
				t.Fatalf("fact_counts reason = %q, want %q", missing.Reason, "fact_counts_not_exposed_by_status_api")
			}
		}
	}
	if !found {
		t.Fatal("Missing does not contain a fact_counts entry")
	}
}

// TestBuildLiveBundleRejectsPrivateData proves that if a caller's snapshot
// carries a private endpoint or local absolute path anywhere in its free-text
// fields (e.g. a health reason from a misconfigured stack), the shared
// Validate() rejects the resulting bundle the same way it rejects the demo
// bundle's canaries. This is the live path's defense in depth on top of
// LiveSnapshot never carrying a URL/host field at all.
func TestBuildLiveBundleRejectsPrivateData(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{
			name:   "private endpoint in health reason",
			reason: "postgres dial failed at http://127.0.0.1:5432",
			want:   "private endpoint",
		},
		{
			name:   "local absolute path in health reason",
			reason: "config not found at /Users/example/.eshu/config.yaml",
			want:   "local absolute path",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := LiveSnapshot{
				HealthState:   "degraded",
				HealthReasons: []string{tt.reason},
			}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})
			err := Validate(bundle)
			if err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

// TestDemoBundleUnchangedByLiveAddition pins BuildDemoBundle's bundle_id to
// the value captured before the live-bundle addition. The new Contents
// fields (PipelineState, SemanticProviderState) are pointers with
// json:",omitempty" precisely so the demo bundle's serialized shape, and
// therefore its content hash, never changes.
func TestDemoBundleUnchangedByLiveAddition(t *testing.T) {
	const wantBundleID = "evidence-bundle:a3d3ad013f9f4b7c09ef8b723e214536"
	bundle := BuildDemoBundle(DemoBundleOptions{ScopeID: "repo:demo/service"})
	if bundle.BundleID != wantBundleID {
		t.Fatalf("BuildDemoBundle().BundleID = %q, want %q (live-bundle addition must not change demo bundle_id)", bundle.BundleID, wantBundleID)
	}
	if bundle.Contents.PipelineState != nil {
		t.Fatalf("Contents.PipelineState = %+v, want nil for demo bundle", bundle.Contents.PipelineState)
	}
	if bundle.Contents.SemanticProviderState != nil {
		t.Fatalf("Contents.SemanticProviderState = %+v, want nil for demo bundle", bundle.Contents.SemanticProviderState)
	}
}

// TestBuildLiveBundleSortsEveryNestedSlice feeds each new nested slice in
// deliberately reversed order with more than one element, because the shipped
// cases all used a single element and so could not have caught a wrong sort
// key. bundle_id is a hash of the serialized bundle, so an unsorted slice
// makes the same stack produce a different id on every export.
func TestBuildLiveBundleSortsEveryNestedSlice(t *testing.T) {
	snapshot := LiveSnapshot{
		RepositoryCount: 3,
		HealthState:     "degraded",
		HealthReasons:   []string{"zeta reason", "alpha reason", "mid reason"},
		StageSummaries: []LiveStageSummarySnapshot{
			{Stage: "reduce"}, {Stage: "parse"}, {Stage: "discover"},
		},
		DomainBacklogs: []LiveDomainBacklogSnapshot{
			{Domain: "workload_identity"}, {Domain: "aws_relationship"}, {Domain: "deployment_mapping"},
		},
		Collectors: []LiveCollectorSnapshot{
			{CollectorKind: "git", StatusCategory: "ready", Health: "unhealthy"},
			{CollectorKind: "git", StatusCategory: "ready", Health: "healthy"},
			{CollectorKind: "aws", StatusCategory: "failed", Health: "unhealthy"},
		},
		SemanticExtraction: LiveSemanticExtractionSnapshot{
			State:              "ready",
			ProviderConfigured: true,
			ProviderProfiles: []LiveSemanticProviderProfileSnapshot{
				{ProfileID: "zeta", ProviderKind: "openai", State: "ready"},
				{ProfileID: "alpha", ProviderKind: "anthropic", State: "ready"},
			},
		},
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:sort", CreatedAt: fixedLiveCreatedAt})
	state := bundle.Contents.PipelineState
	if state == nil {
		t.Fatal("PipelineState is nil")
	}

	assertSorted := func(name string, got []string) {
		t.Helper()
		for i := 1; i < len(got); i++ {
			if got[i-1] > got[i] {
				t.Fatalf("%s not sorted: %v", name, got)
			}
		}
	}

	assertSorted("HealthReasons", state.HealthReasons)
	stages := make([]string, 0, len(state.StageSummaries))
	for _, row := range state.StageSummaries {
		stages = append(stages, row.Stage)
	}
	assertSorted("StageSummaries", stages)
	domains := make([]string, 0, len(state.DomainBacklogs))
	for _, row := range state.DomainBacklogs {
		domains = append(domains, row.Domain)
	}
	assertSorted("DomainBacklogs", domains)
	collectors := make([]string, 0, len(state.Collectors))
	for _, row := range state.Collectors {
		collectors = append(collectors, row.CollectorKind+"/"+row.StatusCategory+"/"+row.Health)
	}
	assertSorted("Collectors", collectors)

	profiles := make([]string, 0)
	if provider := bundle.Contents.SemanticProviderState; provider != nil {
		for _, row := range provider.ProviderProfiles {
			profiles = append(profiles, row.ProfileID)
		}
	}
	assertSorted("ProviderProfiles", profiles)

	// Same input must hash identically on a rebuild.
	again := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:sort", CreatedAt: fixedLiveCreatedAt})
	if again.BundleID != bundle.BundleID {
		t.Fatalf("bundle_id not stable across rebuilds: %q vs %q", bundle.BundleID, again.BundleID)
	}
}

// TestBuildLiveBundleNeverClaimsFreshness guards against reporting an empty,
// degraded, or stalled stack as current. The status routes carry no indexed-at
// or generation-completed-at signal, so "fresh" was an assertion the data could
// not support; support tooling reading it would have shown stale evidence as
// up to date.
func TestBuildLiveBundleNeverClaimsFreshness(t *testing.T) {
	for name, snapshot := range map[string]LiveSnapshot{
		"empty":    {},
		"degraded": {RepositoryCount: 3, HealthState: "degraded"},
		"ready":    {RepositoryCount: 9, HealthState: "ready"},
	} {
		t.Run(name, func(t *testing.T) {
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			for _, item := range bundle.Contents.OperatorState {
				if item.Kind != "freshness" {
					continue
				}
				if item.State == "fresh" {
					t.Fatalf("freshness reported as %q with no freshness signal available", item.State)
				}
			}
			var recorded bool
			for _, gap := range bundle.Missing {
				if gap.Family == "freshness" {
					recorded = true
				}
			}
			if !recorded {
				t.Fatal("missing_evidence does not record the absent freshness signal")
			}
		})
	}
}
