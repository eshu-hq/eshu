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

	superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if got, want := len(superseded), 1; got != want {
		t.Fatalf("len(superseded) = %d, want %d (superseded=%v)", got, want, superseded)
	}
	if got, want := superseded[0], "workload-instance:api:production"; got != want {
		t.Fatalf("superseded[0] = %q, want %q", got, want)
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

	superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
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

	superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, instanceRows, lookup)
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

func TestReconcileWorkloadInstanceRetractionNoDescriptorsIsNoop(t *testing.T) {
	t.Parallel()

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-api", InstanceID: "workload-instance:api:production"},
		},
	}

	superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), nil, nil, lookup)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if superseded != nil {
		t.Fatalf("superseded = %v, want nil", superseded)
	}
	if lookup.calledRepoIDs != nil {
		t.Fatalf("lookup should not be called with no descriptors, calledRepoIDs = %v", lookup.calledRepoIDs)
	}
}

func TestReconcileWorkloadInstanceRetractionNilLookupIsNoop(t *testing.T) {
	t.Parallel()

	descriptors := []RepoDescriptor{{RepoID: "repo-api", WorkloadID: "workload:api"}}
	superseded, err := ReconcileWorkloadInstanceRetraction(context.Background(), descriptors, nil, nil)
	if err != nil {
		t.Fatalf("ReconcileWorkloadInstanceRetraction() error = %v", err)
	}
	if superseded != nil {
		t.Fatalf("superseded = %v, want nil", superseded)
	}
}
