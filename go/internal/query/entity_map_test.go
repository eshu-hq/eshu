package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingEntityMapGraph struct {
	runCalls []entityMapRunCall
	runRows  [][]map[string]any
}

type entityMapRunCall struct {
	cypher string
	params map[string]any
}

func (g *recordingEntityMapGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.runCalls = append(g.runCalls, entityMapRunCall{cypher: cypher, params: params})
	if len(g.runRows) == 0 {
		return nil, nil
	}
	rows := g.runRows[0]
	g.runRows = g.runRows[1:]
	return rows, nil
}

func (g *recordingEntityMapGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func TestEntityMapReturnsAmbiguityWithoutTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{{
		{
			"id":              "workload:orders-api",
			"name":            "orders",
			"labels":          []any{"Workload"},
			"repo_id":         "repo-api",
			"anchor_label":    "Workload",
			"anchor_property": "id",
			"anchor_value":    "workload:orders-api",
		},
		{
			"id":              "workload:orders-worker",
			"name":            "orders",
			"labels":          []any{"Workload"},
			"repo_id":         "repo-worker",
			"anchor_label":    "Workload",
			"anchor_property": "id",
			"anchor_value":    "workload:orders-worker",
		},
	}}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"orders","from_type":"service","limit":1}`),
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

	data := decodeEntityMapData(t, w)
	if got, want := data["status"], "ambiguous"; got != want {
		t.Fatalf("status = %#v, want %#v", got, want)
	}
	resolution := data["resolution"].(map[string]any)
	if got, want := resolution["status"], "ambiguous"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	if got, want := len(resolution["candidates"].([]any)), 1; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	evidence := data["evidence"].(map[string]any)
	if got, want := evidence["truncated"], true; got != want {
		t.Fatalf("evidence.truncated = %#v, want %#v", got, want)
	}
}

func TestEntityMapUsesTypedAnchorAndGroupsBoundedNeighborhood(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "workload:checkout",
				"name":            "checkout",
				"labels":          []any{"Workload"},
				"repo_id":         "repo-checkout",
				"anchor_label":    "Workload",
				"anchor_property": "id",
				"anchor_value":    "workload:checkout",
			},
		},
		{
			{
				"entity_id":          "resource:db",
				"entity_name":        "checkout-db",
				"entity_labels":      []any{"CloudResource"},
				"direction":          "outgoing",
				"depth":              int64(1),
				"relationship_types": []any{"USES"},
				"repo_id":            "repo-checkout",
				"environment":        "prod",
			},
			{
				"entity_id":          "workload:payments",
				"entity_name":        "payments",
				"entity_labels":      []any{"Workload"},
				"direction":          "outgoing",
				"depth":              int64(1),
				"relationship_types": []any{"DEPENDS_ON"},
				"repo_id":            "repo-payments",
			},
		},
		{
			{
				"entity_id":          "repo-checkout",
				"entity_name":        "checkout-service",
				"entity_labels":      []any{"Repository"},
				"direction":          "incoming",
				"depth":              int64(1),
				"relationship_types": []any{"DEFINES"},
				"repo_id":            "repo-checkout",
			},
		},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"checkout","from_type":"service","repo_id":"repo-checkout","environment":"prod","depth":2,"limit":3}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 3; got != want {
		t.Fatalf("graph Run calls = %d, want resolver plus outgoing and incoming traversals", got)
	}
	if resolver := graph.runCalls[0].cypher; strings.Contains(resolver, "MATCH (n) WHERE") {
		t.Fatalf("resolver used unlabelled scan: %s", resolver)
	}
	for _, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "MATCH (start:Workload {id: $from_id})") {
			t.Fatalf("traversal cypher = %s, want typed Workload id anchor", call.cypher)
		}
		if !strings.Contains(call.cypher, "*1..2") {
			t.Fatalf("traversal cypher = %s, want bounded depth", call.cypher)
		}
		if !strings.Contains(call.cypher, "LIMIT $limit") {
			t.Fatalf("traversal cypher = %s, want limit parameter", call.cypher)
		}
		if strings.Contains(call.cypher, "start.repo_id") || strings.Contains(call.cypher, "start.environment") {
			t.Fatalf("traversal cypher = %s, want filters scoped to returned entity, not the start node", call.cypher)
		}
		for _, want := range []string{
			"coalesce(entity.environment, '') = '' OR entity.environment = $environment",
			"coalesce(entity.repo_id, '') = '' OR entity.repo_id = $repo_id",
		} {
			if !strings.Contains(call.cypher, want) {
				t.Fatalf("traversal cypher missing %q: %s", want, call.cypher)
			}
		}
		if got, want := call.params["repo_id"], "repo-checkout"; got != want {
			t.Fatalf("traversal repo_id param = %#v, want %#v", got, want)
		}
		if got, want := call.params["environment"], "prod"; got != want {
			t.Fatalf("traversal environment param = %#v, want %#v", got, want)
		}
	}

	data := decodeEntityMapData(t, w)
	if got, want := data["status"], "mapped"; got != want {
		t.Fatalf("status = %#v, want %#v", got, want)
	}
	sections := data["sections"].(map[string]any)
	for name, wantCount := range map[string]int{
		"defined_by": 1,
		"depends_on": 2,
		"runs_as":    1,
	} {
		got := sections[name].([]any)
		if len(got) != wantCount {
			t.Fatalf("section %s count = %d, want %d; sections=%#v", name, len(got), wantCount, sections)
		}
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "typed_entity_map_bounded_relationship_family"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestEntityMapRepositoryAnchorUsesDirectRelationshipFamilyTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 1+len(entityMapRepositoryOutgoingRelationships)+len(entityMapRepositoryIncomingRelationships); got != want {
		t.Fatalf("graph Run calls = %d, want resolver plus fixed relationship-family traversals", got)
	}
	for _, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "MATCH (start:Repository {id: $from_id})") {
			t.Fatalf("traversal cypher = %s, want typed Repository id anchor", call.cypher)
		}
		if strings.Contains(call.cypher, "*1..1") {
			t.Fatalf("traversal cypher = %s, want direct one-hop traversal without variable path expansion", call.cypher)
		}
		if !strings.Contains(call.cypher, "1 AS depth") {
			t.Fatalf("traversal cypher = %s, want direct traversal depth projection", call.cypher)
		}
		if !strings.Contains(call.cypher, " AS relationship_type") {
			t.Fatalf("traversal cypher = %s, want direct relationship type projection", call.cypher)
		}
		if strings.Contains(call.cypher, "-[rel]->") || strings.Contains(call.cypher, "<-[rel]-") {
			t.Fatalf("traversal cypher = %s, want explicit relationship family instead of untyped fanout", call.cypher)
		}
		if strings.Contains(call.cypher, "CONTAINS") || strings.Contains(call.cypher, "REPO_CONTAINS") {
			t.Fatalf("traversal cypher = %s, want default map to avoid structural repository fanout", call.cypher)
		}
		if strings.Contains(call.cypher, "CALLS") || strings.Contains(call.cypher, "IMPORTS") {
			t.Fatalf("traversal cypher = %s, want repository map to avoid code-edge fanout", call.cypher)
		}
	}
	var sawIncomingDeploys bool
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "(start)<-[rel:DEPLOYS_FROM]-(entity)") {
			sawIncomingDeploys = true
		}
	}
	if !sawIncomingDeploys {
		t.Fatalf("traversal calls = %#v, want incoming DEPLOYS_FROM family", graph.runCalls)
	}

	data := decodeEntityMapData(t, w)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "typed_entity_map_relationship_family"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestEntityMapDirectTraversalNormalizesScalarRelationshipType(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{
			{
				"entity_id":         "directory:.github",
				"entity_name":       ".github",
				"entity_labels":     []any{"Directory"},
				"direction":         "outgoing",
				"depth":             int64(1),
				"relationship_type": "CONTAINS",
				"repo_id":           "repo-checkout",
			},
		},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","relationship":"CONTAINS","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("relationship count = %d, want %d", got, want)
	}
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["relationship_type"], "CONTAINS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	types := relationship["relationship_types"].([]any)
	if got, want := types[0], "CONTAINS"; got != want {
		t.Fatalf("relationship_types[0] = %#v, want %#v", got, want)
	}
}

func TestEntityMapExplicitRelationshipBackfillsMissingBackendType(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{
			{
				"entity_id":     "directory:.github",
				"entity_name":   ".github",
				"entity_labels": []any{"Directory"},
				"direction":     "outgoing",
				"depth":         int64(1),
				"repo_id":       "repo-checkout",
			},
		},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","relationship":"CONTAINS","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["relationship_type"], "CONTAINS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	types := relationship["relationship_types"].([]any)
	if got, want := types[0], "CONTAINS"; got != want {
		t.Fatalf("relationship_types[0] = %#v, want %#v", got, want)
	}
}

func TestEntityMapExplicitRelationshipUsesRequestedDirectTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","relationship":"CONTAINS","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "*1..1") {
			t.Fatalf("traversal cypher = %s, want direct one-hop traversal for explicit relationship", call.cypher)
		}
		if !strings.Contains(call.cypher, "[rel:CONTAINS]") {
			t.Fatalf("traversal cypher = %s, want requested CONTAINS relationship pattern", call.cypher)
		}
	}
}

func TestEntityMapResolvesTerraformAddressWithoutWholeGraphScan(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "tfstate:aws_lb.main",
				"name":            "aws_lb.main",
				"labels":          []any{"TerraformResource"},
				"repo_id":         "repo-infra",
				"anchor_label":    "TerraformResource",
				"anchor_property": "uid",
				"anchor_value":    "tfstate:aws_lb.main",
			},
		},
		{},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"terraform/aws_lb.main","limit":5,"depth":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 1+len(entityMapDefaultOutgoingRelationships)+len(entityMapDefaultIncomingRelationships); got != want {
		t.Fatalf("graph Run calls = %d, want terraform resolver plus fixed relationship-family traversals", got)
	}
	resolver := graph.runCalls[0]
	if strings.Contains(resolver.cypher, "MATCH (n) WHERE") {
		t.Fatalf("resolver used unlabelled scan: %s", resolver.cypher)
	}
	if !strings.Contains(resolver.cypher, "MATCH (n:TerraformResource {address: $from})") {
		t.Fatalf("resolver cypher = %s, want TerraformResource address probe", resolver.cypher)
	}
	if got, want := resolver.params["from"], "aws_lb.main"; got != want {
		t.Fatalf("resolver from = %#v, want normalized Terraform address %#v", got, want)
	}
	if traversal := graph.runCalls[1].cypher; !strings.Contains(traversal, "MATCH (start:TerraformResource {uid: $from_id})") {
		t.Fatalf("traversal cypher = %s, want TerraformResource uid anchor", traversal)
	}

	data := decodeEntityMapData(t, w)
	if got, want := data["from"], "terraform/aws_lb.main"; got != want {
		t.Fatalf("data.from = %#v, want original selector %#v", got, want)
	}
	resolution := data["resolution"].(map[string]any)
	if got, want := resolution["input"], "terraform/aws_lb.main"; got != want {
		t.Fatalf("resolution.input = %#v, want original selector %#v", got, want)
	}
	selected := resolution["selected"].(map[string]any)
	if got, want := selected["id"], "tfstate:aws_lb.main"; got != want {
		t.Fatalf("selected.id = %#v, want %#v", got, want)
	}
}

func decodeEntityMapData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; body=%s", err, w.Body.String())
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil, want capability metadata")
	}
	if got, want := envelope.Truth.Capability, "platform_impact.entity_map"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
