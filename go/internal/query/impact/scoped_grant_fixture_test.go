// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// twoTenantNode is one node of the #5167 impact two-tenant fixture.
type twoTenantNode struct {
	label  string
	id     string
	uid    string
	name   string
	repoID string
}

// The #5167 impact two-tenant fixture. repo-a is granted, repo-b is not.
var twoTenantNodes = []twoTenantNode{
	{label: "Repository", id: "repo-a", name: "svc-a"},
	{label: "Repository", id: "repo-b", name: "svc-b"},
	{label: "WorkloadInstance", id: "wi-a", name: "inst-a", repoID: "repo-a"},
	{label: "WorkloadInstance", id: "wi-b", name: "inst-b", repoID: "repo-b"},
	// wi-rescued is repo-b's instance whose manifests live in repo-a
	// (DEPLOYMENT_SOURCE -> repo-a), so R1b admits it.
	{label: "WorkloadInstance", id: "wi-rescued", name: "inst-rescued", repoID: "repo-b"},
	{label: "CloudResource", id: "cr-a", uid: "cr-a", name: "bucket-a"},
	{label: "CloudResource", id: "cr-b", uid: "cr-b", name: "bucket-b"},
	{label: "CloudResource", id: "cr-shared", uid: "cr-shared", name: "bucket-shared"},
	{label: "CloudResource", id: "cr-orphan", uid: "cr-orphan", name: "bucket-orphan"},
	{label: "TerraformResource", id: "tf-b", uid: "tf-b", name: "tf-b", repoID: "repo-b"},
	{label: "TerraformStateResource", id: "tsr-b", uid: "tsr-b", name: "state-b"},
	{label: "Platform", id: "platform-1", name: "eks-prod"},
	{label: "CidrBlock", id: "cidr-1", uid: "cidr-1", name: "0.0.0.0/0"},
	{label: "Function", id: "fn-a", uid: "fn-a", name: "HandleA", repoID: "repo-a"},
	{label: "Function", id: "fn-b", uid: "fn-b", name: "helperB", repoID: "repo-b"},
	{label: "Function", id: "fn-a2", uid: "fn-a2", name: "helperA2", repoID: "repo-a"},
	{label: "ShellCommand", id: "sh-a", uid: "sh-a", name: "ls", repoID: "repo-a"},
	// sh-shared's uid is shared across tenants; repo-b wrote it last.
	{label: "ShellCommand", id: "sh-shared", uid: "sh-shared", name: "curl", repoID: "repo-b"},
}

// twoTenantUses is WorkloadInstance -[:USES]-> CloudResource.
var twoTenantUses = [][2]string{{"wi-a", "cr-a"}, {"wi-b", "cr-b"}, {"wi-a", "cr-shared"}, {"wi-b", "cr-shared"}}

// twoTenantDeploymentSource is WorkloadInstance -[:DEPLOYMENT_SOURCE]-> Repository.
var twoTenantDeploymentSource = [][2]string{{"wi-rescued", "repo-a"}}

// twoTenantMatchesState is TerraformResource -[:MATCHES_STATE]-> TerraformStateResource.
var twoTenantMatchesState = [][2]string{{"tf-b", "tsr-b"}}

// tenantBIdentifiers are every repo-b identifier that must never appear in a
// scoped repo-a response body.
var tenantBIdentifiers = []string{"cr-b", "bucket-b", "repo-b", "svc-b", "scope-b", "tsr-b", "wi-b", "fn-b", "helperB", "sh-shared"}

func twoTenantNodeByKey(key string) (twoTenantNode, bool) {
	for _, node := range twoTenantNodes {
		if node.id == key || node.name == key || (node.uid != "" && node.uid == key) {
			return node, true
		}
	}
	return twoTenantNode{}, false
}

// graphNode renders a fixture node the way NornicDB renders a nodes(path)
// element to the root decoder: a map with properties and labels.
func (n twoTenantNode) graphNode() map[string]any {
	props := map[string]any{"id": n.id, "name": n.name}
	if n.uid != "" {
		props["uid"] = n.uid
	}
	if n.repoID != "" {
		props["repo_id"] = n.repoID
	}
	return map[string]any{"properties": props, "labels": []any{n.label}}
}

// twoTenantGraph is the fake backend: it answers anchor resolution, the
// resource-to-code traversal (applying the terminal-repository grant only when
// the statement carries it, as the backend would), the ownership statements
// (computed from the fixture edges), shortestPath, and the exposure walk.
type twoTenantGraph struct {
	// tracePaths are the node-id sequences (start ... Repository) the
	// resource-to-code traversal returns, in backend order.
	tracePaths [][]string
	// shortest is the node-id sequence shortestPath returns (nil: no path).
	shortest []string
	// exposureRows are the exposure walk rows: chain node ids plus a sink.
	exposureRows []twoTenantExposureRow

	// resolveNothing makes anchor resolution find no node: the "unknown
	// anchor" twin every ungranted-anchor response is compared against.
	resolveNothing bool

	mu    sync.Mutex
	calls []twoTenantCall
}

type twoTenantExposureRow struct {
	chain   []string
	sinkRel string
	sink    string
}

type twoTenantCall struct {
	kind   string
	params map[string]any
}

func (g *twoTenantGraph) record(kind string, params map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, twoTenantCall{kind: kind, params: params})
}

func (g *twoTenantGraph) callsOf(kind string) []twoTenantCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []twoTenantCall
	for _, call := range g.calls {
		if kind == "" || call.kind == kind {
			out = append(out, call)
		}
	}
	return out
}

func (g *twoTenantGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (g *twoTenantGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "AS label, n.id AS id"):
		g.record("resolve", params)
		if g.resolveNothing {
			return nil, nil
		}
		for _, key := range []string{"start_id", "source_id", "target_id"} {
			value, ok := params[key].(string)
			if !ok {
				continue
			}
			node, found := twoTenantNodeByKey(value)
			if !found {
				return nil, nil
			}
			row := map[string]any{"label": node.label, "id": node.id, "name": node.name, "labels": []any{node.label}}
			if node.uid != "" {
				row["uid"] = node.uid
			}
			if node.repoID != "" {
				row["repo_id"] = node.repoID
			}
			return []map[string]any{row}, nil
		}
		return nil, nil
	case strings.Contains(cypher, "$uids"):
		g.record("ownership", params)
		return g.ownershipRows(cypher, params), nil
	case strings.Contains(cypher, "shortestPath"):
		g.record("shortest", params)
		if g.shortest == nil {
			return nil, nil
		}
		return []map[string]any{g.pathRow(g.shortest)}, nil
	case strings.Contains(cypher, "(repo:Repository)"):
		g.record("trace", params)
		return g.traceRows(cypher, params), nil
	case strings.Contains(cypher, ":CALLS*"):
		g.record("exposure", params)
		return g.exposureWalkRows(params), nil
	}
	return nil, nil
}

func (g *twoTenantGraph) pathRow(ids []string) map[string]any {
	nodes := make([]any, 0, len(ids))
	rels := make([]any, 0, len(ids))
	for i, id := range ids {
		node, _ := twoTenantNodeByKey(id)
		nodes = append(nodes, node.graphNode())
		if i > 0 {
			rels = append(rels, map[string]any{"type": "DEPENDS_ON", "properties": map[string]any{"confidence": 0.9, "reason": "fixture"}})
		}
	}
	return map[string]any{"depth": int64(len(ids) - 1), "ns": nodes, "rels": rels}
}

func (g *twoTenantGraph) traceRows(cypher string, params map[string]any) []map[string]any {
	startID, _ := params["start_id"].(string)
	grantInCypher := strings.Contains(cypher, "$allowed_repository_ids")
	allowedRepos, _ := params["allowed_repository_ids"].([]string)
	allowedScopes, _ := params["allowed_scope_ids"].([]string)
	projectsNodes := strings.Contains(cypher, "nodes(path) AS ns")
	var rows []map[string]any
	for _, path := range g.tracePaths {
		if path[0] != startID {
			continue
		}
		repo, _ := twoTenantNodeByKey(path[len(path)-1])
		if grantInCypher && !slices.Contains(allowedRepos, repo.id) && !slices.Contains(allowedScopes, repo.id) {
			continue
		}
		row := g.pathRow(path)
		row["repo_id"] = repo.id
		row["repo_name"] = repo.name
		if !projectsNodes {
			delete(row, "ns")
		}
		rows = append(rows, row)
	}
	limit, _ := params["limit"].(int)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// ownershipRows answers the grant-free owner statements from the fixture
// edges: one (uid, owner repository) row per asked key and owning edge.
func (g *twoTenantGraph) ownershipRows(cypher string, params map[string]any) []map[string]any {
	uids, _ := params["uids"].([]string)
	owner := func(id string) string { node, _ := twoTenantNodeByKey(id); return node.repoID }
	var rows []map[string]any
	emit := func(edges [][2]string, keyed int, ownerOf func([2]string) string) {
		for _, edge := range edges {
			if slices.Contains(uids, edge[keyed]) {
				rows = append(rows, map[string]any{"uid": edge[keyed], "repo_id": ownerOf(edge)})
			}
		}
	}
	switch {
	case strings.Contains(cypher, "<-[:USES]-"):
		emit(twoTenantUses, 1, func(e [2]string) string { return owner(e[0]) })
	case strings.Contains(cypher, "<-[:MATCHES_STATE]-"):
		emit(twoTenantMatchesState, 1, func(e [2]string) string { return owner(e[0]) })
	case strings.Contains(cypher, "-[:DEPLOYMENT_SOURCE]->"):
		emit(twoTenantDeploymentSource, 0, func(e [2]string) string { return e[1] })
	}
	return rows
}

func (g *twoTenantGraph) exposureWalkRows(params map[string]any) []map[string]any {
	source, _ := params["source_entity_id"].(string)
	var rows []map[string]any
	for _, row := range g.exposureRows {
		if row.chain[0] != source {
			continue
		}
		chain := make([]any, 0, len(row.chain))
		for _, id := range row.chain {
			node, _ := twoTenantNodeByKey(id)
			chain = append(chain, exposureFixtureNode(node))
		}
		sink, _ := twoTenantNodeByKey(row.sink)
		rows = append(rows, map[string]any{
			"chain":       chain,
			"sink_rel":    row.sinkRel,
			"sink_node":   exposureFixtureNode(sink),
			"sink_labels": []any{sink.label},
			"depth":       int64(len(row.chain) - 1),
		})
	}
	return rows
}

// exposureFixtureNode renders a node as the flat map the exposure mapper reads.
func exposureFixtureNode(n twoTenantNode) map[string]any {
	node := map[string]any{"id": n.id, "name": n.name, "labels": []any{n.label}}
	if n.uid != "" {
		node["uid"] = n.uid
	}
	if n.repoID != "" {
		node["repo_id"] = n.repoID
	}
	if n.label == "CidrBlock" {
		node["is_internet"] = true
	}
	return node
}

// tenantAAuth is the scoped repo-a caller; tenantEmptyAuth has no grant.
func tenantAAuth() auth.AuthContext {
	return testutil.ScopedTestAuthContext("tenant-a", []string{"repo-a"})
}

// postImpact serves one request through Mount with the given auth (nil: an
// unauthenticated shared-key caller) and returns the recorder.
func postImpact(t *testing.T, h *Handler, path, body string, authCtx *auth.AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// assertNoTenantB fails when any repo-b identifier appears in body.
func assertNoTenantB(t *testing.T, body string) {
	t.Helper()
	for _, id := range tenantBIdentifiers {
		if strings.Contains(body, `"`+id+`"`) || strings.Contains(body, id) {
			t.Errorf("scoped response leaks tenant-B identifier %q: %s", id, body)
		}
	}
}

func newTwoTenantHandler(g *twoTenantGraph) *Handler {
	return &Handler{Neo4j: g, Profile: querycontract.ProfileLocalAuthoritative}
}
