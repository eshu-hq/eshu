package reducer

import (
	"testing"
)

func TestBuildRepoDependencyRowsEmpty(t *testing.T) {
	t.Parallel()
	rows := BuildRepoDependencyRows(nil, nil, nil)
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0", len(rows))
	}
}

func TestBuildRepoDependencyRowsSingleDependency(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
	}

	rows := BuildRepoDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].RepoID != "repo-a" {
		t.Fatalf("RepoID = %q, want repo-a", rows[0].RepoID)
	}
	if rows[0].TargetRepoID != "repo-b" {
		t.Fatalf("TargetRepoID = %q, want repo-b", rows[0].TargetRepoID)
	}
	if rows[0].DependencyName != "svc-b" {
		t.Fatalf("DependencyName = %q, want svc-b", rows[0].DependencyName)
	}
}

func TestBuildRepoDependencyRowsDeduplicates(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
	}

	rows := BuildRepoDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1 (deduplicated)", len(rows))
	}
}

func TestBuildRepoDependencyRowsSkipsUnknownTarget(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"unknown-svc"},
	}

	rows := BuildRepoDependencyRows(descriptors, dependenciesByRepo, nil)
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0", len(rows))
	}
}

func TestBuildWorkloadDependencyRowsEmpty(t *testing.T) {
	t.Parallel()
	rows := BuildWorkloadDependencyRows(nil, nil, nil)
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0", len(rows))
	}
}

func TestBuildWorkloadDependencyRowsSingleDependency(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].WorkloadID != "workload:svc-a" {
		t.Fatalf("WorkloadID = %q, want workload:svc-a", rows[0].WorkloadID)
	}
	if rows[0].TargetWorkloadID != "workload:svc-b" {
		t.Fatalf("TargetWorkloadID = %q, want workload:svc-b", rows[0].TargetWorkloadID)
	}
	if rows[0].RepoID != "repo-a" {
		t.Fatalf("RepoID = %q, want repo-a", rows[0].RepoID)
	}
	if rows[0].TargetRepoID != "repo-b" {
		t.Fatalf("TargetRepoID = %q, want repo-b", rows[0].TargetRepoID)
	}
}

func TestBuildWorkloadDependencyRowsDeduplicates(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1 (deduplicated)", len(rows))
	}
}

func TestBuildWorkloadDependencyRowsMultipleDependencies(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b", "svc-c"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
		"svc-c": "repo-c",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs)
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
}
