// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCallGraphMetricsEdgesCypherUsesOneRepoIndexedEdgePass(t *testing.T) {
	t.Parallel()

	cypher, params := callGraphMetricsEdgesCypher(" repo-1 ")
	if got, want := params["repo_id"], "repo-1"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
	if !strings.Contains(cypher, "MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})") {
		t.Fatalf("cypher = %q, want one repo-indexed CALLS pass", cypher)
	}
	for _, forbidden := range []string{"OPTIONAL MATCH", "REPO_CONTAINS", "SKIP $offset", "LIMIT $limit"} {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("cypher = %q, must not contain %q", cypher, forbidden)
		}
	}
}

func TestCallGraphMetricsRowsAggregatesHubFunctionsExactly(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		callGraphMetricEdgeRow("fn-a", "a.go", "go", "same", 10, "fn-b", "b.go", "go", "same", 20),
		callGraphMetricEdgeRow("fn-a", "a.go", "go", "same", 10, "fn-b", "b.go", "go", "same", 20),
		callGraphMetricEdgeRow("fn-b", "b.go", "go", "same", 20, "fn-a", "a.go", "go", "same", 10),
		callGraphMetricEdgeRow("fn-c", "c.go", "go", "self", 30, "fn-c", "c.go", "go", "self", 30),
		callGraphMetricEdgeRow("fn-d", "d.go", "rust", "other", 40, "fn-a", "a.go", "go", "same", 10),
	}
	req := callGraphMetricsRequest{
		MetricType: "hub_functions",
		RepoID:     "repo-1",
		Language:   "go",
		Limit:      intPtr(2),
	}

	rows := callGraphMetricsRows(req, edges)
	if got, want := len(rows), 3; got != want {
		t.Fatalf("len(rows) = %d, want limit+1 %d; rows=%#v", got, want, rows)
	}
	if got, want := []string{
		StringVal(rows[0], "function_id"),
		StringVal(rows[1], "function_id"),
		StringVal(rows[2], "function_id"),
	}, []string{"fn-a", "fn-b", "fn-c"}; !slices.Equal(got, want) {
		t.Fatalf("function order = %#v, want %#v", got, want)
	}
	if got, want := IntVal(rows[0], "incoming_calls"), 2; got != want {
		t.Fatalf("fn-a incoming_calls = %d, want distinct callers %d", got, want)
	}
	if got, want := IntVal(rows[0], "outgoing_calls"), 1; got != want {
		t.Fatalf("fn-a outgoing_calls = %d, want distinct callees %d", got, want)
	}
	if got, want := IntVal(rows[2], "total_degree"), 2; got != want {
		t.Fatalf("self total_degree = %d, want %d", got, want)
	}
}

func TestCallGraphMetricsRowsFindsUniqueRecursivePairs(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		callGraphMetricEdgeRow("fn-a", "a.go", "go", "a", 10, "fn-b", "b.go", "go", "b", 20),
		callGraphMetricEdgeRow("fn-a", "a.go", "go", "a", 10, "fn-b", "b.go", "go", "b", 20),
		callGraphMetricEdgeRow("fn-b", "b.go", "go", "b", 20, "fn-a", "a.go", "go", "a", 10),
		callGraphMetricEdgeRow("fn-c", "c.go", "go", "c", 30, "fn-c", "c.go", "go", "c", 30),
		callGraphMetricEdgeRow("fn-d", "d.go", "go", "d", 40, "fn-e", "e.go", "go", "e", 50),
		callGraphMetricEdgeRow("fn-r1", "r1.rs", "rust", "r1", 1, "fn-r2", "r2.rs", "rust", "r2", 2),
		callGraphMetricEdgeRow("fn-r2", "r2.rs", "rust", "r2", 2, "fn-r1", "r1.rs", "rust", "r1", 1),
	}
	req := callGraphMetricsRequest{
		MetricType: "recursive_functions",
		RepoID:     "repo-1",
		Language:   "go",
		Limit:      intPtr(25),
	}

	rows := callGraphMetricsRows(req, edges)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d; rows=%#v", got, want, rows)
	}
	if got, want := []string{
		StringVal(rows[0], "function_id") + ":" + StringVal(rows[0], "partner_id"),
		StringVal(rows[1], "function_id") + ":" + StringVal(rows[1], "partner_id"),
	}, []string{"fn-a:fn-b", "fn-c:fn-c"}; !slices.Equal(got, want) {
		t.Fatalf("recursive pairs = %#v, want %#v", got, want)
	}
}

func TestCallGraphMetricsRowsReturnsEmptyPageForEmptyGraph(t *testing.T) {
	t.Parallel()

	rows := callGraphMetricsRows(callGraphMetricsRequest{RepoID: "repo-1"}, nil)
	if rows == nil {
		t.Fatal("rows = nil, want non-nil empty page")
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestCallGraphMetricsRowsPagesDeterministicExactTies(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		callGraphMetricEdgeRow("fn-c", "same.go", "go", "same", 10, "fn-c", "same.go", "go", "same", 10),
		callGraphMetricEdgeRow("fn-a", "same.go", "go", "same", 10, "fn-a", "same.go", "go", "same", 10),
		callGraphMetricEdgeRow("fn-b", "same.go", "go", "same", 10, "fn-b", "same.go", "go", "same", 10),
	}
	rows := callGraphMetricsRows(callGraphMetricsRequest{
		RepoID: "repo-1",
		Limit:  intPtr(1),
		Offset: 1,
	}, edges)
	if got, want := []string{
		StringVal(rows[0], "function_id"),
		StringVal(rows[1], "function_id"),
	}, []string{"fn-b", "fn-c"}; !slices.Equal(got, want) {
		t.Fatalf("paged tie order = %#v, want %#v", got, want)
	}
}

func TestCallGraphMetricsRowsPagesRecursivePartnerIDTies(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		callGraphMetricEdgeRow("fn-a", "same.go", "go", "same", 10, "fn-d", "partner.go", "go", "partner", 20),
		callGraphMetricEdgeRow("fn-d", "partner.go", "go", "partner", 20, "fn-a", "same.go", "go", "same", 10),
		callGraphMetricEdgeRow("fn-a", "same.go", "go", "same", 10, "fn-b", "partner.go", "go", "partner", 20),
		callGraphMetricEdgeRow("fn-b", "partner.go", "go", "partner", 20, "fn-a", "same.go", "go", "same", 10),
		callGraphMetricEdgeRow("fn-a", "same.go", "go", "same", 10, "fn-c", "partner.go", "go", "partner", 20),
		callGraphMetricEdgeRow("fn-c", "partner.go", "go", "partner", 20, "fn-a", "same.go", "go", "same", 10),
	}
	rows := callGraphMetricsRows(callGraphMetricsRequest{
		MetricType: "recursive_functions",
		RepoID:     "repo-1",
		Limit:      intPtr(1),
		Offset:     1,
	}, edges)
	if got, want := []string{
		StringVal(rows[0], "partner_id"),
		StringVal(rows[1], "partner_id"),
	}, []string{"fn-c", "fn-d"}; !slices.Equal(got, want) {
		t.Fatalf("paged recursive partner order = %#v, want %#v", got, want)
	}
}

func TestCallGraphMetricsDataRecordsExpansionAndResultTelemetry(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("test").Start(context.Background(), "call-graph-test")
	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		_ string,
		_ map[string]any,
	) ([]map[string]any, error) {
		return []map[string]any{
			callGraphMetricEdgeRow("fn-a", "a.go", "go", "a", 1, "fn-b", "b.go", "go", "b", 2),
			callGraphMetricEdgeRow("fn-a", "a.go", "go", "a", 1, "fn-b", "b.go", "go", "b", 2),
			callGraphMetricEdgeRow("fn-b", "b.go", "go", "b", 2, "fn-a", "a.go", "go", "a", 1),
		}, nil
	}}}
	data, err := handler.callGraphMetricsData(ctx, callGraphMetricsRequest{
		RepoID: "repo-1",
		Limit:  intPtr(1),
	})
	if err != nil {
		t.Fatalf("callGraphMetricsData() error = %v, want nil", err)
	}
	span.End()
	if got, want := IntVal(data, "count"), 1; got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	attributes := make(map[string]any)
	for _, spanAttribute := range recorder.Ended()[0].Attributes() {
		attributes[string(spanAttribute.Key)] = spanAttribute.Value.AsInterface()
	}
	for key, want := range map[string]any{
		"eshu.query.call_graph.metric_type":         "hub_functions",
		"eshu.query.call_graph.expanded_edge_count": int64(3),
		"eshu.query.call_graph.expanded_node_count": int64(2),
		"eshu.query.call_graph.result_count":        int64(1),
		"eshu.query.call_graph.truncated":           true,
	} {
		if got := attributes[key]; got != want {
			t.Fatalf("span attribute %s = %#v, want %#v; attributes=%#v", key, got, want, attributes)
		}
	}
}

func callGraphMetricEdgeRow(
	sourceID string,
	sourcePath string,
	sourceLanguage string,
	sourceName string,
	sourceLine int,
	targetID string,
	targetPath string,
	targetLanguage string,
	targetName string,
	targetLine int,
) map[string]any {
	return map[string]any{
		"source_id":         sourceID,
		"source_path":       sourcePath,
		"source_language":   sourceLanguage,
		"source_name":       sourceName,
		"source_start_line": sourceLine,
		"source_end_line":   sourceLine + 1,
		"target_id":         targetID,
		"target_path":       targetPath,
		"target_language":   targetLanguage,
		"target_name":       targetName,
		"target_start_line": targetLine,
		"target_end_line":   targetLine + 1,
	}
}
