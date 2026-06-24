// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	// Both service families are reported. The fixture has only ownership rows, so
	// the deployment family is present with zero deltas (never silently dropped).
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	deployment := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDeployment)
	c := owner.Counts
	if c.Added != 0 || c.Updated != 0 || c.Retired != 0 || c.Superseded != 0 {
		t.Fatalf("unchanged generation produced false deltas: %+v", c)
	}
	if c.Unchanged != 3 {
		t.Fatalf("unchanged count = %d, want 3", c.Unchanged)
	}
	if deployment.Counts.Total() != 0 {
		t.Fatalf("deployment family should report zero deltas for an ownership-only fixture: %+v", deployment.Counts)
	}
}

// serviceCategoryDelta returns the category delta for one family, failing the
// test when the family is missing from the summary.
func serviceCategoryDelta(
	t *testing.T,
	summary statuspkg.ServiceChangedSinceSummary,
	family statuspkg.ChangedSinceCategory,
) statuspkg.ChangedSinceCategoryDelta {
	t.Helper()
	for _, category := range summary.Categories {
		if category.Category == family {
			return category
		}
	}
	t.Fatalf("summary missing %q family: %+v", family, summary.Categories)
	return statuspkg.ChangedSinceCategoryDelta{}
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

func TestComputeServiceChangedSinceDeltaClassifiesDeploymentFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The family-generic counts query returns rows for every family present in
		// the diff. Deployment carries each verdict; ownership stays unchanged.
		{rows: [][]any{
			serviceCountRow("ownership", "unchanged", 2),
			serviceCountRow("deployment", "added", 1),
			serviceCountRow("deployment", "updated", 1),
			serviceCountRow("deployment", "unchanged", 1),
			serviceCountRow("deployment", "retired", 1),
			serviceCountRow("deployment", "superseded", 1),
		}},
		// Sample reads run per family in category order (ownership, deployment),
		// per non-zero classification in classification order.
		{rows: [][]any{{"ownership:svc-a:team-a"}}}, // ownership unchanged
		{rows: [][]any{{"deployment:svc-a:add"}}},   // deployment added
		{rows: [][]any{{"deployment:svc-a:upd"}}},   // deployment updated
		{rows: [][]any{{"deployment:svc-a:same"}}},  // deployment unchanged
		{rows: [][]any{{"deployment:svc-a:gone"}}},  // deployment retired
		{rows: [][]any{{"deployment:svc-a:drop"}}},  // deployment superseded
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
	deployment := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDeployment)
	c := deployment.Counts
	if c.Added != 1 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("deployment counts wrong: %+v", c)
	}
	if got := deployment.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "deployment:svc-a:gone" {
		t.Fatalf("deployment retired samples wrong: %+v", got)
	}
	if got := deployment.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "deployment:svc-a:drop" {
		t.Fatalf("deployment superseded samples wrong: %+v", got)
	}
	if got := deployment.Samples[statuspkg.ChangedSinceRetired]; got[0].FactKind != "deployment" {
		t.Fatalf("deployment sample fact_kind = %q, want deployment", got[0].FactKind)
	}
	// Ownership stayed unchanged: a deployment-only change must not invent
	// ownership deltas.
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	if owner.Counts.Added != 0 || owner.Counts.Updated != 0 || owner.Counts.Retired != 0 || owner.Counts.Superseded != 0 {
		t.Fatalf("ownership family must not churn on a deployment-only change: %+v", owner.Counts)
	}
}

func TestComputeServiceChangedSinceDeltaClassifiesRuntimeFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 13, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The family-generic counts query returns rows for every family present in
		// the diff. Runtime carries each verdict; ownership stays unchanged.
		{rows: [][]any{
			serviceCountRow("ownership", "unchanged", 2),
			serviceCountRow("runtime", "added", 1),
			serviceCountRow("runtime", "updated", 1),
			serviceCountRow("runtime", "unchanged", 1),
			serviceCountRow("runtime", "retired", 1),
			serviceCountRow("runtime", "superseded", 1),
		}},
		// Sample reads run per family in category order (ownership, deployment,
		// runtime), per non-zero classification in classification order. The
		// deployment family has zero counts here, so it consumes no sample reads.
		{rows: [][]any{{"ownership:svc-a:team-a"}}},                    // ownership unchanged
		{rows: [][]any{{"runtime:svc-a:kubernetes:prod:wi-checkout"}}}, // runtime added
		{rows: [][]any{{"runtime:svc-a:kubernetes:prod:wi-payments"}}}, // runtime updated
		{rows: [][]any{{"runtime:svc-a:ecs:staging:wi-checkout"}}},     // runtime unchanged
		{rows: [][]any{{"runtime:svc-a:kubernetes:qa:wi-gone"}}},       // runtime retired
		{rows: [][]any{{"runtime:svc-a:kubernetes:dev:wi-drop"}}},      // runtime superseded
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
	runtime := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryRuntime)
	c := runtime.Counts
	if c.Added != 1 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("runtime counts wrong: %+v", c)
	}
	if got := runtime.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "runtime:svc-a:kubernetes:qa:wi-gone" {
		t.Fatalf("runtime retired samples wrong: %+v", got)
	}
	if got := runtime.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "runtime:svc-a:kubernetes:dev:wi-drop" {
		t.Fatalf("runtime superseded samples wrong: %+v", got)
	}
	if got := runtime.Samples[statuspkg.ChangedSinceRetired]; got[0].FactKind != "runtime" {
		t.Fatalf("runtime sample fact_kind = %q, want runtime", got[0].FactKind)
	}
	// Ownership stayed unchanged: a runtime-only change must not invent ownership
	// deltas, and the deployment family reports zero deltas (never silently dropped).
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	if owner.Counts.Added != 0 || owner.Counts.Updated != 0 || owner.Counts.Retired != 0 || owner.Counts.Superseded != 0 {
		t.Fatalf("ownership family must not churn on a runtime-only change: %+v", owner.Counts)
	}
	deployment := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDeployment)
	if deployment.Counts.Total() != 0 {
		t.Fatalf("deployment family should report zero deltas for a runtime-only change: %+v", deployment.Counts)
	}
}

func TestComputeServiceChangedSinceDeltaClassifiesDependenciesFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 13, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The family-generic counts query returns rows for every family present in
		// the diff. Dependencies carries each verdict; ownership stays unchanged.
		{rows: [][]any{
			serviceCountRow("ownership", "unchanged", 2),
			serviceCountRow("dependencies", "added", 1),
			serviceCountRow("dependencies", "updated", 1),
			serviceCountRow("dependencies", "unchanged", 1),
			serviceCountRow("dependencies", "retired", 1),
			serviceCountRow("dependencies", "superseded", 1),
		}},
		// Sample reads run per family in category order (ownership, deployment,
		// runtime, dependencies), per non-zero classification in classification
		// order. The deployment and runtime families have zero counts here, so they
		// consume no sample reads.
		{rows: [][]any{{"ownership:svc-a:team-a"}}},           // ownership unchanged
		{rows: [][]any{{"dependencies:svc-a:dep-added"}}},     // dependencies added
		{rows: [][]any{{"dependencies:svc-a:dep-updated"}}},   // dependencies updated
		{rows: [][]any{{"dependencies:svc-a:dep-unchanged"}}}, // dependencies unchanged
		{rows: [][]any{{"dependencies:svc-a:dep-gone"}}},      // dependencies retired
		{rows: [][]any{{"dependencies:svc-a:dep-drop"}}},      // dependencies superseded
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
	dependencies := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDependencies)
	c := dependencies.Counts
	if c.Added != 1 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("dependencies counts wrong: %+v", c)
	}
	if got := dependencies.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "dependencies:svc-a:dep-gone" {
		t.Fatalf("dependencies retired samples wrong: %+v", got)
	}
	if got := dependencies.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "dependencies:svc-a:dep-drop" {
		t.Fatalf("dependencies superseded samples wrong: %+v", got)
	}
	if got := dependencies.Samples[statuspkg.ChangedSinceRetired]; got[0].FactKind != "dependencies" {
		t.Fatalf("dependencies sample fact_kind = %q, want dependencies", got[0].FactKind)
	}
	// Ownership stayed unchanged: a dependencies-only change must not invent
	// ownership deltas, and the deployment/runtime families report zero deltas
	// (never silently dropped).
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	if owner.Counts.Added != 0 || owner.Counts.Updated != 0 || owner.Counts.Retired != 0 || owner.Counts.Superseded != 0 {
		t.Fatalf("ownership family must not churn on a dependencies-only change: %+v", owner.Counts)
	}
	deployment := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDeployment)
	if deployment.Counts.Total() != 0 {
		t.Fatalf("deployment family should report zero deltas for a dependencies-only change: %+v", deployment.Counts)
	}
	runtime := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryRuntime)
	if runtime.Counts.Total() != 0 {
		t.Fatalf("runtime family should report zero deltas for a dependencies-only change: %+v", runtime.Counts)
	}
}

func TestComputeServiceChangedSinceDeltaClassifiesDocsFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "gen-current", observed, false)},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		// The family-generic counts query returns rows for every family present in
		// the diff. Docs carries each verdict; ownership stays unchanged.
		{rows: [][]any{
			serviceCountRow("ownership", "unchanged", 2),
			serviceCountRow("docs", "added", 1),
			serviceCountRow("docs", "updated", 1),
			serviceCountRow("docs", "unchanged", 1),
			serviceCountRow("docs", "retired", 1),
			serviceCountRow("docs", "superseded", 1),
		}},
		// Sample reads run per family in category order (ownership, deployment,
		// runtime, dependencies, docs), per non-zero classification in
		// classification order. The deployment, runtime, and dependencies families
		// have zero counts here, so they consume no sample reads.
		{rows: [][]any{{"ownership:svc-a:team-a"}}},                       // ownership unchanged
		{rows: [][]any{{"docs:svc-a:confluence:section:add:doc:1"}}},      // docs added
		{rows: [][]any{{"docs:svc-a:confluence:section:upd:doc:1"}}},      // docs updated
		{rows: [][]any{{"docs:svc-a:confluence:section:same:doc:1"}}},     // docs unchanged
		{rows: [][]any{{"docs:svc-a:confluence:section:gone:doc:1"}}},     // docs retired
		{rows: [][]any{{"docs:svc-a:git_markdown:readme.md#drop:doc:2"}}}, // docs superseded
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
	docs := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDocs)
	c := docs.Counts
	if c.Added != 1 || c.Updated != 1 || c.Unchanged != 1 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("docs counts wrong: %+v", c)
	}
	if got := docs.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "docs:svc-a:confluence:section:gone:doc:1" {
		t.Fatalf("docs retired samples wrong: %+v", got)
	}
	if got := docs.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "docs:svc-a:git_markdown:readme.md#drop:doc:2" {
		t.Fatalf("docs superseded samples wrong: %+v", got)
	}
	if got := docs.Samples[statuspkg.ChangedSinceRetired]; got[0].FactKind != "docs" {
		t.Fatalf("docs sample fact_kind = %q, want docs", got[0].FactKind)
	}
	// Ownership stayed unchanged: a docs-only change must not invent ownership
	// deltas, and the deployment/runtime/dependencies families report zero deltas
	// (never silently dropped).
	owner := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryOwnership)
	if owner.Counts.Added != 0 || owner.Counts.Updated != 0 || owner.Counts.Retired != 0 || owner.Counts.Superseded != 0 {
		t.Fatalf("ownership family must not churn on a docs-only change: %+v", owner.Counts)
	}
	for _, family := range []statuspkg.ChangedSinceCategory{
		statuspkg.ChangedSinceCategoryDeployment,
		statuspkg.ChangedSinceCategoryRuntime,
		statuspkg.ChangedSinceCategoryDependencies,
	} {
		if delta := serviceCategoryDelta(t, summary, family); delta.Counts.Total() != 0 {
			t.Fatalf("%s family should report zero deltas for a docs-only change: %+v", family, delta.Counts)
		}
	}
}

func TestComputeServiceChangedSinceDeltaUnavailableReportsDocsFamily(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 15, 0, 0, 0, time.UTC)
	// The service exists but has no current active generation: the diff is
	// unavailable and every family, docs included, is reported unavailable rather
	// than as a confident zero-delta.
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: serviceScopeRow("svc-a", "", observed, false)},
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
		t.Fatal("Unavailable = false, want true for a service with no active generation")
	}
	docs := serviceCategoryDelta(t, summary, statuspkg.ChangedSinceCategoryDocs)
	if !docs.Unavailable {
		t.Fatalf("docs family must be reported unavailable, not zero-delta: %+v", docs)
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
	if len(summary.Categories) != len(statuspkg.ServiceChangedSinceCategories) {
		t.Fatalf("categories = %+v, want one per service family", summary.Categories)
	}
	for _, category := range summary.Categories {
		if !category.Unavailable {
			t.Fatalf("category %q should be marked unavailable: %+v", category.Category, summary.Categories)
		}
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
