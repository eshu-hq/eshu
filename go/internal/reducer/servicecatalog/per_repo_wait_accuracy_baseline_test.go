// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Per-repo-wait accuracy baseline (#10877), service-catalog leg.
//
// Same contract as the reducer package's per_repo_wait_accuracy_baseline_test:
// pin what the CURRENT global corpus fence does for the should-match,
// should-not-match, and unclear/absence classes, as outcome fingerprints a
// future per-scope-wait lane replays for exact equality. Service-catalog
// specifics pinned here:
//   - the deferral names only the waiting scope+generation (no holding-scope
//     list), so the narrowed design must decide where holder attribution
//     lives before it can claim actionable deferrals;
//   - both evidence families (deployment #1985, dependencies #1987) share one
//     bounded fused load: the fingerprint covers both families' identities;
//   - the #7258 regression shape: a partial set must defer with zero writes,
//     never materialize and report spurious removals on changed-since.
//
// Fixtures and harness only: no production code changes in this lane.

// perRepoWaitServiceCatalogFingerprint renders one comparable string over
// everything the handler would commit: per-service deployment and dependency
// evidence identities plus the correlation-write count. A narrowed replay of
// the same seed must produce the identical string.
func perRepoWaitServiceCatalogFingerprint(
	correlations *recordingServiceCatalogCorrelationWriter,
	materialization *recordingServiceMaterializationWriter,
) string {
	var deployment, dependencies []string
	for _, write := range materialization.writes {
		for _, row := range write.Deployment {
			deployment = append(deployment, row.Identity)
		}
		for _, row := range write.Dependencies {
			dependencies = append(dependencies, row.Identity)
		}
	}
	return fmt.Sprintf(
		"correlations=%d services=%d deployment=[%s] dependencies=[%s]",
		correlations.calls,
		len(materialization.writes),
		strings.Join(slices.Sorted(slices.Values(deployment)), ","),
		strings.Join(slices.Sorted(slices.Values(dependencies)), ","),
	)
}

func TestPerRepoWaitBaselineServiceCatalogShouldMatch(t *testing.T) {
	t.Parallel()

	// First attempt: the foreign scope's generation is retired-or-pending.
	// The fused read reports the fence open; the handler must defer with
	// zero writes (#7258: anything else supersedes the prior generation
	// from a partial set with nothing ever reopening it).
	_, deferredCorrelations, deferredMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{complete: false})
	if err == nil {
		t.Fatal("first attempt error = nil, want a corpus-fence deferral while the foreign scope is pending")
	}
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if !errors.As(err, &classified) {
		t.Fatalf("first attempt error = %v (%T), want a retryable classified deferral", err, err)
	}
	if got, want := classified.FailureClass(), ServiceCatalogCorrelationResolutionNotReadyFailureClass; got != want {
		t.Fatalf("first attempt FailureClass() = %q, want %q", got, want)
	}
	if deferredCorrelations.calls != 0 || len(deferredMaterialization.writes) != 0 {
		t.Fatalf("deferral writes = %d correlation / %d materialization, want 0/0",
			deferredCorrelations.calls, len(deferredMaterialization.writes))
	}

	// Retry once the foreign scope activates: converges to the clean run.
	// The retry must also read exactly the service repository, pinning the
	// known-before-the-read set the narrowed wait would key on.
	retryLoader := &corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true}
	if _, _, _, err := runFenceTestHandler(t, retryLoader); err != nil {
		t.Fatalf("retry error = %v, want nil once the foreign scope activates", err)
	}
	if len(retryLoader.fencedRepoIDs) != 1 || len(retryLoader.fencedRepoIDs[0]) != 1 || retryLoader.fencedRepoIDs[0][0] != "repo-checkout" {
		t.Fatalf("retry fenced read repo ids = %v, want [[repo-checkout]]", retryLoader.fencedRepoIDs)
	}
	_, wokenCorrelations, wokenMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true})
	if err != nil {
		t.Fatalf("retry error = %v, want nil once the foreign scope activates", err)
	}
	_, cleanCorrelations, cleanMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true})
	if err != nil {
		t.Fatalf("clean run error = %v, want nil", err)
	}
	if got, want := perRepoWaitServiceCatalogFingerprint(wokenCorrelations, wokenMaterialization),
		perRepoWaitServiceCatalogFingerprint(cleanCorrelations, cleanMaterialization); got != want {
		t.Fatalf("retry fingerprint = %q, clean fingerprint = %q: the woken retry must converge to the clean global run",
			got, want)
	}
}

func TestPerRepoWaitBaselineServiceCatalogUnrelatedHolderStillDefers(t *testing.T) {
	t.Parallel()

	// The fence is held by a scope whose rows never touch repo-checkout.
	// The global fence still defers with zero writes: it cannot distinguish
	// a producer from a bystander. This over-wait is the documented perf gap
	// a per-producer-scope wait may close; the narrowed replay of this seed
	// must succeed with the clean-run fingerprint instead of deferring.
	// (The service-catalog deferral carries no holding-scope list, unlike
	// the DU/workload deferrals: holder attribution for this domain is an
	// open design item, not a pinned behavior.)
	_, correlations, materialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{complete: false})
	if err == nil {
		t.Fatal("seed error = nil, want a corpus-fence deferral: the global fence waits on unrelated holders too")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("seed error = %v (%T), want a classified deferral", err, err)
	}
	if got, want := classified.FailureClass(), ServiceCatalogCorrelationResolutionNotReadyFailureClass; got != want {
		t.Fatalf("seed FailureClass() = %q, want %q", got, want)
	}
	if correlations.calls != 0 || len(materialization.writes) != 0 {
		t.Fatalf("deferral writes = %d correlation / %d materialization, want 0/0",
			correlations.calls, len(materialization.writes))
	}
}

func TestPerRepoWaitBaselineServiceCatalogAbsenceThenLateArrival(t *testing.T) {
	t.Parallel()

	// Absence: fence complete, only the own scope's row present. The handler
	// succeeds on the own-only set: deployment evidence from the own row,
	// no dependency evidence, no deferral.
	_, absentCorrelations, absentMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{fenced: []relationships.ResolvedRelationship{fenceTestOwnRelationship}, complete: true})
	if err != nil {
		t.Fatalf("absence run error = %v, want nil: a complete fence with only own rows succeeds", err)
	}
	if got := perRepoWaitServiceCatalogFingerprint(absentCorrelations, absentMaterialization); !strings.Contains(got, "services=1") {
		t.Fatalf("absence run fingerprint = %q, want one service write", got)
	}
	if len(absentMaterialization.writes) != 1 {
		t.Fatalf("absence run service writes = %d, want 1", len(absentMaterialization.writes))
	}
	if got := len(absentMaterialization.writes[0].Dependencies); got != 0 {
		t.Fatalf("absence run dependency rows = %d, want 0: the foreign dependency row has not arrived", got)
	}

	// Late arrival: the foreign scope's rows land. The converged run must
	// equal the clean full run, and the absence run's deployment evidence
	// must be a subset of it (#7258: no spurious changed-since removals).
	_, arrivedCorrelations, arrivedMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true})
	if err != nil {
		t.Fatalf("late-arrival run error = %v, want nil", err)
	}
	_, cleanCorrelations, cleanMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true})
	if err != nil {
		t.Fatalf("clean run error = %v, want nil", err)
	}
	if got, want := perRepoWaitServiceCatalogFingerprint(arrivedCorrelations, arrivedMaterialization),
		perRepoWaitServiceCatalogFingerprint(cleanCorrelations, cleanMaterialization); got != want {
		t.Fatalf("late-arrival fingerprint = %q, clean fingerprint = %q", got, want)
	}
	absentDeployment := absentMaterialization.writes[0].Deployment
	arrivedDeployment := arrivedMaterialization.writes[0].Deployment
	for _, row := range absentDeployment {
		found := false
		for _, converged := range arrivedDeployment {
			if converged.Identity == row.Identity {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("absence deployment identity %q missing after convergence: the late row must add evidence, never remove it (#7258)",
				row.Identity)
		}
	}
	if len(arrivedDeployment) <= len(absentDeployment) {
		t.Fatalf("converged deployment rows = %d, absence = %d: the foreign row must add deployment evidence",
			len(arrivedDeployment), len(absentDeployment))
	}
}
