// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"strings"
	"testing"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
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
	if !strings.Contains(exec.calls[0].Cypher, "DELETE e, g") || !strings.Contains(exec.calls[0].Cypher, "AdvisoryGroup {id:") || !strings.Contains(exec.calls[0].Cypher, "AFFECTED_BY") {
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
	// 1 node create + 1 node update + 2 edge merges + 2 edge updates +
	// 1 stale-edge retract + 1 orphan cleanup.
	if len(exec.calls) != 8 {
		t.Fatalf("calls = %d, want 8", len(exec.calls))
	}
	for i, call := range exec.calls {
		if strings.Contains(call.Cypher, "FOREACH") {
			t.Errorf("call %d must not rely on FOREACH routing (#11161):\n%s", i, call.Cypher)
		}
	}
	nodeCreate := exec.calls[0].Cypher
	for _, want := range []string{"MERGE (g:AdvisoryGroup {id: $id})", "ON CREATE SET", "g.member_count"} {
		if !strings.Contains(nodeCreate, want) {
			t.Errorf("node create missing %q:\n%s", want, nodeCreate)
		}
	}
	if got := exec.calls[0].Parameters["member_count"]; got != 2 {
		t.Fatalf("member_count = %v, want 2", got)
	}
	nodeUpdate := exec.calls[1].Cypher
	for _, want := range []string{"MATCH (g:AdvisoryGroup {id: $id})", "coalesce(g.cve_ids, []) <> $cve_ids", "SET g.cve_ids"} {
		if !strings.Contains(nodeUpdate, want) {
			t.Errorf("node update missing %q:\n%s", want, nodeUpdate)
		}
	}
	if strings.Contains(nodeUpdate, "MERGE") {
		t.Errorf("node update must not MERGE after the gate:\n%s", nodeUpdate)
	}
	for _, merge := range []string{exec.calls[2].Cypher, exec.calls[3].Cypher} {
		for _, want := range []string{"MATCH (r:Repository {id: row.repository_id})", "MERGE (g:AdvisoryGroup {id: $id})", "MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)"} {
			if !strings.Contains(merge, want) {
				t.Errorf("edge merge missing %q:\n%s", want, merge)
			}
		}
		if strings.Contains(merge, "MERGE (r:Repository") || strings.Contains(merge, "MERGE (repo:Repository") {
			t.Errorf("edge merge must MATCH never MERGE its Repository endpoint:\n%s", merge)
		}
		if strings.Contains(merge, "FOREACH") || strings.Contains(merge, "WITH") || strings.Contains(merge, "SET") {
			t.Errorf("edge merge must carry no clause after the MERGEs (#11161):\n%s", merge)
		}
		if !strings.HasPrefix(strings.TrimSpace(merge), "UNWIND $rows AS row") {
			t.Errorf("edge merge must open UNWIND-first:\n%s", merge)
		}
	}
	for _, update := range []string{exec.calls[4].Cypher, exec.calls[5].Cypher} {
		for _, want := range []string{"AFFECTED_BY {package_id: row.package_id}", "coalesce(e.ecosystem, '') <> row.ecosystem", "SET e.ecosystem"} {
			if !strings.Contains(update, want) {
				t.Errorf("edge update missing %q:\n%s", want, update)
			}
		}
		if strings.Contains(update, "MERGE") {
			t.Errorf("edge update must not MERGE before the gate (#11161):\n%s", update)
		}
		if !strings.HasPrefix(strings.TrimSpace(update), "UNWIND $rows AS row") {
			t.Errorf("edge update must open UNWIND-first:\n%s", update)
		}
	}
	rows, ok := exec.calls[2].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("first batch rows = %v, want 2 rows", exec.calls[2].Parameters["rows"])
	}
	retract := exec.calls[6].Cypher
	if !strings.Contains(retract, "AdvisoryGroup {id: $id}") || !strings.Contains(retract, "DELETE") {
		t.Errorf("retract not group-anchored:\n%s", retract)
	}
	if !strings.Contains(retract, "IN $keepkeys") || strings.Contains(retract, "ANY") {
		t.Errorf("retract must select the keepkeys complement with IN, never ANY (#11161):\n%s", retract)
	}
	keepkeys, ok := exec.calls[6].Parameters["keepkeys"].([]string)
	if !ok || len(keepkeys) != 3 {
		t.Fatalf("keepkeys = %v, want 3 pair keys", exec.calls[6].Parameters["keepkeys"])
	}
	for _, want := range []string{"repo-1|pkg:maven/a", "repo-2|pkg:maven/a", "repo-3|pkg:maven/b"} {
		found := false
		for _, key := range keepkeys {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("keepkeys missing %q: %v", want, keepkeys)
		}
	}
	cleanup := exec.calls[7].Cypher
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

// advisoryCountingExecutor reports fixed Bolt counters per statement
// template, once per Execute, as the production Bolt seam does when a
// statement commits.
type advisoryCountingExecutor struct {
	counters map[string]sourcecypher.WriteCounters
}

func (e *advisoryCountingExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, e.counters[stmt.Cypher])
	return nil
}

// TestReconcileAdvisoryGroupWriteStats pins the counter accounting the
// handler's telemetry and the EPSS-isolation assertion read: stats sum the
// per-statement Bolt counters across the reconcile's statements.
func TestReconcileAdvisoryGroupWriteStats(t *testing.T) {
	t.Parallel()
	exec := &advisoryCountingExecutor{
		counters: map[string]sourcecypher.WriteCounters{
			AdvisoryGroupNodeCreateCypher:       {NodesCreated: 1, PropertiesSet: 3},
			AdvisoryGroupNodeUpdateCypher:       {},
			AdvisoryGroupEdgeMergeCypher:        {RelationshipsCreated: 2},
			AdvisoryGroupEdgeUpdateCypher:       {PropertiesSet: 4},
			AdvisoryGroupStaleEdgeRetractCypher: {RelationshipsDeleted: 1},
			AdvisoryGroupDeleteEdgelessCypher:   {},
			AdvisoryGroupEmptyRetractCypher:     {NodesDeleted: 1, RelationshipsDeleted: 2},
		},
	}
	w := NewAdvisoryGroupWriter(exec, 500)
	stats, err := w.ReconcileAdvisoryGroup(context.Background(), advisorygroup.Group{
		ID:     "CVE-1",
		CVEIDs: []string{"CVE-1"},
		Edges: []advisorygroup.Edge{
			{RepositoryID: "repo-1", PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
			{RepositoryID: "repo-2", PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
		},
	})
	if err != nil {
		t.Fatalf("ReconcileAdvisoryGroup: %v", err)
	}
	want := advisorygroup.WriteStats{NodesCreated: 1, EdgesCreated: 2, EdgesDeleted: 1, PropertiesSet: 7}
	if stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	stats, err = w.ReconcileAdvisoryGroup(context.Background(), advisorygroup.Group{ID: "CVE-1"})
	if err != nil {
		t.Fatalf("empty ReconcileAdvisoryGroup: %v", err)
	}
	want = advisorygroup.WriteStats{NodesDeleted: 1, EdgesDeleted: 2}
	if stats != want {
		t.Fatalf("empty stats = %+v, want %+v", stats, want)
	}
}
