// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestComputeServiceChangedSinceDeltaClassifiesIncidentsFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 16, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The family-generic counts query returns rows for every family present in
		// the diff. Incidents carries each verdict; ownership stays unchanged.
		{rows: [][]any{
			serviceCountRow("ownership", "unchanged", 2),
			serviceCountRow("incidents", "added", 1),
			serviceCountRow("incidents", "updated", 1),
			serviceCountRow("incidents", "unchanged", 1),
			serviceCountRow("incidents", "retired", 1),
			serviceCountRow("incidents", "superseded", 1),
		}},
		// Sample reads run per family in category order (ownership, deployment,
		// runtime, dependencies, docs, incidents), per non-zero classification in
		// classification order. The deployment, runtime, dependencies, and docs
		// families have zero counts here, so they consume no sample reads.
		{rows: [][]any{{"ownership:svc-a:team-a"}}},                                        // ownership unchanged
		{rows: [][]any{{"incidents:svc-a:pagerduty:PINC-1:applied_routing:applied:add"}}},  // incidents added
		{rows: [][]any{{"incidents:svc-a:pagerduty:PINC-1:applied_routing:applied:upd"}}},  // incidents updated
		{rows: [][]any{{"incidents:svc-a:pagerduty:PINC-1:applied_routing:applied:same"}}}, // incidents unchanged
		{rows: [][]any{{"incidents:svc-a:pagerduty:PINC-1:applied_routing:applied:gone"}}}, // incidents retired
		{rows: [][]any{{"incidents:svc-a:pagerduty:PINC-2:live_routing:observed:drop"}}},   // incidents superseded
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
	incidents := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryIncidents)
	c := incidents.Counts
	if c.Added != 1 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("incidents counts wrong: %+v", c)
	}
	if got := incidents.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "incidents:svc-a:pagerduty:PINC-1:applied_routing:applied:gone" {
		t.Fatalf("incidents retired samples wrong: %+v", got)
	}
	if got := incidents.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "incidents:svc-a:pagerduty:PINC-2:live_routing:observed:drop" {
		t.Fatalf("incidents superseded samples wrong: %+v", got)
	}
	if got := incidents.Samples[statuspkg.ChangedSinceRetired]; got[0].FactKind != "incidents" {
		t.Fatalf("incidents sample fact_kind = %q, want incidents", got[0].FactKind)
	}
	// Ownership stayed unchanged: an incidents-only change must not invent ownership
	// deltas, and the deployment/runtime/dependencies/docs families report zero
	// deltas (never silently dropped).
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	if owner.Counts.Added != 0 || owner.Counts.Updated != 0 || owner.Counts.Retired != 0 || owner.Counts.Superseded != 0 {
		t.Fatalf("ownership family must not churn on an incidents-only change: %+v", owner.Counts)
	}
	for _, family := range []statuspkg.ChangedSinceCategory{
		statuspkg.ChangedSinceCategoryDeployment,
		statuspkg.ChangedSinceCategoryRuntime,
		statuspkg.ChangedSinceCategoryDependencies,
		statuspkg.ChangedSinceCategoryDocs,
	} {
		if delta := serviceCategoryDelta(t, summary, family); delta.Counts.Total() != 0 {
			t.Fatalf("%s family should report zero deltas for an incidents-only change: %+v", family, delta.Counts)
		}
	}
}

func TestComputeServiceChangedSinceDeltaUnchangedProducesNoFalseIncidentDeltas(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 17, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The incidents family is entirely unchanged across the two generations: the
		// generation-stable key matches, so the diff must classify it unchanged and
		// invent no added/updated/retired/superseded incident deltas.
		{rows: [][]any{serviceCountRow("incidents", "unchanged", 3)}},
		{rows: [][]any{{"incidents:svc-a:pagerduty:PINC-1:applied_routing:applied:same"}}}, // incidents unchanged
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
	incidents := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryIncidents)
	c := incidents.Counts
	if c.Added != 0 || c.Updated != 0 || c.Retired != 0 || c.Superseded != 0 {
		t.Fatalf("unchanged incidents generation produced false deltas: %+v", c)
	}
	if c.Unchanged != 3 {
		t.Fatalf("incidents unchanged count = %d, want 3", c.Unchanged)
	}
}
