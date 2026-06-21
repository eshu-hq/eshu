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

// legacyChangeSurfaceGraph records every Run call so a test can assert the
// resolver-then-traversal call shape and reply with a scripted row set per call.
type legacyChangeSurfaceGraph struct {
	calls   []changeSurfaceRunCall
	handler func(cypher string, params map[string]any) ([]map[string]any, error)
}

func (g *legacyChangeSurfaceGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.calls = append(g.calls, changeSurfaceRunCall{cypher: cypher, params: params})
	if g.handler != nil {
		return g.handler(cypher, params)
	}
	return nil, nil
}

func (g *legacyChangeSurfaceGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func decodeLegacyChangeSurfaceData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	return data
}

// TestFindChangeSurfaceServiceKindAnchorsLabeledStartAndBoundsDepth proves the
// service-kind hang fix (#3384): the start node MUST be resolved via a
// label-anchored indexed lookup and the traversal MUST anchor a labeled start
// node with a bounded, parameterized depth. The prior unlabeled
// `MATCH (start) WHERE start.id = $target_id` + hardcoded `*1..8` forced a
// full-node scan plus an 8-hop neighborhood explosion on a dense Workload node.
func TestFindChangeSurfaceServiceKindAnchorsLabeledStartAndBoundsDepth(t *testing.T) {
	t.Parallel()

	var traversalCypher string
	var traversalParams map[string]any
	graph := &legacyChangeSurfaceGraph{
		handler: func(cypher string, params map[string]any) ([]map[string]any, error) {
			// Resolver probe: label-anchored Workload lookup by id.
			if strings.Contains(cypher, "MATCH (n:Workload {id:") {
				return []map[string]any{
					{"id": "workload:orders-api", "name": "orders", "labels": []any{"Workload"}, "repo_id": "repo-api"},
				}, nil
			}
			// Traversal: the bounded impact expansion.
			if strings.Contains(cypher, "relationships(path)") {
				traversalCypher = cypher
				traversalParams = params
				return []map[string]any{
					{"id": "repo:billing", "name": "billing", "labels": []any{"Repository"}, "environment": "prod", "rel_type": "DEPENDS_ON", "confidence": 0.9, "reason": "import", "depth": int64(1)},
					{"id": "repo:ledger", "name": "ledger", "labels": []any{"Repository"}, "environment": "prod", "rel_type": "CALLS", "confidence": 0.8, "reason": "rpc", "depth": int64(2)},
				}, nil
			}
			return nil, nil
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface",
		bytes.NewBufferString(`{"kind":"service","target":"orders","environment":"prod","max_depth":3,"limit":50}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.findChangeSurface(rec, req)

	data := decodeLegacyChangeSurfaceData(t, rec)

	// Root cause (a): start node MUST be resolved by an indexed, label-anchored
	// MATCH, never an unlabeled all-node scan.
	if traversalCypher == "" {
		t.Fatalf("traversal query was never issued; calls = %+v", graph.calls)
	}
	if strings.Contains(traversalCypher, "MATCH (start) WHERE start.id") {
		t.Fatalf("traversal must not anchor an unlabeled start node (all-node scan): %s", traversalCypher)
	}
	if !strings.Contains(traversalCypher, "MATCH (start:Workload {id:") {
		t.Fatalf("traversal must anchor the resolved label in the start MATCH: %s", traversalCypher)
	}
	// Root cause (b): depth MUST be bounded by the requested/clamped max_depth,
	// not the hardcoded 8.
	if !strings.Contains(traversalCypher, "rels*1..3]") {
		t.Fatalf("traversal depth must honor max_depth=3, got: %s", traversalCypher)
	}
	if strings.Contains(traversalCypher, "*1..8") {
		t.Fatalf("traversal must not keep the hardcoded *1..8 depth: %s", traversalCypher)
	}
	// Over-fetch by one row to detect truncation honestly.
	if got, want := traversalParams["limit"], 51; got != want {
		t.Fatalf("traversal limit = %#v, want limit+1 = %#v", got, want)
	}

	// Accuracy: per-relationship fields stay in the response (legacy contract).
	impacted, ok := data["impacted"].([]any)
	if !ok || len(impacted) != 2 {
		t.Fatalf("impacted = %#v, want two rows", data["impacted"])
	}
	first, _ := impacted[0].(map[string]any)
	if got, want := first["reason"], "import"; got != want {
		t.Fatalf("impacted[0].reason = %#v, want %#v (per-rel fields must be preserved)", got, want)
	}
	if _, ok := first["confidence"]; !ok {
		t.Fatalf("impacted[0] must preserve confidence field: %#v", first)
	}
	if got, want := data["count"], float64(2); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

// TestFindChangeSurfaceClampsMaxDepth proves max_depth is defaulted and clamped
// to the bounded range so an attacker/caller cannot reintroduce the deep scan.
func TestFindChangeSurfaceClampsMaxDepth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		body      string
		wantDepth string
	}{
		{"default_when_absent", `{"kind":"service","target":"orders"}`, "rels*1..4]"},
		{"clamped_when_over_max", `{"kind":"service","target":"orders","max_depth":99}`, "rels*1..8]"},
		{"floored_when_zero", `{"kind":"service","target":"orders","max_depth":0}`, "rels*1..4]"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var traversalCypher string
			graph := &legacyChangeSurfaceGraph{
				handler: func(cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "MATCH (n:Workload {id:") {
						return []map[string]any{
							{"id": "workload:orders", "name": "orders", "labels": []any{"Workload"}},
						}, nil
					}
					if strings.Contains(cypher, "relationships(path)") {
						traversalCypher = cypher
					}
					return nil, nil
				},
			}
			handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface", bytes.NewBufferString(tc.body))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			handler.findChangeSurface(rec, req)
			decodeLegacyChangeSurfaceData(t, rec)
			if !strings.Contains(traversalCypher, tc.wantDepth) {
				t.Fatalf("traversal depth = %q, want contains %q", traversalCypher, tc.wantDepth)
			}
		})
	}
}

// TestFindChangeSurfaceReportsTruncationWithOverfetch proves the handler
// surfaces truncation honestly when the backend held more rows than the bound.
func TestFindChangeSurfaceReportsTruncationWithOverfetch(t *testing.T) {
	t.Parallel()

	graph := &legacyChangeSurfaceGraph{
		handler: func(cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "MATCH (n:Workload {id:") {
				return []map[string]any{{"id": "workload:orders", "name": "orders", "labels": []any{"Workload"}}}, nil
			}
			if strings.Contains(cypher, "relationships(path)") {
				// Backend returns limit+1 rows (over-fetch) -> truncated.
				rows := make([]map[string]any, 0, 3)
				for i := 0; i < 3; i++ {
					rows = append(rows, map[string]any{"id": "repo:" + string(rune('a'+i)), "name": "r", "labels": []any{"Repository"}, "depth": int64(1)})
				}
				return rows, nil
			}
			return nil, nil
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface",
		bytes.NewBufferString(`{"kind":"service","target":"orders","limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.findChangeSurface(rec, req)

	data := decodeLegacyChangeSurfaceData(t, rec)
	impacted, ok := data["impacted"].([]any)
	if !ok || len(impacted) != 2 {
		t.Fatalf("impacted = %#v, want trimmed to two rows", data["impacted"])
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
}

// TestFindChangeSurfaceBareTargetUsesLabeledProbesNotUnlabeledScan proves a
// request with no kind (existing MCP/HTTP callers send only target) still
// resolves through label-anchored probes and never falls back to an unlabeled
// all-node scan for either resolution or traversal.
func TestFindChangeSurfaceBareTargetUsesLabeledProbesNotUnlabeledScan(t *testing.T) {
	t.Parallel()

	var traversalCypher string
	graph := &legacyChangeSurfaceGraph{
		handler: func(cypher string, _ map[string]any) ([]map[string]any, error) {
			// Repository resolves on a later labeled probe.
			if strings.Contains(cypher, "MATCH (n:Repository {id:") {
				return []map[string]any{{"id": "repo:payments", "name": "payments", "labels": []any{"Repository"}}}, nil
			}
			if strings.Contains(cypher, "relationships(path)") {
				traversalCypher = cypher
				return []map[string]any{{"id": "repo:billing", "name": "billing", "labels": []any{"Repository"}, "depth": int64(1)}}, nil
			}
			return nil, nil
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface",
		bytes.NewBufferString(`{"target":"repo:payments","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.findChangeSurface(rec, req)

	data := decodeLegacyChangeSurfaceData(t, rec)
	if strings.Contains(traversalCypher, "MATCH (start:Repository {id:") == false {
		t.Fatalf("bare-target traversal must anchor the resolved Repository label: %s", traversalCypher)
	}
	// No probe nor traversal may use an unlabeled anchor.
	for _, call := range graph.calls {
		if strings.Contains(call.cypher, "MATCH (n) WHERE n.id") || strings.Contains(call.cypher, "MATCH (start) WHERE start.id") {
			t.Fatalf("no query may anchor an unlabeled node (all-node scan): %s", call.cypher)
		}
	}
	if impacted, ok := data["impacted"].([]any); !ok || len(impacted) != 1 {
		t.Fatalf("impacted = %#v, want one row", data["impacted"])
	}
}

// TestFindChangeSurfaceBareTargetPrefersExactIdOverNameCollision proves the
// bare (no-kind) legacy path resolves by exact identity across labels BEFORE any
// name fallback. The old handler anchored `MATCH (start) WHERE start.id =
// $target_id`, so a value that is a Repository id always traversed from the
// repository. Codex P2 on #3388 flagged that the generic resolver probed Workload
// name before Repository id, so a repo id that collides with a workload name
// resolved to the wrong node. Here the value is BOTH a Repository id and a
// Workload name; resolution must select the Repository.
func TestFindChangeSurfaceBareTargetPrefersExactIdOverNameCollision(t *testing.T) {
	t.Parallel()

	const collision = "payments"
	var traversalCypher string
	graph := &legacyChangeSurfaceGraph{
		handler: func(cypher string, params map[string]any) ([]map[string]any, error) {
			target, _ := params["target"].(string)
			// A Workload exists whose NAME equals the value (no Workload id match).
			if strings.Contains(cypher, "MATCH (n:Workload {name:") && target == collision {
				return []map[string]any{{"id": "workload:checkout", "name": collision, "labels": []any{"Workload"}}}, nil
			}
			// A Repository exists whose ID equals the value.
			if strings.Contains(cypher, "MATCH (n:Repository {id:") && target == collision {
				return []map[string]any{{"id": collision, "name": "payments-repo", "labels": []any{"Repository"}}}, nil
			}
			if strings.Contains(cypher, "relationships(path)") {
				traversalCypher = cypher
				return []map[string]any{{"id": "repo:billing", "name": "billing", "labels": []any{"Repository"}, "depth": int64(1)}}, nil
			}
			return nil, nil
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface",
		bytes.NewBufferString(`{"target":"`+collision+`","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.findChangeSurface(rec, req)

	data := decodeLegacyChangeSurfaceData(t, rec)
	// Exact id (Repository) must win over the colliding Workload name.
	if !strings.Contains(traversalCypher, "MATCH (start:Repository {id:") {
		t.Fatalf("exact Repository id must win over colliding Workload name; traversal = %q", traversalCypher)
	}
	if strings.Contains(traversalCypher, "MATCH (start:Workload") {
		t.Fatalf("must not resolve to the name-colliding Workload: %q", traversalCypher)
	}
	target, _ := data["target"].(map[string]any)
	if got, want := target["id"], collision; got != want {
		t.Fatalf("resolved target id = %#v, want %#v (the Repository)", got, want)
	}
}

// TestFindChangeSurfaceUnresolvableTargetReturnsEmpty proves a target that
// matches no labeled node returns a bounded empty result rather than scanning.
func TestFindChangeSurfaceUnresolvableTargetReturnsEmpty(t *testing.T) {
	t.Parallel()

	graph := &legacyChangeSurfaceGraph{
		handler: func(cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "relationships(path)") {
				t.Fatalf("traversal must not run when target is unresolved: %s", cypher)
			}
			return nil, nil
		},
	}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface",
		bytes.NewBufferString(`{"kind":"service","target":"does-not-exist"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.findChangeSurface(rec, req)

	data := decodeLegacyChangeSurfaceData(t, rec)
	if impacted, ok := data["impacted"].([]any); !ok || len(impacted) != 0 {
		t.Fatalf("impacted = %#v, want empty for unresolved target", data["impacted"])
	}
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want 0", got)
	}
}
