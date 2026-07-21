// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

type fakeWorkloadInstanceRetractionLookup struct {
	instances     []ExistingWorkloadInstance
	calledRepoIDs []string
	calledSource  string
	err           error
}

func (f *fakeWorkloadInstanceRetractionLookup) ListWorkloadInstances(
	_ context.Context,
	repoIDs []string,
	evidenceSource string,
) ([]ExistingWorkloadInstance, error) {
	f.calledRepoIDs = append([]string(nil), repoIDs...)
	f.calledSource = evidenceSource
	if f.err != nil {
		return nil, f.err
	}
	return append([]ExistingWorkloadInstance(nil), f.instances...), nil
}

func TestReconcileWorkloadInstanceRetractionFindsSupersededAliasKey(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-api", InstanceID: "workload-instance:api:production"},
		},
	}
	descriptors := []RepoDescriptor{
		{RepoID: "repo-api", RepoName: "api", WorkloadID: "workload:api"},
	}
	instanceRows := []InstanceRow{
		{RepoID: "repo-api", InstanceID: "workload-instance:api:prod", WorkloadID: "workload:api"},
	}

	repoIDs, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if got, want := len(superseded), 1; got != want {
		t.Fatalf("len(superseded) = %d, want %d (superseded=%v)", got, want, superseded)
	}
	if got, want := superseded[0], "workload-instance:api:production"; got != want {
		t.Fatalf("superseded[0] = %q, want %q", got, want)
	}
	if got, want := len(repoIDs), 1; got != want || repoIDs[0] != "repo-api" {
		t.Fatalf("repoIDs = %v, want [repo-api]", repoIDs)
	}
	if got, want := lookup.calledSource, EvidenceSourceWorkloads; got != want {
		t.Fatalf("evidence source = %q, want %q", got, want)
	}
	if got, want := len(lookup.calledRepoIDs), 1; got != want || lookup.calledRepoIDs[0] != "repo-api" {
		t.Fatalf("calledRepoIDs = %v, want [repo-api]", lookup.calledRepoIDs)
	}
}

func TestReconcileWorkloadInstanceRetractionKeepsMatchingCurrentInstance(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-api", InstanceID: "workload-instance:api:prod"},
		},
	}
	descriptors := []RepoDescriptor{
		{RepoID: "repo-api", RepoName: "api", WorkloadID: "workload:api"},
	}
	instanceRows := []InstanceRow{
		{RepoID: "repo-api", InstanceID: "workload-instance:api:prod", WorkloadID: "workload:api"},
	}

	_, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if len(superseded) != 0 {
		t.Fatalf("superseded = %v, want none: an instance already written this generation must never be retracted", superseded)
	}
}

// TestReconcileWorkloadInstanceRetractionNeverCrossesRepoScope proves the
// defense-in-depth repo-ownership recheck: even if a Lookup implementation
// returns a row for a repository outside the current materialization pass (a
// bug an adapter could introduce), ReconcileWorkloadInstanceRetraction must
// not fold it into the retract set. This mirrors the same safety property
// currentWorkloadDependencyRepoIDs enforces for workload dependency edges.
func TestReconcileWorkloadInstanceRetractionNeverCrossesRepoScope(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-api", InstanceID: "workload-instance:api:production"},
			// Out-of-scope row: a different repository this pass did not
			// materialize. A correct Lookup would never return this (it filters
			// by repo_id), but the reconcile function must not trust that alone.
			{RepoID: "repo-unrelated", InstanceID: "workload-instance:other:production"},
		},
	}
	descriptors := []RepoDescriptor{
		{RepoID: "repo-api", RepoName: "api", WorkloadID: "workload:api"},
	}
	instanceRows := []InstanceRow{
		{RepoID: "repo-api", InstanceID: "workload-instance:api:prod", WorkloadID: "workload:api"},
	}

	_, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if got, want := len(superseded), 1; got != want {
		t.Fatalf("len(superseded) = %d, want %d (superseded=%v)", got, want, superseded)
	}
	if got, want := superseded[0], "workload-instance:api:production"; got != want {
		t.Fatalf("superseded[0] = %q, want %q", got, want)
	}
	for _, id := range superseded {
		if id == "workload-instance:other:production" {
			t.Fatalf("retraction crossed repo scope: superseded = %v", superseded)
		}
	}
}

// TestReconcileWorkloadInstanceRetractionSkipsRepoWithZeroInstanceRowsThisPass
// is the regression test for CRITICAL 1 (round-2 review of #5473): a
// repository can have a RepoDescriptor this pass (its workload candidate
// passed classification/confidence) while contributing ZERO InstanceRows --
// for example a transient gap in environment resolution, which is possible
// for any of the seven environment-alias evidence classes. Before the fix,
// ReconcileWorkloadInstanceRetraction built `current` as one flat GLOBAL set
// of instance ids, so a repo contributing zero rows had none of its ids in
// that set and every one of its EXISTING instances (deterministically, with
// no concurrency involved) was folded into `superseded` -- wiping a repo's
// entire instance set on a transient partial materialization. The fix
// requires POSITIVE evidence (a non-empty instance-row set for that specific
// repo this pass) before treating absence-from-current as supersession.
func TestReconcileWorkloadInstanceRetractionSkipsRepoWithZeroInstanceRowsThisPass(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-a", InstanceID: "workload-instance:api:prod"},
			{RepoID: "repo-a", InstanceID: "workload-instance:api:stage"},
		},
	}
	// repoA has a descriptor this pass (its candidate/workload is still
	// present) but zero InstanceRows -- e.g. environment resolution came back
	// empty this pass for every one of repoA's environment-alias evidence
	// classes. This is "environment unresolved this pass", not "workload
	// genuinely absent".
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "api", WorkloadID: "workload:api"},
	}
	var instanceRows []InstanceRow // zero instance rows for repo-a this pass

	repoIDs, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if len(superseded) != 0 {
		t.Fatalf(
			"superseded = %v, want none: repo-a produced zero instance rows this pass, "+
				"which is insufficient positive evidence to retract its prior instances",
			superseded,
		)
	}
	if got, want := len(repoIDs), 1; got != want || repoIDs[0] != "repo-a" {
		t.Fatalf("repoIDs = %v, want [repo-a]", repoIDs)
	}
}

// TestReconcileWorkloadInstanceRetractionPositiveEvidenceIsPerRepo proves the
// zero-instance-rows guard is scoped per repository, not global: repoB
// produces instance rows this pass (positive evidence) and its superseded
// instance is retracted, while repoA -- in the SAME materialization pass, with
// zero instance rows -- keeps every one of its existing instances untouched.
func TestReconcileWorkloadInstanceRetractionPositiveEvidenceIsPerRepo(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-a", InstanceID: "workload-instance:api:prod"},
			{RepoID: "repo-b", InstanceID: "workload-instance:web:production"},
		},
	}
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "api", WorkloadID: "workload:api"},
		{RepoID: "repo-b", RepoName: "web", WorkloadID: "workload:web"},
	}
	instanceRows := []InstanceRow{
		// repo-a: zero rows this pass (transient environment-resolution gap).
		// repo-b: canonicalized environment, positive evidence.
		{RepoID: "repo-b", InstanceID: "workload-instance:web:prod", WorkloadID: "workload:web"},
	}

	_, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if got, want := len(superseded), 1; got != want {
		t.Fatalf("len(superseded) = %d, want %d (superseded=%v)", got, want, superseded)
	}
	if got, want := superseded[0], "workload-instance:web:production"; got != want {
		t.Fatalf("superseded[0] = %q, want %q", got, want)
	}
	for _, id := range superseded {
		if id == "workload-instance:api:prod" {
			t.Fatalf("repo-a instance retracted despite zero positive-evidence instance rows: superseded = %v", superseded)
		}
	}
}

func TestReconcileWorkloadInstanceRetractionNoDescriptorsIsNoop(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-api", InstanceID: "workload-instance:api:production"},
		},
	}

	repoIDs, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), nil, nil, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if superseded != nil {
		t.Fatalf("superseded = %v, want nil", superseded)
	}
	if repoIDs != nil {
		t.Fatalf("repoIDs = %v, want nil", repoIDs)
	}
	if lookup.calledRepoIDs != nil {
		t.Fatalf("lookup should not be called with no descriptors, calledRepoIDs = %v", lookup.calledRepoIDs)
	}
}

func TestReconcileWorkloadInstanceRetractionNilLookupIsNoop(t *testing.T) {
	t.Parallel()

	descriptors := []RepoDescriptor{{RepoID: "repo-api", WorkloadID: "workload:api"}}
	repoIDs, superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, nil, nil)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if superseded != nil {
		t.Fatalf("superseded = %v, want nil", superseded)
	}
	if repoIDs != nil {
		t.Fatalf("repoIDs = %v, want nil", repoIDs)
	}
}
