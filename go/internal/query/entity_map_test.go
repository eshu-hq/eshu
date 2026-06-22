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
		{},
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
	if got, want := len(graph.runCalls), 5; got != want {
		t.Fatalf("graph Run calls = %d, want resolver plus four bounded traversal specs", got)
	}
	if resolver := graph.runCalls[0].cypher; strings.Contains(resolver, "MATCH (n) WHERE") {
		t.Fatalf("resolver used unlabelled scan: %s", resolver)
	}
	var directReads, deeperReads int
	for _, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "(start:Workload {id: $from_id})") {
			t.Fatalf("traversal cypher = %s, want typed Workload id anchor", call.cypher)
		}
		if got := strings.Count(call.cypher, "MATCH "); got != 1 {
			t.Fatalf("traversal cypher has %d MATCH clauses, want one connected anchor+expand: %s", got, call.cypher)
		}
		if strings.Contains(call.cypher, "*2..2") {
			deeperReads++
		} else {
			directReads++
		}
		if !strings.Contains(call.cypher, "LIMIT $limit") {
			t.Fatalf("traversal cypher = %s, want limit parameter", call.cypher)
		}
		if strings.Contains(call.cypher, "start.repo_id") || strings.Contains(call.cypher, "start.environment") {
			t.Fatalf("traversal cypher = %s, want filters scoped to returned entity, not the start node", call.cypher)
		}
		for _, want := range []string{
			"coalesce(entity.environment, '') = '' OR entity.environment = $environment",
			"coalesce(entity.repo_id, entity.id, '') = $repo_id",
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
	if got, want := directReads, 2; got != want {
		t.Fatalf("direct traversal reads = %d, want %d", got, want)
	}
	if got, want := deeperReads, 2; got != want {
		t.Fatalf("deeper traversal reads = %d, want %d", got, want)
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

func TestEntityMapDepthTwoUsesBoundedTraversalSpecs(t *testing.T) {
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
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"checkout","from_type":"service","depth":2,"limit":25}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, max := len(graph.runCalls), 5; got > max {
		t.Fatalf("graph Run calls = %d, want resolver plus at most four bounded traversal specs", got)
	}
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "*1..2") {
			t.Fatalf("traversal cypher = %s, want direct depth-1 read plus separate bounded deeper read", call.cypher)
		}
	}
}

// TestEntityMapTraversalAnchorsExpansionInSingleConnectedMatch is the #3549
// regression guard: the neighborhood traversal must anchor the indexed start
// node and expand from it in one connected MATCH pattern. Splitting the anchor
// and the expansion across two MATCH clauses (a bare MATCH (start:Label {...})
// then a separate MATCH (start)-[rel]->(entity)) makes NornicDB re-plan the
// second clause independently of the resolved start, scanning the relationship
// family population instead of the indexed anchor — every service-node entity
// map then timed out past the console's 15s budget (HTTP 000) returning zero
// rows. Each traversal Cypher must therefore contain exactly one MATCH clause.
func TestEntityMapTraversalAnchorsExpansionInSingleConnectedMatch(t *testing.T) {
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
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"checkout","from_type":"service","depth":2,"limit":25}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if len(graph.runCalls) < 2 {
		t.Fatalf("graph Run calls = %d, want resolver plus traversal specs", len(graph.runCalls))
	}
	for _, call := range graph.runCalls[1:] {
		if got := strings.Count(call.cypher, "MATCH "); got != 1 {
			t.Fatalf("traversal cypher has %d MATCH clauses, want exactly one connected anchor+expand: %s", got, call.cypher)
		}
		if !strings.Contains(call.cypher, "(start:Workload {id: $from_id})-[") &&
			!strings.Contains(call.cypher, "(start:Workload {id: $from_id})<-[") {
			t.Fatalf("traversal cypher = %s, want connected indexed Workload anchor", call.cypher)
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
	if got, want := len(graph.runCalls), 3; got != want {
		t.Fatalf("graph Run calls = %d, want terraform resolver plus two direct traversal specs", got)
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

// TestEntityMapPopulatesTypedVerbAndEntityIDForVarLengthEdge covers the #1604
// row-shape contract after #1723's bounded traversal rewrite: direct
// relationship reads preserve a typed verb, Go normalization keeps a stable
// entity id, and depth is clamped to at least one when a backend reports zero.
func TestEntityMapPopulatesTypedVerbAndEntityIDForVarLengthEdge(t *testing.T) {
	t.Parallel()

	resolverRows := []map[string]any{
		{
			"id":              "workload:orders-api",
			"name":            "orders-api",
			"labels":          []any{"Workload"},
			"repo_id":         "repository:r_orders_api",
			"anchor_label":    "Workload",
			"anchor_property": "id",
			"anchor_value":    "workload:orders-api",
		},
	}
	definesRow := map[string]any{
		"entity_id":          "repository:r_orders_api",
		"entity_name":        "orders-api",
		"entity_labels":      []any{"Repository"},
		"direction":          "incoming",
		"depth":              int64(0),
		"relationship_type":  "DEFINES",
		"relationship_types": []any{},
		"repo_id":            "repository:r_orders_api",
	}
	runRows := [][]map[string]any{
		resolverRows,
		nil,
		nil,
		{definesRow},
		nil,
	}

	graph := &recordingEntityMapGraph{runRows: runRows}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"orders-api","from_type":"service","depth":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var definesCall entityMapRunCall
	foundDefinesCall := false
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "RETURN DISTINCT") {
			t.Fatalf("traversal cypher uses RETURN DISTINCT: %s", call.cypher)
		}
		if strings.Contains(call.cypher, "[rel:DEFINES|") {
			definesCall = call
			foundDefinesCall = true
		}
	}
	if !foundDefinesCall {
		t.Fatal("incoming DEFINES traversal call not recorded")
	}
	if strings.Contains(definesCall.cypher, "coalesce(entity.id, entity.uid, entity.resource_id, entity.path, entity.name) AS entity_id") {
		t.Fatalf("DEFINES traversal cypher still uses fragile multi-property entity_id coalesce: %s", definesCall.cypher)
	}

	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	if len(relationships) != 1 {
		t.Fatalf("relationship count = %d, want 1; evidence=%#v", len(relationships), evidence)
	}
	row := relationships[0].(map[string]any)
	if got, want := row["entity_id"], "repository:r_orders_api"; got != want {
		t.Fatalf("entity_id = %#v, want %#v; row=%#v", got, want, row)
	}
	if got, want := row["relationship_type"], "DEFINES"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v; row=%#v", got, want, row)
	}
	types, ok := row["relationship_types"].([]any)
	if !ok || len(types) != 1 || types[0] != "DEFINES" {
		t.Fatalf("relationship_types = %#v, want [DEFINES]; row=%#v", row["relationship_types"], row)
	}
	if got, want := row["depth"], float64(1); got != want {
		t.Fatalf("depth = %#v, want %#v; row=%#v", got, want, row)
	}

	sections := data["sections"].(map[string]any)
	if got := sections["defined_by"].([]any); len(got) != 1 {
		t.Fatalf("defined_by count = %d, want 1; sections=%#v", len(got), sections)
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
