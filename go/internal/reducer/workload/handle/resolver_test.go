// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package handle

import (
	"context"
	"fmt"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type stubHandleLookup struct {
	names [][]string
	rows  []Candidate
	err   error
}

func (s *stubHandleLookup) ListWorkloadHandleCandidates(_ context.Context, names []string) ([]Candidate, error) {
	s.names = append(s.names, names)
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

type stubScopeRepos struct {
	repos []string
	err   error
}

func (s *stubScopeRepos) ListScopeRepositoryIDs(_ context.Context, _, _ string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.repos, nil
}

func TestResolveAllBatchesOneLookupPerIntent(t *testing.T) {
	t.Parallel()

	lookup := &stubHandleLookup{rows: []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
	}}
	resolver := &Resolver{
		ScopeRepos: &stubScopeRepos{repos: []string{"repository:r_alpha"}},
		Lookup:     lookup,
	}

	items := []Item{
		{Selector: "workload:checkout"},
		{Selector: "workload:checkout"},
		{Selector: "workload:repository:r_alpha:checkout"},
	}
	got, err := resolver.ResolveAll(context.Background(), "scope-1", "gen-1", items)
	if err != nil {
		t.Fatalf("ResolveAll() error = %v", err)
	}
	if len(lookup.names) != 1 {
		t.Fatalf("lookup calls = %d, want 1 batched call per intent", len(lookup.names))
	}
	if got["workload:checkout"].Outcome != OutcomeUniqueByName {
		t.Fatalf("handle outcome = %q, want unique_by_name inside the scope", got["workload:checkout"].Outcome)
	}
	if got["workload:repository:r_alpha:checkout"].Outcome != OutcomeExact {
		t.Fatalf("full id outcome = %q, want exact", got["workload:repository:r_alpha:checkout"].Outcome)
	}
}

func TestResolveAllExplicitRepoBeatsScopeRepos(t *testing.T) {
	t.Parallel()

	lookup := &stubHandleLookup{rows: []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
	}}
	resolver := &Resolver{
		ScopeRepos: &stubScopeRepos{repos: []string{"repository:r_alpha"}},
		Lookup:     lookup,
	}

	item := Item{Selector: "workload:checkout", RepoID: "repository:r_beta"}
	got, err := resolver.ResolveAll(context.Background(), "scope-1", "gen-1", []Item{item})
	if err != nil {
		t.Fatalf("ResolveAll() error = %v", err)
	}
	resolution := got[item.Key()]
	if resolution.Outcome != OutcomeUniqueByName {
		t.Fatalf("outcome = %q, want unique_by_name in the explicit repo", resolution.Outcome)
	}
	if resolution.WorkloadID != "workload:repository:r_beta:checkout" {
		t.Fatalf("id = %q, want the explicit-repo node", resolution.WorkloadID)
	}
}

func TestResolveAllNilLookupKeepsLegacyRows(t *testing.T) {
	t.Parallel()

	resolver := &Resolver{}
	got, err := resolver.ResolveAll(context.Background(), "scope-1", "gen-1", []Item{{Selector: "workload:checkout"}})
	if err != nil {
		t.Fatalf("ResolveAll() error = %v", err)
	}
	if got != nil {
		t.Fatalf("ResolveAll() = %v, want nil so the caller keeps legacy rows", got)
	}
}

func TestResolveAllLookupErrorFailsClosed(t *testing.T) {
	t.Parallel()

	resolver := &Resolver{Lookup: &stubHandleLookup{err: fmt.Errorf("boom")}}
	if _, err := resolver.ResolveAll(context.Background(), "scope-1", "gen-1", []Item{{Selector: "workload:checkout"}}); err == nil {
		t.Fatal("ResolveAll() error = nil, want the lookup failure")
	}
}

func TestRecordCountsOutcomesOnReducerSurface(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Errorf("meter provider shutdown: %v", err)
		}
	})
	instruments, err := telemetry.NewInstruments(provider.Meter("handle-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}

	Record(ctx, instruments, OutcomeAmbiguous)
	Record(ctx, instruments, OutcomeUniqueByName)
	// Nil instruments and an unregistered counter must never panic a pass.
	Record(ctx, nil, OutcomeAmbiguous)
	Record(ctx, &telemetry.Instruments{}, OutcomeAmbiguous)

	var snapshot metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &snapshot); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts := map[string]int64{}
	for _, scope := range snapshot.ScopeMetrics {
		for _, point := range scope.Metrics {
			if point.Name != "eshu_dp_workload_handle_resolution_total" {
				continue
			}
			sum, ok := point.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data type = %T, want Sum[int64]", point.Data)
			}
			for _, dp := range sum.DataPoints {
				surface, _ := dp.Attributes.Value("surface")
				outcome, _ := dp.Attributes.Value("outcome")
				counts[surface.AsString()+"/"+outcome.AsString()] += dp.Value
			}
		}
	}
	if counts["reducer/ambiguous"] != 1 || counts["reducer/unique_by_name"] != 1 {
		t.Fatalf("resolution counts = %v, want one reducer/ambiguous and one reducer/unique_by_name", counts)
	}
}
