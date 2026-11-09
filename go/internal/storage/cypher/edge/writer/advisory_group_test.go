// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"strings"
	"testing"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
)

func TestReconcileAdvisoryGroupEmptyDeletesNodeOnly(t *testing.T) {
	t.Parallel()
	exec := &recordingExecutor{}
	w := NewAdvisoryGroupWriter(exec, 500)
	stats, err := w.ReconcileAdvisoryGroup(context.Background(), advisorygroup.Group{ID: "CVE-1"})
	if err != nil {
		t.Fatalf("ReconcileAdvisoryGroup: %v", err)
	}
	if stats != (advisorygroup.WriteStats{}) {
		t.Fatalf("stats = %+v, want zero from a counter-less executor", stats)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (delete only)", len(exec.calls))
	}
	if !strings.Contains(exec.calls[0].Cypher, "DETACH DELETE") || !strings.Contains(exec.calls[0].Cypher, "AdvisoryGroup {id:") {
		t.Fatalf("delete cypher not anchored by group id: %s", exec.calls[0].Cypher)
	}
}

func TestReconcileAdvisoryGroupUpsertRetractCleanup(t *testing.T) {
	t.Parallel()
	exec := &recordingExecutor{}
	w := NewAdvisoryGroupWriter(exec, 2) // force 3 edges into 2 batches
	group := advisorygroup.Group{
		ID:          "CVE-2021-44228",
		CVEIDs:      []string{"CVE-2021-44228"},
		AdvisoryIDs: []string{"GHSA-jfh8-c2jp-5v3q"},
		Edges: []advisorygroup.Edge{
			{RepositoryID: "repo-1", PackageID: "pkg:maven/a", Ecosystem: "maven", PURL: "pkg:maven/a@1"},
			{RepositoryID: "repo-2", PackageID: "pkg:maven/a", Ecosystem: "maven", PURL: "pkg:maven/a@1"},
			{RepositoryID: "repo-3", PackageID: "pkg:maven/b", Ecosystem: "maven", PURL: "pkg:maven/b@2"},
		},
	}
	if _, err := w.ReconcileAdvisoryGroup(context.Background(), group); err != nil {
		t.Fatalf("ReconcileAdvisoryGroup: %v", err)
	}
	// 2 upsert batches + 1 stale-edge retract + 1 orphan cleanup.
	if len(exec.calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(exec.calls))
	}
	upsert := exec.calls[0].Cypher
	for _, want := range []string{"MATCH (r:Repository {id: row.repository_id})", "MERGE (g:AdvisoryGroup {id: $id})", "AFFECTED_BY {package_id: row.package_id}", "g.member_count"} {
		if !strings.Contains(upsert, want) {
			t.Errorf("upsert missing %q:\n%s", want, upsert)
		}
	}
	if strings.Contains(upsert, "MERGE (r:Repository") || strings.Contains(upsert, "MERGE (repo:Repository") {
		t.Errorf("upsert must MATCH never MERGE Repository:\n%s", upsert)
	}
	rows, ok := exec.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("first batch rows = %v, want 2 rows", exec.calls[0].Parameters["rows"])
	}
	if got := exec.calls[0].Parameters["member_count"]; got != 2 {
		t.Fatalf("member_count = %v, want 2", got)
	}
	retract := exec.calls[2].Cypher
	if !strings.Contains(retract, "AdvisoryGroup {id: $id}") || !strings.Contains(retract, "DELETE") {
		t.Errorf("retract not group-anchored:\n%s", retract)
	}
	keep, ok := exec.calls[2].Parameters["keep"].([]map[string]any)
	if !ok || len(keep) != 3 {
		t.Fatalf("keep = %v, want 3 pairs", exec.calls[2].Parameters["keep"])
	}
	cleanup := exec.calls[3].Cypher
	if !strings.Contains(cleanup, "DELETE g") {
		t.Errorf("cleanup must delete an edgeless group node:\n%s", cleanup)
	}
}

func TestReconcileAdvisoryGroupRequiresExecutorAndID(t *testing.T) {
	t.Parallel()
	w := NewAdvisoryGroupWriter(nil, 500)
	if _, err := w.ReconcileAdvisoryGroup(context.Background(), advisorygroup.Group{ID: "CVE-1"}); err == nil {
		t.Fatal("want error for nil executor")
	}
	w = NewAdvisoryGroupWriter(&recordingExecutor{}, 500)
	if _, err := w.ReconcileAdvisoryGroup(context.Background(), advisorygroup.Group{}); err == nil {
		t.Fatal("want error for blank group id")
	}
}
