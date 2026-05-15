package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordingResourceInvestigationGraph struct {
	mu           sync.Mutex
	runCalls     []resourceInvestigationRunCall
	runRows      [][]map[string]any
	workloadRows []map[string]any
	incomingRows []map[string]any
	outgoingRows []map[string]any
}

type resourceInvestigationRunCall struct {
	cypher string
	params map[string]any
}

func (g *recordingResourceInvestigationGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runCalls = append(g.runCalls, resourceInvestigationRunCall{cypher: cypher, params: params})
	switch {
	case strings.Contains(cypher, "MATCH (instance:WorkloadInstance)"):
		return g.workloadRows, nil
	case strings.Contains(cypher, "<-[rels"):
		return g.incomingRows, nil
	case strings.Contains(cypher, "-[rels"):
		return g.outgoingRows, nil
	}
	if len(g.runRows) == 0 {
		return nil, nil
	}
	rows := g.runRows[0]
	g.runRows = g.runRows[1:]
	return rows, nil
}

func (g *recordingResourceInvestigationGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func TestInvestigateResourceReturnsAmbiguityWithoutTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{runRows: [][]map[string]any{{
		{"id": "cloud:queue:orders", "name": "orders", "labels": []any{"CloudResource"}, "environment": "prod"},
		{"id": "k8s:queue:orders", "name": "orders", "labels": []any{"K8sResource"}, "environment": "prod"},
	}}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/resource-investigation",
		bytes.NewBufferString(`{"query":"orders","resource_type":"queue","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 1; got != want {
		t.Fatalf("graph Run calls = %d, want only resolver call", got)
	}
	data := decodeImpactEnvelopeData(t, w)
	resolution := data["target_resolution"].(map[string]any)
	if got, want := resolution["status"], "ambiguous"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	candidates := resolution["candidates"].([]any)
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestInvestigateResourceReturnsBoundedResourcePacket(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{
		runRows: [][]map[string]any{{
			{
				"id": "cloud:rds:orders", "name": "orders-db", "labels": []any{"CloudResource"},
				"resource_type": "aws_db_instance", "provider": "aws", "environment": "prod",
				"repo_id": "repo-infra", "config_path": "terraform/orders/main.tf",
			},
		}},
		workloadRows: []map[string]any{
			{
				"workload_id": "workload:orders-api", "workload_name": "orders-api",
				"instance_id": "instance:orders-api:prod", "environment": "prod",
				"relationship_type": "USES", "relationship_reason": "env DATABASE_URL",
			},
			{
				"workload_id": "workload:orders-worker", "workload_name": "orders-worker",
				"instance_id": "instance:orders-worker:prod", "environment": "prod",
				"relationship_type": "USES", "relationship_reason": "queue consumer",
			},
		},
		incomingRows: []map[string]any{
			{
				"repo_id": "repo-infra", "repo_name": "infra", "direction": "incoming", "depth": int64(2),
				"hops": []any{map[string]any{"type": "PROVISIONS", "reason": "terraform"}},
			},
		},
		outgoingRows: []map[string]any{
			{
				"repo_id": "repo-app", "repo_name": "orders-api", "direction": "outgoing", "depth": int64(3),
				"hops": []any{map[string]any{"type": "DEFINED_IN", "reason": "manifest"}},
			},
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/resource-investigation",
		bytes.NewBufferString(`{"query":"orders-db","environment":"prod","max_depth":3,"limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 4; got != want {
		t.Fatalf("graph Run calls = %d, want resolver, workloads, incoming paths, outgoing paths", got)
	}
	var depthQueries int
	for i, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "LIMIT $limit") {
			t.Fatalf("call %d cypher missing LIMIT $limit: %s", i+1, call.cypher)
		}
		if got, want := call.params["limit"], 2; got != want {
			t.Fatalf("call %d params[limit] = %#v, want %#v", i+1, got, want)
		}
		if strings.Contains(call.cypher, "*1..3") {
			depthQueries++
		}
	}
	if got, want := depthQueries, 2; got != want {
		t.Fatalf("path query count with requested depth = %d, want %d", got, want)
	}

	data := decodeImpactEnvelopeData(t, w)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	workloads := data["workloads"].([]any)
	if got, want := len(workloads), 1; got != want {
		t.Fatalf("workload count after limit = %d, want %d", got, want)
	}
	repos := data["provisioning_paths"].([]any)
	if got, want := len(repos), 2; got != want {
		t.Fatalf("provisioning path count = %d, want %d", got, want)
	}
	nextCalls := data["recommended_next_calls"].([]any)
	if got, want := len(nextCalls), 2; got < want {
		t.Fatalf("recommended_next_calls = %d, want at least %d", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "resolved_resource_investigation"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}
