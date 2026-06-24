// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestComputeServiceChangedSinceDeltaClassifiesVulnerabilitiesFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 16, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The family-generic counts query returns rows for every family present in
		// the diff. Vulnerabilities carries each verdict; ownership stays unchanged.
		{rows: [][]any{
			serviceCountRow("ownership", "unchanged", 2),
			serviceCountRow("vulnerabilities", "added", 1),
			serviceCountRow("vulnerabilities", "updated", 1),
			serviceCountRow("vulnerabilities", "unchanged", 1),
			serviceCountRow("vulnerabilities", "retired", 1),
			serviceCountRow("vulnerabilities", "superseded", 1),
		}},
		// Sample reads run per family in category order (ownership, deployment,
		// runtime, dependencies, docs, incidents, vulnerabilities), per non-zero
		// classification in classification order. The deployment, runtime,
		// dependencies, docs, and incidents families have zero counts here, so they
		// consume no sample reads.
		{rows: [][]any{{"ownership:svc-a:team-a"}}},                                // ownership unchanged
		{rows: [][]any{{"vulnerabilities:svc-a:GHSA-1:npm:left-pad"}}},             // vulnerabilities added
		{rows: [][]any{{"vulnerabilities:svc-a:GHSA-2:npm:right-pad"}}},            // vulnerabilities updated
		{rows: [][]any{{"vulnerabilities:svc-a:GHSA-3:pypi:requests"}}},            // vulnerabilities unchanged
		{rows: [][]any{{"vulnerabilities:svc-a:GHSA-4:go:golang.org/x/net"}}},      // vulnerabilities retired
		{rows: [][]any{{"vulnerabilities:svc-a:GHSA-5:maven:org.apache.logging"}}}, // vulnerabilities superseded
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
	vulns := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryVulnerabilities)
	c := vulns.Counts
	if c.Added != 1 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("vulnerabilities counts wrong: %+v", c)
	}
	if got := vulns.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "vulnerabilities:svc-a:GHSA-4:go:golang.org/x/net" {
		t.Fatalf("vulnerabilities retired samples wrong: %+v", got)
	}
	if got := vulns.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "vulnerabilities:svc-a:GHSA-5:maven:org.apache.logging" {
		t.Fatalf("vulnerabilities superseded samples wrong: %+v", got)
	}
	if got := vulns.Samples[statuspkg.ChangedSinceRetired]; got[0].FactKind != "vulnerabilities" {
		t.Fatalf("vulnerabilities sample fact_kind = %q, want vulnerabilities", got[0].FactKind)
	}
	// Ownership stayed unchanged: a vulnerabilities-only change must not invent
	// ownership deltas, and the deployment/runtime/dependencies/docs/incidents
	// families report zero deltas (never silently dropped).
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	if owner.Counts.Added != 0 || owner.Counts.Updated != 0 || owner.Counts.Retired != 0 || owner.Counts.Superseded != 0 {
		t.Fatalf("ownership family must not churn on a vulnerabilities-only change: %+v", owner.Counts)
	}
	for _, family := range []statuspkg.ChangedSinceCategory{
		statuspkg.ChangedSinceCategoryDeployment,
		statuspkg.ChangedSinceCategoryRuntime,
		statuspkg.ChangedSinceCategoryDependencies,
		statuspkg.ChangedSinceCategoryDocs,
		statuspkg.ChangedSinceCategoryIncidents,
	} {
		if delta := serviceCategoryDelta(t, summary, family); delta.Counts.Total() != 0 {
			t.Fatalf("%s family should report zero deltas for a vulnerabilities-only change: %+v", family, delta.Counts)
		}
	}
}

func TestComputeServiceChangedSinceDeltaUnchangedProducesNoFalseVulnerabilityDeltas(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 17, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The vulnerabilities family is entirely unchanged across the two
		// generations: the generation-stable advisory/package key matches, so the
		// diff must classify it unchanged and invent no added/updated/retired/
		// superseded vulnerability deltas.
		{rows: [][]any{serviceCountRow("vulnerabilities", "unchanged", 3)}},
		{rows: [][]any{{"vulnerabilities:svc-a:GHSA-3:pypi:requests"}}}, // vulnerabilities unchanged
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
	vulns := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryVulnerabilities)
	c := vulns.Counts
	if c.Added != 0 || c.Updated != 0 || c.Retired != 0 || c.Superseded != 0 {
		t.Fatalf("unchanged vulnerabilities generation produced false deltas: %+v", c)
	}
	if c.Unchanged != 3 {
		t.Fatalf("unchanged vulnerabilities count = %d, want 3", c.Unchanged)
	}
}
