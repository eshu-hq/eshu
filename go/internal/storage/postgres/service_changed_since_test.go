package postgres

import (
	"context"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func serviceScopeRow(serviceID, currentGen string, currentObserved any, hasPending bool) [][]any {
	return [][]any{{serviceID, currentGen, currentObserved, hasPending}}
}

func serviceCountRow(family, classification string, count int64) []any {
	return []any{family, classification, count}
}

func TestComputeServiceChangedSinceDeltaUnchangedProducesNoFalseDeltas(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-checkout", "service-gen-current", observed, false)},
		{rows: [][]any{{"service-gen-prior", observed.Add(-time.Hour)}}},
		{rows: [][]any{serviceCountRow("ownership", "unchanged", 3)}},
		{rows: [][]any{{"ownership:svc-checkout:team-a"}}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID:         "svc-checkout",
		SinceGenerationID: "service-gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if summary.Unavailable {
		t.Fatal("Unavailable = true, want false for unchanged service generation")
	}
	if summary.SinceGenerationID != "service-gen-prior" || summary.CurrentActiveGenerationID != "service-gen-current" {
		t.Fatalf("unexpected generations: %+v", summary)
	}
	if len(summary.Categories) != 1 || summary.Categories[0].Category != statuspkg.ChangedSinceCategoryOwnership {
		t.Fatalf("categories = %+v, want a single ownership category", summary.Categories)
	}
	c := summary.Categories[0].Counts
	if c.Added != 0 || c.Updated != 0 || c.Retired != 0 || c.Superseded != 0 {
		t.Fatalf("unchanged generation produced false deltas: %+v", c)
	}
	if c.Unchanged != 3 {
		t.Fatalf("unchanged count = %d, want 3", c.Unchanged)
	}
}

func TestComputeServiceChangedSinceDeltaClassifiesAllVerdicts(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		{rows: [][]any{
			serviceCountRow("ownership", "added", 2),
			serviceCountRow("ownership", "updated", 1),
			serviceCountRow("ownership", "unchanged", 1),
			serviceCountRow("ownership", "retired", 1),
			serviceCountRow("ownership", "superseded", 1),
		}},
		// One sample read per non-zero classification, in classification order:
		// added, updated, unchanged, retired, superseded.
		{rows: [][]any{{"ownership:svc-a:new1"}, {"ownership:svc-a:new2"}}},
		{rows: [][]any{{"ownership:svc-a:upd"}}},
		{rows: [][]any{{"ownership:svc-a:same"}}},
		{rows: [][]any{{"ownership:svc-a:gone"}}},
		{rows: [][]any{{"ownership:svc-a:dropped"}}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID:         "svc-a",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	owner := summary.Categories[0]
	c := owner.Counts
	if c.Added != 2 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("ownership counts wrong: %+v", c)
	}
	if got := owner.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "ownership:svc-a:gone" {
		t.Fatalf("retired samples wrong: %+v", got)
	}
	if got := owner.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "ownership:svc-a:dropped" {
		t.Fatalf("superseded samples wrong: %+v", got)
	}
}

func TestComputeServiceChangedSinceDeltaUnknownServiceNotFound(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID:         "missing",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if summary.ServiceID != "" {
		t.Fatalf("unknown service should leave ServiceID empty, got %q", summary.ServiceID)
	}
}

func TestComputeServiceChangedSinceDeltaNoActiveGenerationUnavailable(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "", nil, false)},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID:         "svc-a",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if !summary.Unavailable {
		t.Fatal("service with no current active generation must be Unavailable, never zero deltas")
	}
	if len(summary.Categories) != 1 || !summary.Categories[0].Unavailable {
		t.Fatalf("categories should be marked unavailable: %+v", summary.Categories)
	}
}

func TestComputeServiceChangedSinceDeltaUnknownPriorGenerationNotFound(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID:         "svc-a",
		SinceGenerationID: "missing",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if summary.ServiceID == "" {
		t.Fatal("known service should resolve a ServiceID even when the prior generation is missing")
	}
	if summary.SinceGenerationID != "" {
		t.Fatalf("missing prior generation should leave SinceGenerationID empty, got %q", summary.SinceGenerationID)
	}
	if summary.Unavailable {
		t.Fatal("missing prior generation is not-found, not unavailable")
	}
}
