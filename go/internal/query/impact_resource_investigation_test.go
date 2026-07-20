// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordingResourceInvestigationGraph struct {
	mu                   sync.Mutex
	runCalls             []resourceInvestigationRunCall
	runRows              [][]map[string]any
	workloadRows         []map[string]any
	instanceWorkloadRows []map[string]any
	incomingRows         []map[string]any
	outgoingRows         []map[string]any
	workloadErr          error
	instanceWorkloadErr  error
	incomingErr          error
	outgoingErr          error
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
	case strings.Contains(cypher, "-[:INSTANCE_OF]->(workload:Workload)"):
		if g.instanceWorkloadErr != nil {
			return nil, g.instanceWorkloadErr
		}
		return g.instanceWorkloadRows, nil
	case strings.Contains(cypher, "MATCH (instance:WorkloadInstance)"):
		if g.workloadErr != nil {
			return nil, g.workloadErr
		}
		return g.workloadRows, nil
	case strings.Contains(cypher, "<-[rels"):
		if g.incomingErr != nil {
			return nil, g.incomingErr
		}
		return g.incomingRows, nil
	case strings.Contains(cypher, "-[rels"):
		if g.outgoingErr != nil {
			return nil, g.outgoingErr
		}
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
				"instance_id": "instance:orders-api:prod", "environment": "prod",
				"workload_id_raw": "workload:orders-api", "instance_name": "orders-api",
				"relationship_type": "USES", "relationship_reason": "env DATABASE_URL",
				"confidence": 0.95,
			},
			{
				"instance_id": "instance:orders-worker:prod", "environment": "prod",
				"workload_id_raw": "workload:orders-worker", "instance_name": "orders-worker",
				"relationship_type": "USES", "relationship_reason": "queue consumer",
			},
		},
		instanceWorkloadRows: []map[string]any{
			{
				"instance_id": "instance:orders-api:prod",
				"workload_id": "workload:orders-api", "workload_name": "orders-api",
			},
			{
				"instance_id": "instance:orders-worker:prod",
				"workload_id": "workload:orders-worker", "workload_name": "orders-worker",
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
	if got, want := len(graph.runCalls), 5; got != want {
		t.Fatalf("graph Run calls = %d, want resolver, workload instances, instance-of resolve, incoming paths, outgoing paths", got)
	}
	var depthQueries int
	for i, call := range graph.runCalls[1:] {
		// The INSTANCE_OF workload-resolve read is bounded by the (already
		// limited) instance-id set, not a LIMIT clause.
		if strings.Contains(call.cypher, "-[:INSTANCE_OF]->(workload:Workload)") {
			continue
		}
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
	firstWorkload := workloads[0].(map[string]any)
	if got, want := firstWorkload["confidence"], 0.95; got != want {
		t.Fatalf("workload confidence = %#v, want numeric %#v", got, want)
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

func TestInvestigateResourceResolvesExactCloudARN(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ssm:us-east-1:123456789012:parameter/configd/sample-service/client/port"
	graph := &recordingResourceInvestigationGraph{
		runRows: [][]map[string]any{{
			{
				"id": "cloud:ssm:sample-service-client-port", "name": "/configd/sample-service/client/port",
				"labels": []any{"CloudResource"}, "resource_type": "ssm_parameter",
				"provider": "aws", "environment": "dev", "resource_id": "/configd/sample-service/client/port",
				"arn": arn,
			},
		}},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/resource-investigation",
		bytes.NewBufferString(`{"resource_id":"`+arn+`","resource_type":"cloud","limit":5}`),
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
	resolverCall := graph.runCalls[0]
	if !strings.Contains(resolverCall.cypher, "n.arn = $selector") {
		t.Fatalf("resolver cypher does not match exact ARN selectors: %s", resolverCall.cypher)
	}
	if got, want := resolverCall.params["selector"], arn; got != want {
		t.Fatalf("resolver selector = %#v, want %#v", got, want)
	}
	for i, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "resource.arn = $resource_arn") {
			t.Fatalf("section call %d cypher does not carry selected ARN: %s", i+1, call.cypher)
		}
		if got, want := call.params["resource_arn"], arn; got != want {
			t.Fatalf("section call %d resource_arn = %#v, want %#v", i+1, got, want)
		}
	}
	data := decodeImpactEnvelopeData(t, w)
	resolution := data["target_resolution"].(map[string]any)
	if got, want := resolution["status"], "resolved"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	resource := data["resource"].(map[string]any)
	if got, want := resource["arn"], arn; got != want {
		t.Fatalf("resource.arn = %#v, want %#v", got, want)
	}
	missingEvidence := StringSliceVal(data, "missing_evidence")
	for _, want := range []string{"resource_usage_relationship_missing", "repository_provenance_path_missing"} {
		if !stringSliceContains(missingEvidence, want) {
			t.Fatalf("missing_evidence = %#v, want %q", missingEvidence, want)
		}
	}
}

func TestResourceInvestigationResolverNarrowsQueueAndDatabaseTypes(t *testing.T) {
	t.Parallel()

	queueCypher := resourceInvestigationResolverCypher(resourceInvestigationRequest{
		Query:        "orders",
		ResourceType: "queue",
		Limit:        25,
	}, repositoryAccessFilter{allScopes: true})
	for _, want := range []string{"CONTAINS 'queue'", "CONTAINS 'sqs'"} {
		if !strings.Contains(queueCypher, want) {
			t.Fatalf("queue resolver cypher missing %q: %s", want, queueCypher)
		}
	}

	databaseCypher := resourceInvestigationResolverCypher(resourceInvestigationRequest{
		Query:        "orders",
		ResourceType: "database",
		Limit:        25,
	}, repositoryAccessFilter{allScopes: true})
	for _, want := range []string{"CONTAINS 'database'", "CONTAINS 'rds'", "CONTAINS 'postgres'"} {
		if !strings.Contains(databaseCypher, want) {
			t.Fatalf("database resolver cypher missing %q: %s", want, databaseCypher)
		}
	}
}

func TestLoadResourceInvestigationSectionsJoinsParallelErrors(t *testing.T) {
	t.Parallel()

	workloadErr := errors.New("workload query failed")
	incomingErr := errors.New("incoming query failed")
	handler := &ImpactHandler{Neo4j: &recordingResourceInvestigationGraph{
		workloadErr: workloadErr,
		incomingErr: incomingErr,
	}}

	_, _, _, _, _, _, err := handler.loadResourceInvestigationSections(
		context.Background(),
		resourceInvestigationRequest{Limit: 1, MaxDepth: 1},
		&resourceInvestigationCandidate{ID: "cloud:rds:orders", Labels: []string{"CloudResource"}},
		repositoryAccessFilter{allScopes: true},
	)
	if !errors.Is(err, workloadErr) {
		t.Fatalf("joined error missing workload error: %v", err)
	}
	if !errors.Is(err, incomingErr) {
		t.Fatalf("joined error missing incoming error: %v", err)
	}
}

func TestLoadResourceInvestigationSectionsRejectsUnknownAnchorLabel(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{}
	handler := &ImpactHandler{Neo4j: graph}
	_, _, _, _, _, _, err := handler.loadResourceInvestigationSections(
		context.Background(),
		resourceInvestigationRequest{Limit: 1, MaxDepth: 1},
		&resourceInvestigationCandidate{ID: "proof", Labels: []string{"Repository"}},
		repositoryAccessFilter{allScopes: true},
	)
	if err == nil || !strings.Contains(err.Error(), "supported infrastructure label") {
		t.Fatalf("loadResourceInvestigationSections() error = %v, want fail-closed label error", err)
	}
	if len(graph.runCalls) != 0 {
		t.Fatalf("graph calls = %d, want 0", len(graph.runCalls))
	}
}

// TestResourceInvestigationWorkloadsGrantFiltersBeforeTruncation is the #5167
// W3 P1 mutation-check for resourceInvestigationWorkloads: a cross-tenant
// workload sorted ahead of two granted ones (in the raw fetched instance-id
// order) must not consume a limit slot and drop a granted workload past the
// boundary. With the prior trim-before-filter order, limit=2 returned only one
// granted workload; filtering the full fetched set before trimming returns
// both. The grated rows are deliberately NOT all within the first `limit`
// positions of the raw set.
func TestResourceInvestigationWorkloadsGrantFiltersBeforeTruncation(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{
		workloadRows: []map[string]any{
			{"instance_id": "inst-b", "workload_id_raw": "wl-b", "instance_name": "svc-b", "relationship_type": "USES"},
			{"instance_id": "inst-a1", "workload_id_raw": "wl-a1", "instance_name": "svc-a1", "relationship_type": "USES"},
			{"instance_id": "inst-a2", "workload_id_raw": "wl-a2", "instance_name": "svc-a2", "relationship_type": "USES"},
		},
		instanceWorkloadRows: []map[string]any{
			{"instance_id": "inst-b", "workload_id": "wl-b", "workload_name": "svc-b", "workload_repo_id": "repo-b"},
			{"instance_id": "inst-a1", "workload_id": "wl-a1", "workload_name": "svc-a1", "workload_repo_id": "repo-a"},
			{"instance_id": "inst-a2", "workload_id": "wl-a2", "workload_name": "svc-a2", "workload_repo_id": "repo-a"},
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	access := repositoryAccessFilterFromContext(ContextWithAuthContext(context.Background(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))

	workloads, _, err := handler.resourceInvestigationWorkloads(
		context.Background(),
		resourceInvestigationRequest{Limit: 2, MaxDepth: 2},
		&resourceInvestigationCandidate{ID: "res", Labels: []string{"CloudResource"}},
		access,
	)
	if err != nil {
		t.Fatalf("resourceInvestigationWorkloads() error = %v", err)
	}
	if got, want := len(workloads), 2; got != want {
		t.Fatalf("granted workload count = %d, want %d (both repo-a workloads must survive; trim-before-filter drops one): %#v", got, want, workloads)
	}
	for _, workload := range workloads {
		if id := StringVal(workload, "workload_id"); id == "wl-b" {
			t.Fatalf("cross-tenant workload wl-b (repo-b) present for repo-a caller: %#v", workloads)
		}
	}
}

// TestResourceInvestigationRepoPathsGrantFiltersBeforeTruncation is the same
// #5167 W3 P1 proof for resourceInvestigationRepoPaths: a cross-tenant path
// sorted ahead of two granted paths must not steal a limit slot.
func TestResourceInvestigationRepoPathsGrantFiltersBeforeTruncation(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{
		outgoingRows: []map[string]any{
			{"repo_id": "repo-b", "repo_name": "svc-b", "depth": int64(1)},
			{"repo_id": "repo-a", "repo_name": "svc-a1", "depth": int64(1)},
			{"repo_id": "repo-a", "repo_name": "svc-a2", "depth": int64(2)},
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	access := repositoryAccessFilterFromContext(ContextWithAuthContext(context.Background(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))

	paths, _, err := handler.resourceInvestigationRepoPaths(
		context.Background(),
		resourceInvestigationRequest{Limit: 2, MaxDepth: 2},
		&resourceInvestigationCandidate{ID: "res", Labels: []string{"CloudResource"}},
		"outgoing",
		access,
	)
	if err != nil {
		t.Fatalf("resourceInvestigationRepoPaths() error = %v", err)
	}
	if got, want := len(paths), 2; got != want {
		t.Fatalf("granted path count = %d, want %d (both repo-a paths must survive; trim-before-filter drops one): %#v", got, want, paths)
	}
	for _, path := range paths {
		if repoID := StringVal(path, "repo_id"); repoID == "repo-b" {
			t.Fatalf("cross-tenant repo-b path present for repo-a caller: %#v", paths)
		}
	}
}
