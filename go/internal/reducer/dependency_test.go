// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workloadid"
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
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
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
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
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
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
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
	rows := BuildWorkloadDependencyRows(nil, nil, nil, nil)
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0", len(rows))
	}
}

func TestBuildWorkloadDependencyRowsSingleDependency(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs, nil)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].WorkloadID != "workload:repo-a:svc-a" {
		t.Fatalf("WorkloadID = %q, want workload:repo-a:svc-a", rows[0].WorkloadID)
	}
	if rows[0].TargetWorkloadID != "workload:repo-b:svc-b" {
		t.Fatalf("TargetWorkloadID = %q, want workload:repo-b:svc-b", rows[0].TargetWorkloadID)
	}
	if rows[0].RepoID != "repo-a" {
		t.Fatalf("RepoID = %q, want repo-a", rows[0].RepoID)
	}
	if rows[0].TargetRepoID != "repo-b" {
		t.Fatalf("TargetRepoID = %q, want repo-b", rows[0].TargetRepoID)
	}
}

func TestBuildWorkloadDependencyRowsDropsBlankDepName(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {""},
	}
	// Blank depName maps to a non-empty repo so the row reaches the
	// empty-id guard instead of the unknown-target drop above it.
	targetRepoIDs := map[string]string{
		"": "repo-b",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs, nil)
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0 (blank depName must not emit rows)", len(rows))
	}
}

// The blank-depName drop above must be counted in the tally that feeds
// eshu_dp_workload_identity_rejected_total: a drop is counted, never silent
// (#5385).
func TestBuildWorkloadDependencyRowsTalliesBlankDepName(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"", "svc:b"},
	}
	targetRepoIDs := map[string]string{
		"":      "repo-b",
		"svc:b": "repo-b",
	}
	var tally workloadid.RejectionTally
	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs, &tally)
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0 (blank and colon names must not emit rows)", len(rows))
	}
	if tally.BlankSegment != 1 {
		t.Errorf("tally.BlankSegment = %d, want 1", tally.BlankSegment)
	}
	if tally.ColonInName != 1 {
		t.Errorf("tally.ColonInName = %d, want 1", tally.ColonInName)
	}
}

func TestBuildWorkloadDependencyRowsDeduplicates(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs, nil)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1 (deduplicated)", len(rows))
	}
}

func TestBuildWorkloadDependencyRowsMultipleDependencies(t *testing.T) {
	t.Parallel()
	descriptors := []RepoDescriptor{
		{RepoID: "repo-a", RepoName: "svc-a", WorkloadID: "workload:repo-a:svc-a"},
	}
	dependenciesByRepo := map[string][]string{
		"repo-a": {"svc-b", "svc-c"},
	}
	targetRepoIDs := map[string]string{
		"svc-b": "repo-b",
		"svc-c": "repo-c",
	}

	rows := BuildWorkloadDependencyRows(descriptors, dependenciesByRepo, targetRepoIDs, nil)
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
}
