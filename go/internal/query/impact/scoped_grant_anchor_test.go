// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// sharedNameGraph answers anchor resolution for a name two tenants share:
// fn-dup-1 (repo-b) and fn-dup-2 (repo-a) are both named "shared-handler",
// and "only-b" names one repo-b node. Rows come back foreign first, as an
// unordered backend may return them; a LIMIT 1 resolve sees only that first
// row. Every other statement goes to the embedded two-tenant fake.
type sharedNameGraph struct {
	*twoTenantGraph
}

var sharedNameRows = map[string][]map[string]any{
	"shared-handler": {
		{"label": "Function", "id": "fn-dup-1", "name": "shared-handler", "labels": []any{"Function"}, "uid": "fn-dup-1", "repo_id": "repo-b"},
		{"label": "Function", "id": "fn-dup-2", "name": "shared-handler", "labels": []any{"Function"}, "uid": "fn-dup-2", "repo_id": "repo-a"},
	},
	"only-b": {
		{"label": "Function", "id": "fn-only-b", "name": "only-b", "labels": []any{"Function"}, "uid": "fn-only-b", "repo_id": "repo-b"},
	},
}

func (g sharedNameGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (g sharedNameGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if !strings.Contains(cypher, "AS label, n.id AS id") {
		return g.twoTenantGraph.Run(ctx, cypher, params)
	}
	g.record("resolve", params)
	if g.resolveNothing {
		return nil, nil
	}
	start, _ := params["start_id"].(string)
	rows := sharedNameRows[start]
	if strings.Contains(cypher, "LIMIT 1") && len(rows) > 1 {
		rows = rows[:1]
	}
	return rows, nil
}

// D1 (#5167 review): a name two tenants share must resolve, for a scoped
// repo-a caller, to repo-a's node on every run, never to the foreign node
// (which would render the caller's own anchor as unknown).
func TestScopedTraceResourceToCodeSharedNameResolvesGrantedNode(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	for run := range 20 {
		g := sharedNameGraph{&twoTenantGraph{}}
		h := newTwoTenantHandler(g.twoTenantGraph)
		h.Neo4j = g
		rec := postImpact(t, h, traceRoute, `{"start":"shared-handler"}`, &a)
		data := testutil.DecodeImpactEnvelopeData(t, rec)
		start, _ := data["start"].(map[string]any)
		if start["id"] != "fn-dup-2" {
			t.Fatalf("run %d: start = %v, want repo-a's fn-dup-2", run, start)
		}
		traces := g.callsOf("trace")
		if len(traces) != 1 || traces[0].params["start_id"] != "fn-dup-2" {
			t.Fatalf("run %d: traversals = %v, want one anchored on fn-dup-2", run, traces)
		}
		assertNoTenantB(t, rec.Body.String())
		if strings.Contains(rec.Body.String(), "fn-dup-1") {
			t.Fatalf("run %d: body names the foreign fn-dup-1: %s", run, rec.Body.String())
		}
	}
}

// A name only the other tenant carries still renders exactly like an unknown
// name, with no traversal: candidate resolution is not an existence oracle.
func TestScopedTraceResourceToCodeUngrantedOnlyNameIsUnknown(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	g := sharedNameGraph{&twoTenantGraph{}}
	h := newTwoTenantHandler(g.twoTenantGraph)
	h.Neo4j = g
	got := postImpact(t, h, traceRoute, `{"start":"only-b"}`, &a)
	unknownGraph := sharedNameGraph{&twoTenantGraph{resolveNothing: true}}
	hu := newTwoTenantHandler(unknownGraph.twoTenantGraph)
	hu.Neo4j = unknownGraph
	unknown := postImpact(t, hu, traceRoute, `{"start":"only-b"}`, &a)
	if got.Code != unknown.Code || got.Body.String() != unknown.Body.String() {
		t.Fatalf("ungranted-only name\n got %d %s\nwant %d %s", got.Code, got.Body.String(), unknown.Code, unknown.Body.String())
	}
	if n := len(g.callsOf("trace")); n != 0 {
		t.Fatalf("traversals = %d, want 0", n)
	}
}
