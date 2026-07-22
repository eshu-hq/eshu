// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// goldenSnapshotPath returns the repo-relative path to the committed B-12
// snapshot from this package's working directory (go/cmd/golden-corpus-gate).
func goldenSnapshotPath() string {
	return filepath.Join("..", "..", "..", "testdata", "golden", "e2e-20repo-snapshot.json")
}

func TestGoldenSnapshotCodeownersQueriesUseCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	expectedRepoID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/go_comprehensive", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}

	mcpShape, ok := snap.QueryShapes.MCP["list_codeowners_ownership"]
	if !ok {
		t.Fatal("query_shapes.mcp missing list_codeowners_ownership")
	}
	if got := mcpShape.Arguments["repository_id"]; got != expectedRepoID {
		t.Fatalf("list_codeowners_ownership repository_id = %v, want canonical id %q", got, expectedRepoID)
	}

	httpKey := "GET /api/v0/codeowners/ownership?repository_id=" + expectedRepoID + "&limit=50"
	if _, ok := snap.QueryShapes.HTTP[httpKey]; !ok {
		t.Fatalf("query_shapes.http missing canonical CODEOWNERS query %q", httpKey)
	}
}

func TestLoadSnapshotParsesGoldenContract(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	if snap.SchemaVersion != "1" {
		t.Errorf("schema_version = %q, want %q", snap.SchemaVersion, "1")
	}
	if snap.CorpusID != "supply-chain-demo-20repo" {
		t.Errorf("corpus_id = %q, want supply-chain-demo-20repo", snap.CorpusID)
	}

	// Drain bounds must parse from the distinct JSON keys.
	if got := snap.DrainAssertions.FactWorkItems.Limit(); got != 0 {
		t.Errorf("fact_work_items residual limit = %d, want 0", got)
	}
	if got := snap.DrainAssertions.SharedProjectionIntents.Limit(); got != 0 {
		t.Errorf("shared_projection_intents nonterminal limit = %d, want 0", got)
	}

	// The minimal gate depends on rc-1 (deployable-unit) and rc-3 (DEPENDS_ON).
	wantRC := map[string]RequiredCorrelation{
		"rc-1": {Relationship: "CORRELATES_DEPLOYABLE_UNIT", FromLabel: "Repository", ToLabel: "Repository"},
		"rc-3": {Relationship: "DEPENDS_ON", FromLabel: "Repository", ToLabel: "Repository"},
	}
	got := map[string]RequiredCorrelation{}
	for _, rc := range snap.Graph.RequiredCorrelations {
		got[rc.ID] = rc
	}
	for id, want := range wantRC {
		rc, ok := got[id]
		if !ok {
			t.Fatalf("required_correlations missing %s", id)
		}
		if rc.Relationship != want.Relationship || rc.FromLabel != want.FromLabel || rc.ToLabel != want.ToLabel {
			t.Errorf("%s = %+v, want relationship=%s %s->%s", id, rc, want.Relationship, want.FromLabel, want.ToLabel)
		}
		if rc.MinimumCount < 1 {
			t.Errorf("%s minimum_count = %d, want >= 1", id, rc.MinimumCount)
		}
	}

	// A representative node range and edge range must parse.
	if r, ok := snap.Graph.NodeCounts["Repository"]; !ok || r.Min < 1 {
		t.Errorf("node_counts[Repository] = %+v, ok=%v", r, ok)
	}
	if r, ok := snap.Graph.EdgeCounts["DEPENDS_ON"]; !ok || r.Min < 1 {
		t.Errorf("edge_counts[DEPENDS_ON] = %+v, ok=%v", r, ok)
	}

	// Query shapes for the canonical surfaces must parse.
	if _, ok := snap.QueryShapes.MCP["list_indexed_repositories"]; !ok {
		t.Error("query_shapes.mcp missing list_indexed_repositories")
	}
	httpRepos, ok := snap.QueryShapes.HTTP["GET /api/v0/repositories"]
	if !ok {
		t.Fatal("query_shapes.http missing GET /api/v0/repositories")
	}
	if len(httpRepos.RequiredResponseFields) == 0 {
		t.Error("GET /api/v0/repositories has no required_response_fields")
	}
}

func TestGoldenSnapshotCatalogShapeProtectsWorkloadTruncation(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	const key = "GET /api/v0/catalog?limit=2000&offset=0"
	shape, ok := snap.QueryShapes.HTTP[key]
	if !ok {
		t.Fatalf("query_shapes.http missing %s", key)
	}
	for _, field := range []string{
		"repositories",
		"workloads",
		"services",
		"count",
		"limit",
		"truncated",
		"workloads_truncated",
	} {
		if !containsString(shape.RequiredResponseFields, field) {
			t.Fatalf("%s missing required response field %q", key, field)
		}
	}
}

func TestGoldenSnapshotIncludesDeadCodeReplayLibrary(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for _, key := range []string{
		"POST /api/v0/code/dead-code",
		"POST /api/v0/code/dead-code/investigate",
		"POST /api/v0/code/dead-code/cross-repo",
	} {
		shape, ok := snap.QueryShapes.HTTP[key]
		if !ok {
			t.Fatalf("query_shapes.http missing %s", key)
		}
		assertDeadCodeEnvelopeShape(t, key, shape)
		if len(shape.RequestBody) == 0 {
			t.Fatalf("%s missing request_body", key)
		}
	}

	for _, key := range []string{
		"find_dead_code",
		"investigate_dead_code",
		"find_cross_repo_dead_code",
	} {
		shape, ok := snap.QueryShapes.MCP[key]
		if !ok {
			t.Fatalf("query_shapes.mcp missing %s", key)
		}
		assertDeadCodeEnvelopeShape(t, key, shape)
		if len(shape.Arguments) == 0 {
			t.Fatalf("%s missing MCP arguments", key)
		}
	}

	crossRepoHTTP := snap.QueryShapes.HTTP["POST /api/v0/code/dead-code/cross-repo"]
	crossRepoMCP := snap.QueryShapes.MCP["find_cross_repo_dead_code"]
	for _, path := range []string{
		"data.bucket_counts.dead",
		"data.bucket_counts.live_by_consumer",
		"data.bucket_counts.unknown",
		"data.bucket_counts.suppressed",
		"data.candidate_buckets.dead[]",
		"data.candidate_buckets.live_by_consumer[].consumer_evidence[].citation",
		"data.candidate_buckets.live_by_consumer[].consumer_evidence[].confidence_label",
		"data.candidate_buckets.unknown[].needs_evidence_reasons[]",
		"data.candidate_buckets.suppressed[]",
	} {
		if !containsString(crossRepoHTTP.RequiredJSONPaths, path) {
			t.Fatalf("HTTP cross-repo dead-code shape missing bucket path %q", path)
		}
		if !containsString(crossRepoMCP.RequiredJSONPaths, path) {
			t.Fatalf("MCP cross-repo dead-code shape missing bucket path %q", path)
		}
	}
	for _, shape := range []struct {
		name  string
		shape QueryShape
	}{
		{name: "HTTP", shape: crossRepoHTTP},
		{name: "MCP", shape: crossRepoMCP},
	} {
		if got := shape.shape.RequiredJSONValues["data.query_shape"]; got != "bounded_cross_repo_dead_code" {
			t.Fatalf("%s cross-repo query_shape value = %#v, want bounded_cross_repo_dead_code", shape.name, got)
		}
		if got := shape.shape.RequiredJSONValues["data.candidate_buckets.live_by_consumer[].classification"]; got != "live_by_consumer" {
			t.Fatalf("%s cross-repo live classification value = %#v, want live_by_consumer", shape.name, got)
		}
		if got := shape.shape.RequiredJSONValues["data.candidate_buckets.unknown[].classification"]; got != "unknown_needs_evidence" {
			t.Fatalf("%s cross-repo unknown classification value = %#v, want unknown_needs_evidence", shape.name, got)
		}
	}
}

func TestGoldenSnapshotPinsDuplicateGlobalEntityResolution(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.MCP["resolve_entity"]
	if !ok {
		t.Fatal("query_shapes.mcp missing resolve_entity")
	}
	if shape.MinimumResults != 2 {
		t.Fatalf("resolve_entity minimum_results = %d, want 2", shape.MinimumResults)
	}
	for key, want := range map[string]any{
		"count":         float64(2),
		"limit":         float64(10),
		"truncated":     false,
		"entities[].id": "content-entity:e_85e904a13eae",
		"matches[].id":  "content-entity:e_85bff2c7884a",
	} {
		if got := shape.RequiredJSONValues[key]; got != want {
			t.Fatalf("resolve_entity required_json_values[%q] = %#v, want %#v", key, got, want)
		}
	}
	for _, field := range []string{"id", "entity_id", "name", "labels", "repo_id", "file_path"} {
		if !containsString(shape.ResultItemRequiredFields, field) {
			t.Fatalf("resolve_entity result item fields missing %q", field)
		}
	}
}

func TestGoldenSnapshotPinsCodeSearchOverflowAcrossHTTPAndMCP(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	for _, tc := range []struct {
		name  string
		shape QueryShape
	}{
		{name: "mcp", shape: snapshot.QueryShapes.MCP["find_code"]},
		{name: "http", shape: snapshot.QueryShapes.HTTP["POST /api/v0/code/search"]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shape.MinimumResults != 1 {
				t.Fatalf("minimum_results = %d, want 1", tc.shape.MinimumResults)
			}
			for key, want := range map[string]any{
				"count":               float64(1),
				"limit":               float64(1),
				"truncated":           true,
				"results[].entity_id": "content-entity:e_85e904a13eae",
			} {
				if got := tc.shape.RequiredJSONValues[key]; got != want {
					t.Fatalf("required_json_values[%q] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
	for key, want := range map[string]any{"query": "main", "exact": true, "limit": float64(1)} {
		if got := snapshot.QueryShapes.MCP["find_code"].Arguments[key]; got != want {
			t.Fatalf("find_code arguments[%q] = %#v, want %#v", key, got, want)
		}
		if got := snapshot.QueryShapes.HTTP["POST /api/v0/code/search"].RequestBody[key]; got != want {
			t.Fatalf("code search request_body[%q] = %#v, want %#v", key, got, want)
		}
	}
}

func TestGoldenSnapshotPinsServiceStoryVisualizationIdentityFields(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	shape, ok := snap.QueryShapes.MCP["derive_visualization_packet"]
	if !ok {
		t.Fatal("query_shapes.mcp missing derive_visualization_packet")
	}
	for _, path := range []string{
		"visualization_packet.nodes[].role",
		"visualization_packet.nodes[].roles[]",
		"visualization_packet.nodes[].canonical_key",
		"visualization_packet.nodes[].scope_key",
		"visualization_packet.nodes[].scope_keys[]",
		"visualization_packet.nodes[].evidence_handles[]",
	} {
		if !containsString(shape.RequiredJSONPaths, path) {
			t.Fatalf("derive_visualization_packet shape missing identity path %q", path)
		}
	}
	source, ok := shape.Arguments["source_response"].(map[string]any)
	if !ok {
		t.Fatalf("derive_visualization_packet source_response = %T, want map[string]any", shape.Arguments["source_response"])
	}
	graph, ok := source["evidence_graph"].(map[string]any)
	if !ok {
		t.Fatalf("derive_visualization_packet source_response.evidence_graph = %T, want map[string]any", source["evidence_graph"])
	}
	nodes, ok := graph["nodes"].([]any)
	if !ok || len(nodes) < 2 {
		t.Fatalf("derive_visualization_packet source evidence nodes = %#v, want at least two observations", graph["nodes"])
	}
	packet := query.BuildServiceStoryVisualizationPacket(source, nil)
	body, err := json.Marshal(map[string]any{"visualization_packet": packet})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if finding := EvaluateQueryShape("mcp:derive_visualization_packet", shape, body); !finding.OK {
		t.Fatalf("derive_visualization_packet golden shape failed: %s", finding.Detail)
	}
}

func assertDeadCodeEnvelopeShape(t *testing.T, key string, shape QueryShape) {
	t.Helper()
	if !shape.Envelope {
		t.Fatalf("%s must preserve the truth envelope", key)
	}
	for _, field := range []string{"data", "truth", "error"} {
		if !containsString(shape.RequiredResponseFields, field) {
			t.Fatalf("%s missing required response field %q", key, field)
		}
	}
	if got := shape.RequiredJSONValues["truth.level"]; got != "derived" {
		t.Fatalf("%s truth.level = %#v, want derived", key, got)
	}
	if got := shape.RequiredJSONValues["truth.basis"]; got != "hybrid" {
		t.Fatalf("%s truth.basis = %#v, want hybrid", key, got)
	}
}

func TestCountRangeContains(t *testing.T) {
	r := CountRange{Min: 1, Max: 20}
	cases := []struct {
		n    int64
		want bool
	}{{0, false}, {1, true}, {10, true}, {20, true}, {21, false}}
	for _, c := range cases {
		if got := r.Contains(c.n); got != c.want {
			t.Errorf("Contains(%d) = %v, want %v", c.n, got, c.want)
		}
	}
}

func TestEvidenceNarrowedCorrelationsRequireSourceTool(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for _, rc := range snap.Graph.RequiredCorrelations {
		if len(rc.EvidenceKinds) == 0 {
			continue
		}
		if !containsString(rc.RequiredEdgeProperties, "source_tool") {
			t.Errorf("%s evidence-filtered correlation must require source_tool", rc.ID)
			continue
		}
		if len(rc.AllowedEdgePropertyValues["source_tool"]) == 0 {
			t.Errorf("%s source_tool assertion must pin allowed values", rc.ID)
		}
	}
}

func TestGoldenSnapshotCoversEveryMCPTool(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for _, tool := range mcp.ReadOnlyTools() {
		if _, ok := snap.QueryShapes.MCP[tool.Name]; !ok {
			t.Errorf("query_shapes.mcp missing %s", tool.Name)
		}
	}
	for name := range snap.QueryShapes.MCP {
		if !mcpToolExists(name) {
			t.Errorf("query_shapes.mcp has stale tool %s", name)
		}
	}
}

func TestGoldenSnapshotMCPShapesAssertResponseFields(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for name, shape := range snap.QueryShapes.MCP {
		if shape.ExpectedErrorContains != "" {
			continue
		}
		if len(shape.RequiredResponseFields) == 0 {
			t.Errorf("query_shapes.mcp[%s] has no required_response_fields", name)
		}
	}
}

func TestGoldenSnapshotCLIShapesAssertParityMetadata(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	want := []string{
		"eshu list",
		"eshu index-status",
		"eshu trace service --json",
		"eshu playbooks list",
		"eshu vuln-scan repo --json",
		"eshu component inventory --json",
		"eshu hosted-onboard --json",
	}
	for _, key := range want {
		shape, ok := snap.QueryShapes.CLI[key]
		if !ok {
			t.Fatalf("query_shapes.cli missing %s", key)
		}
		if len(shape.Command) == 0 {
			t.Fatalf("query_shapes.cli[%s] missing command argv", key)
		}
		if shape.TruthClass == "" {
			t.Fatalf("query_shapes.cli[%s] missing truth_class", key)
		}
		if len(shape.RequiredResponseFields) == 0 {
			t.Fatalf("query_shapes.cli[%s] missing required_response_fields", key)
		}
	}
	for _, key := range []string{
		"eshu list",
		"eshu trace service --json",
		"eshu playbooks list",
		"eshu vuln-scan repo --json",
		"eshu component inventory --json",
		"eshu hosted-onboard --json",
	} {
		if len(snap.QueryShapes.CLI[key].ParityWith) == 0 {
			t.Fatalf("shared query_shapes.cli[%s] missing parity_with", key)
		}
	}

	var report Report
	EvaluateQuerySurfaceParity(snap, &report)
	if report.Failed() {
		t.Fatalf("query surface parity failed: %+v", report.Findings)
	}
}

func mcpToolExists(name string) bool {
	for _, tool := range mcp.ReadOnlyTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLoadSnapshotMissingFile(t *testing.T) {
	if _, err := LoadSnapshot(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing snapshot file")
	}
}
