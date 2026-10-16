// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cannedRetirementSplit mirrors the live retirement shape: eleven incoming
// edges standing in for the reducer families (evidence_source test/7324,
// one per type) plus nine outgoing projector edges (six REPO_CONTAINS, three
// depth-0 CONTAINS), so the hermetic split pins the same 11 + 9 = 20 the
// live test measures on the graph.
func cannedRetirementSplit() []DroppedEdge {
	incomingTypes := []string{
		"DEPENDS_ON", "DEPLOYS_FROM", "DISCOVERS_CONFIG_IN", "PROVISIONS_DEPENDENCY_FOR",
		"USES_MODULE", "READS_CONFIG_FROM", "EVIDENCES_REPOSITORY_RELATIONSHIP", "DEPLOYMENT_SOURCE",
		"CORRELATES_DEPLOYABLE_UNIT", "PINS_SUBMODULE", "BUILT_FROM",
	}
	dropped := make([]DroppedEdge, 0, len(incomingTypes)+9)
	for _, edgeType := range incomingTypes {
		dropped = append(dropped, DroppedEdge{Direction: DroppedEdgeIncoming, Type: edgeType, EvidenceSource: "test/7324"})
	}
	for i := 0; i < 6; i++ {
		dropped = append(dropped, DroppedEdge{Direction: DroppedEdgeOutgoing, Type: "REPO_CONTAINS", EvidenceSource: "projector/canonical"})
	}
	for i := 0; i < 3; i++ {
		dropped = append(dropped, DroppedEdge{Direction: DroppedEdgeOutgoing, Type: "CONTAINS", EvidenceSource: "projector/canonical"})
	}
	return dropped
}

// An attributed retirement reports the exact per-direction/per-type split:
// the last attempt's collection wins across driver retries, the direction
// totals sum to the backend's relationships-deleted count, and the split
// counter moves per closed bucket while the outcome counter is untouched in
// shape.
func TestCanonicalNodeWriterReportsAttributedRetirementSplit(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{
		cleanupCounters:     WriteCounters{NodesDeleted: 1, RelationshipsDeleted: 20},
		cleanupDropped:      cannedRetirementSplit(),
		reportsPerStatement: 2,
	}}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	logs := h.retiredLogs(t)
	if len(logs) != 1 {
		t.Fatalf("retirement log lines = %d, want 1; logs = %s", len(logs), h.logs.String())
	}
	assertRetirementLog(t, logs[0], map[string]any{
		"level": "WARN", "nodes_deleted": float64(1), "relationships_deleted": float64(20),
		"deletes_counted": true, "outcome": telemetry.RepositoryRetirementOutcomeDroppedRelationships,
		"attribution": true, "dropped_incoming": float64(11), "dropped_outgoing": float64(9),
	})
	byType, ok := logs[0]["dropped_by_type"].(map[string]any)
	if !ok {
		t.Fatalf("dropped_by_type = %#v, want a per-type map; entry = %v", logs[0]["dropped_by_type"], logs[0])
	}
	if got := byType["incoming:DEPENDS_ON"]; got != float64(1) {
		t.Fatalf("dropped_by_type[incoming:DEPENDS_ON] = %#v, want 1; map = %v", got, byType)
	}
	if got := byType["outgoing:REPO_CONTAINS"]; got != float64(6) {
		t.Fatalf("dropped_by_type[outgoing:REPO_CONTAINS] = %#v, want 6; map = %v", got, byType)
	}
	if got := byType["outgoing:CONTAINS"]; got != float64(3) {
		t.Fatalf("dropped_by_type[outgoing:CONTAINS] = %#v, want 3; map = %v", got, byType)
	}
	// Thirteen distinct direction:type keys exceed the top-10 log cap: ten
	// named plus the overflow count.
	if got := byType["overflow_n"]; got != float64(3) {
		t.Fatalf("dropped_by_type[overflow_n] = %#v, want 3; map = %v", got, byType)
	}
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeDroppedRelationships); got != 1 {
		t.Fatalf("%s{outcome=dropped_relationships} = %d, want 1", retirementMetric, got)
	}
	// The test/7324-seeded incoming edges class as reducer, the
	// projector/canonical outgoing edges as projector; every other bucket
	// stays dark, and the bucket sum equals the backend count.
	if got := h.splitCounter(t, "incoming", "reducer"); got != 11 {
		t.Fatalf("%s{direction=incoming,class=reducer} = %d, want 11", retirementSplitMetric, got)
	}
	if got := h.splitCounter(t, "outgoing", "projector"); got != 9 {
		t.Fatalf("%s{direction=outgoing,class=projector} = %d, want 9", retirementSplitMetric, got)
	}
	for _, bucket := range [][2]string{
		{"incoming", "projector"},
		{"incoming", "other"},
		{"outgoing", "reducer"},
		{"outgoing", "other"},
	} {
		if got := h.splitCounter(t, bucket[0], bucket[1]); got != 0 {
			t.Fatalf("%s{direction=%s,class=%s} = %d, want 0",
				retirementSplitMetric, bucket[0], bucket[1], got)
		}
	}
}

// An attributed edgeless retirement stays clean at INFO with an empty split
// and moves the outcome counter only: there is nothing for the split
// counter to count.
func TestCanonicalNodeWriterReportsAttributedEdgelessRetirementClean(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{
		cleanupCounters: WriteCounters{NodesDeleted: 1},
		cleanupDropped:  []DroppedEdge{},
	}}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	logs := h.retiredLogs(t)
	if len(logs) != 1 {
		t.Fatalf("retirement log lines = %d, want 1; logs = %s", len(logs), h.logs.String())
	}
	assertRetirementLog(t, logs[0], map[string]any{
		"level": "INFO", "nodes_deleted": float64(1), "relationships_deleted": float64(0),
		"deletes_counted": true, "outcome": telemetry.RepositoryRetirementOutcomeClean,
		"attribution": true, "dropped_incoming": float64(0), "dropped_outgoing": float64(0),
	})
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean); got != 1 {
		t.Fatalf("%s{outcome=clean} = %d, want 1", retirementMetric, got)
	}
	for _, bucket := range [][2]string{
		{"incoming", "reducer"},
		{"incoming", "projector"},
		{"incoming", "other"},
		{"outgoing", "reducer"},
		{"outgoing", "projector"},
		{"outgoing", "other"},
	} {
		if got := h.splitCounter(t, bucket[0], bucket[1]); got != 0 {
			t.Fatalf("%s{direction=%s,class=%s} = %d, want 0 for an edgeless retirement",
				retirementSplitMetric, bucket[0], bucket[1], got)
		}
	}
}

func TestSummarizeDroppedEdgesClosesDirectionAndClass(t *testing.T) {
	split := summarizeDroppedEdges([]DroppedEdge{
		{Direction: "out", Type: "REPO_CONTAINS", EvidenceSource: "projector/canonical"},
		{Direction: "in", Type: "DEPENDS_ON", EvidenceSource: "reducer/deployable-unit-correlation"},
		{Direction: "in", Type: "DEPENDS_ON", EvidenceSource: "resolver/cross-repo"},
		{Direction: "in", Type: "BUILT_FROM", EvidenceSource: ""},
		// A direction outside the statement's closed CASE still counts: it
		// falls into incoming so the direction total stays exact.
		{Direction: "sideways", Type: "PINS_SUBMODULE", EvidenceSource: "test/7324"},
	})
	if split.incoming != 4 || split.outgoing != 1 {
		t.Fatalf("incoming/outgoing = %d/%d, want 4/1", split.incoming, split.outgoing)
	}
	wantBuckets := map[retiredRelationshipBucket]int64{
		{direction: "outgoing", class: "projector"}: 1,
		{direction: "incoming", class: "reducer"}:   3,
		{direction: "incoming", class: "other"}:     1,
	}
	if len(split.buckets) != len(wantBuckets) {
		t.Fatalf("buckets = %v, want %v", split.buckets, wantBuckets)
	}
	for bucket, want := range wantBuckets {
		if split.buckets[bucket] != want {
			t.Fatalf("buckets = %v, want %v", split.buckets, wantBuckets)
		}
	}
}

func TestSummarizeDroppedEdgesCapsThePerTypeLogMap(t *testing.T) {
	var dropped []DroppedEdge
	for i := 0; i < droppedEdgeTypeLogCap+4; i++ {
		dropped = append(dropped, DroppedEdge{
			Direction:      "in",
			Type:           string(rune('A' + i)),
			EvidenceSource: "test/7324",
		})
	}
	split := summarizeDroppedEdges(dropped)
	if got := len(split.byType); got != droppedEdgeTypeLogCap+1 {
		t.Fatalf("byType entries = %d, want %d (top %d plus overflow_n)", got, droppedEdgeTypeLogCap+1, droppedEdgeTypeLogCap)
	}
	if got := split.byType["overflow_n"]; got != 4 {
		t.Fatalf("byType[overflow_n] = %d, want 4", got)
	}
	// Equal counts rank alphabetically, so the first ten letters are named.
	for _, name := range []string{"incoming:A", "incoming:J"} {
		if got := split.byType[name]; got != 1 {
			t.Fatalf("byType[%s] = %d, want 1", name, got)
		}
	}
	if _, ok := split.byType["incoming:K"]; ok {
		t.Fatalf("byType carries incoming:K despite the cap: %v", split.byType)
	}
}
