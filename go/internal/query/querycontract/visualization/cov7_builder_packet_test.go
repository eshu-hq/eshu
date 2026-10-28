// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// TestCov7StableIDs proves node and edge IDs are deterministic, trimmed, and
// namespaced by their prefixes.
func TestCov7StableIDs(t *testing.T) {
	t.Parallel()

	first := VisualizationNodeID("repo", " r1 ", "a.go")
	second := VisualizationNodeID("repo", "r1", "a.go")
	if first != second {
		t.Fatalf("node IDs differ for trimmed-equal input: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, "viznode:") {
		t.Fatalf("node ID = %q, want viznode: prefix", first)
	}
	if other := VisualizationNodeID("repo", "r1", "b.go"); other == first {
		t.Fatalf("distinct identities share node ID %q", first)
	}
	edge := VisualizationEdgeID("n1", "n2", " CALLS ")
	if !strings.HasPrefix(edge, "vizedge:") {
		t.Fatalf("edge ID = %q, want vizedge: prefix", edge)
	}
	if again := VisualizationEdgeID("n1", "n2", "CALLS"); again != edge {
		t.Fatalf("edge IDs differ for trimmed-equal input")
	}
}

// TestCov7BuilderEmptyAndGuards proves the empty builder contract and the
// endpoint/identity guards on AddNode and AddEdge.
func TestCov7BuilderEmptyAndGuards(t *testing.T) {
	t.Parallel()

	builder := NewVisualizationBuilder(VisualizationViewServiceStory, "  story ")
	if !builder.Empty() || builder.EdgeCount() != 0 {
		t.Fatalf("fresh builder is not empty")
	}
	builder.AddNode(VisualizationNode{})
	builder.AddEdge(VisualizationEdge{Source: "", Target: "n2"})
	builder.AddEdge(VisualizationEdge{Source: "n1", Target: ""})
	if !builder.Empty() {
		t.Fatalf("guarded adds changed an empty builder")
	}
	builder.AddNode(VisualizationNode{ID: "n1", Type: "repository", Label: "r"})
	builder.AddEdge(VisualizationEdge{Source: "n1", Target: "n2", Relationship: "OWNS"})
	if builder.Empty() || builder.EdgeCount() != 1 {
		t.Fatalf("builder did not record node and edge")
	}
	packet := builder.Finalize()
	if packet.Title != "story" || packet.View != VisualizationViewServiceStory || !packet.Supported {
		t.Fatalf("packet = %#v, want trimmed title with story view", packet)
	}
	if packet.Limits.MaxNodes != VisualizationMaxNodes || packet.Limits.Ordering != "stable_id" {
		t.Fatalf("limits = %#v, want bounds with stable ordering", packet.Limits)
	}
}

// TestCov7BuilderNodeMerge proves duplicate nodes reconcile roles by
// priority, keep the deterministic scope key, strengthen truth, and merge
// evidence handles.
func TestCov7BuilderNodeMerge(t *testing.T) {
	t.Parallel()

	builder := NewVisualizationBuilder(VisualizationViewEvidenceCitation, "t")
	builder.AddNode(VisualizationNode{
		ID: "n1", Type: "repository", Label: "zebra", Role: "downstream_consumer",
		ScopeKey: "s2", TruthLabel: string(querycontract.TruthLevelFallback),
		EvidenceHandle: &evidence.EvidenceCitationHandle{Kind: "file", RepoID: "r1"},
	})
	builder.AddNode(VisualizationNode{
		ID: "n1", Type: "repository", Label: "apple", Role: "workload",
		ScopeKey: "s1", TruthLabel: string(querycontract.TruthLevelExact),
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1"},
			{Kind: "entity", EntityID: "e1"},
		},
	})
	packet := builder.Finalize()
	if len(packet.Nodes) != 1 {
		t.Fatalf("nodes = %#v, want one merged node", packet.Nodes)
	}
	node := packet.Nodes[0]
	if node.Role != "workload" || node.Category != "service" {
		t.Fatalf("node = %#v, want workload role with service category", node)
	}
	if len(node.Roles) != 2 || node.Roles[0] != "workload" {
		t.Fatalf("roles = %#v, want priority-ordered pair", node.Roles)
	}
	if node.ScopeKey != "s1" || len(node.ScopeKeys) != 2 {
		t.Fatalf("scopes = (%q, %#v), want first key with both preserved", node.ScopeKey, node.ScopeKeys)
	}
	if node.TruthLabel != string(querycontract.TruthLevelExact) {
		t.Fatalf("truth = %q, want strengthened exact label", node.TruthLabel)
	}
	if len(node.EvidenceHandles) != 2 || node.EvidenceHandle == nil {
		t.Fatalf("handles = %#v, want merged pair with primary set", node.EvidenceHandles)
	}
}

// TestCov7BuilderEdgeMerge proves duplicate edges strengthen truth and merge
// handles while fresh nodes keep their derived evidence pointer.
func TestCov7BuilderEdgeMerge(t *testing.T) {
	t.Parallel()

	builder := NewVisualizationBuilder(VisualizationViewIncidentContext, "t")
	builder.AddNode(VisualizationNode{ID: "n1"})
	builder.AddNode(VisualizationNode{
		ID:              "n2",
		EvidenceHandle:  &evidence.EvidenceCitationHandle{Kind: "file", RepoID: "r9"},
		EvidenceHandles: []evidence.EvidenceCitationHandle{{Kind: "file", RepoID: "r9"}},
	})
	builder.AddEdge(VisualizationEdge{Source: "n1", Target: "n2", Relationship: "NEXT", TruthLabel: "weak"})
	builder.AddEdge(VisualizationEdge{
		Source: "n1", Target: "n2", Relationship: "NEXT",
		TruthLabel:      string(querycontract.TruthLevelDerived),
		EvidenceHandles: []evidence.EvidenceCitationHandle{{Kind: "entity", EntityID: "e2"}},
	})
	packet := builder.Finalize()
	if len(packet.Edges) != 1 {
		t.Fatalf("edges = %#v, want one merged edge", packet.Edges)
	}
	edge := packet.Edges[0]
	if edge.TruthLabel != string(querycontract.TruthLevelDerived) {
		t.Fatalf("edge truth = %q, want strengthened derived label", edge.TruthLabel)
	}
	if len(edge.EvidenceHandles) != 1 || edge.EvidenceHandle == nil {
		t.Fatalf("edge handles = %#v, want merged handle with pointer", edge.EvidenceHandles)
	}
	if packet.Nodes[1].EvidenceHandle == nil || packet.Nodes[1].EvidenceHandle.RepoID != "r9" {
		t.Fatalf("fresh node handle = %#v, want derived primary", packet.Nodes[1].EvidenceHandle)
	}
}

// TestCov7FinalizeBounds proves node and edge caps, dangling-edge drops, and
// the explicit truncation record.
func TestCov7FinalizeBounds(t *testing.T) {
	t.Parallel()

	builder := NewVisualizationBuilder(VisualizationViewGraphQuery, "big")
	for i := 0; i < VisualizationMaxNodes+5; i++ {
		builder.AddNode(VisualizationNode{ID: VisualizationNodeID("n", fmt.Sprint(i)), Label: fmt.Sprint(i)})
	}
	kept := builder.Finalize()
	if len(kept.Nodes) != VisualizationMaxNodes || !kept.Truncation.Truncated {
		t.Fatalf("nodes = %d truncated = %v, want capped truncated packet", len(kept.Nodes), kept.Truncation.Truncated)
	}
	if kept.Truncation.DroppedNodeCount != 5 || len(kept.Truncation.DroppedNodeIDs) != 5 {
		t.Fatalf("truncation = %#v, want 5 named dropped nodes", kept.Truncation)
	}
	if len(kept.Limitations) != 1 || kept.Limits.NodeCount != VisualizationMaxNodes {
		t.Fatalf("packet = %#v, want one truncation limitation with counts", kept)
	}

	edged := NewVisualizationBuilder(VisualizationViewGraphQuery, "edges")
	edged.AddNode(VisualizationNode{ID: "a"})
	edged.AddNode(VisualizationNode{ID: "b"})
	for i := 0; i < VisualizationMaxEdges+10; i++ {
		edged.AddEdge(VisualizationEdge{Source: "a", Target: "b", Relationship: fmt.Sprintf("REL_%d", i)})
	}
	edged.AddEdge(VisualizationEdge{Source: "a", Target: "ghost", Relationship: "LOST"})
	edgedPacket := edged.Finalize()
	if len(edgedPacket.Edges) != VisualizationMaxEdges {
		t.Fatalf("edges = %d, want capped at %d", len(edgedPacket.Edges), VisualizationMaxEdges)
	}
	if edgedPacket.Truncation.DroppedEdgeCount != 11 {
		t.Fatalf("dropped edges = %d, want 10 over-limit plus 1 dangling", edgedPacket.Truncation.DroppedEdgeCount)
	}
	if edgedPacket.Limits.EdgeCount != VisualizationMaxEdges {
		t.Fatalf("edge count = %d, want retained count", edgedPacket.Limits.EdgeCount)
	}
}

// TestCov7UnsupportedPacket proves the default limitation, truth carryover,
// and custom limitation passthrough.
func TestCov7UnsupportedPacket(t *testing.T) {
	t.Parallel()

	truth := &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisContentIndex}
	packet := UnsupportedVisualizationPacket(VisualizationViewServiceStory, truth, nil, nil)
	if packet.Supported || packet.View != VisualizationViewUnsupported {
		t.Fatalf("packet = %#v, want unsupported view", packet)
	}
	if len(packet.Limitations) != 1 || len(packet.Nodes) != 0 || len(packet.Edges) != 0 {
		t.Fatalf("packet = %#v, want default limitation with no graph", packet)
	}
	if packet.Truth == truth || packet.Truth.Basis != truth.Basis {
		t.Fatalf("truth not cloned")
	}
	custom := UnsupportedVisualizationPacket(VisualizationViewServiceStory, nil,
		[]string{"custom"}, []map[string]any{{"tool": "t"}})
	if len(custom.Limitations) != 1 || custom.Limitations[0] != "custom" || len(custom.RecommendedNextCalls) != 1 {
		t.Fatalf("custom packet = %#v, want passthrough", custom)
	}
	if custom.Truth != nil {
		t.Fatalf("custom truth = %#v, want nil", custom.Truth)
	}
}

// TestCov7MergeHelpers proves role priority, category mapping, string-set
// merging, truth-label strength, and handle dedup ordering.
func TestCov7MergeHelpers(t *testing.T) {
	t.Parallel()

	if got := visualizationRolePriority("workload"); got != 5 {
		t.Fatalf("workload priority = %d, want 5", got)
	}
	if got := visualizationRolePriority("mystery"); got != 0 {
		t.Fatalf("unknown priority = %d, want 0", got)
	}
	if got := primaryVisualizationRole([]string{"a", "b"}); got != "a" {
		t.Fatalf("primary role = %q, want first", got)
	}
	categories := map[string]string{
		"workload": "service", "source_repository": "source", "runtime_instance": "runtime",
		"deployment_configuration": "deployment", "downstream_consumer": "downstream", "mystery": "",
	}
	for role, want := range categories {
		if got := visualizationCategoryForRole(role); got != want {
			t.Fatalf("category(%q) = %q, want %q", role, got, want)
		}
	}
	merged := mergeVisualizationStrings([]string{" b ", "", "a"}, []string{"a", "c"})
	if len(merged) != 3 || merged[0] != "a" || merged[2] != "c" {
		t.Fatalf("merged strings = %#v, want sorted deduped triple", merged)
	}
	if got := firstNonEmptyVisualizationString(" ", "", "x"); got != "x" {
		t.Fatalf("first non-empty = %q, want x", got)
	}
	if got := strongerVisualizationTruthLabel("", "b"); got != "b" {
		t.Fatalf("empty vs b = %q, want b", got)
	}
	if got := strongerVisualizationTruthLabel("b", "a"); got != "a" {
		t.Fatalf("tie b vs a = %q, want smaller a", got)
	}
	if got := strongerVisualizationTruthLabel(string(querycontract.TruthLevelExact), string(querycontract.TruthLevelDerived)); got != string(querycontract.TruthLevelExact) {
		t.Fatalf("exact vs derived = %q, want exact", got)
	}
	handles := mergeVisualizationEvidenceHandles(
		[]evidence.EvidenceCitationHandle{{Kind: "file", RepoID: "r2", Confidence: 0.1}},
		[]evidence.EvidenceCitationHandle{{Kind: "file", RepoID: "r1"}},
		&evidence.EvidenceCitationHandle{Kind: "file", RepoID: "r2", Confidence: 0.9},
		nil,
	)
	if len(handles) != 2 || handles[0].RepoID != "r1" || handles[1].Confidence != 0.9 {
		t.Fatalf("handles = %#v, want deduped sorted pair with last write winning", handles)
	}
	normalized := normalizeVisualizationNode(VisualizationNode{ID: "n", Role: "workload", ScopeKey: "s"})
	if normalized.Role != "workload" || normalized.Category != "service" || normalized.ScopeKey != "s" {
		t.Fatalf("normalized = %#v, want role category and scope", normalized)
	}
	presentation := mergeVisualizationNodePresentation(
		VisualizationNode{Role: "workload", Label: "z-keep", Type: "t", Category: "c"},
		VisualizationNode{Role: "workload", Label: "a-wins", Type: "t", Category: "c", CanonicalKey: "k"},
	)
	if presentation.Label != "a-wins" || presentation.CanonicalKey != "k" {
		t.Fatalf("presentation = %#v, want smaller key winning on tie", presentation)
	}
	kept := mergeVisualizationNodePresentation(
		VisualizationNode{Role: "workload", Label: "a-keep"},
		VisualizationNode{Role: "downstream_consumer", Label: "z-loses"},
	)
	if kept.Label != "a-keep" {
		t.Fatalf("presentation = %#v, want higher priority winning", kept)
	}
}
