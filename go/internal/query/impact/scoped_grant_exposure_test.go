// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

const exposureRoute = "/api/v0/impact/trace-exposure-path"

// exposureTwoTenantStore resolves the fixture's Function entities, each
// classified as an HTTP handler, by id and by (repo, exact name).
type exposureTwoTenantStore struct {
	content.FakePortContentStore
}

func exposureEntity(id string) *querycontract.EntityContent {
	node, ok := twoTenantNodeByKey(id)
	if !ok || node.label != "Function" {
		return nil
	}
	return &querycontract.EntityContent{
		EntityID: node.id, RepoID: node.repoID, EntityName: node.name, EntityType: "function",
		Metadata: map[string]any{"dead_code_root_kinds": []any{"go.net_http_handler_signature"}},
	}
}

func (exposureTwoTenantStore) GetEntityContent(_ context.Context, id string) (*querycontract.EntityContent, error) {
	return exposureEntity(id), nil
}

func (exposureTwoTenantStore) SearchEntitiesByName(_ context.Context, repoID, _, name string, _ int) ([]querycontract.EntityContent, error) {
	var out []querycontract.EntityContent
	for _, node := range twoTenantNodes {
		if node.label == "Function" && node.name == name && (repoID == "" || node.repoID == repoID) {
			out = append(out, *exposureEntity(node.id))
		}
	}
	return out, nil
}

func exposureFixtureRows() []twoTenantExposureRow {
	return []twoTenantExposureRow{
		{chain: []string{"fn-a", "fn-a2"}, sinkRel: "EXECUTES_SHELL", sink: "sh-a"}, // X3 kept
		{chain: []string{"fn-a", "fn-b"}, sinkRel: "EXECUTES_SHELL", sink: "sh-a"},  // X2 dropped: foreign interior
		{chain: []string{"fn-a"}, sinkRel: "EXECUTES_SHELL", sink: "sh-shared"},     // X4 dropped: repo-b wrote the shared uid
		{chain: []string{"fn-a"}, sinkRel: "CAN_PERFORM", sink: "cr-b"},             // dropped: foreign CloudResource sink
		{chain: []string{"fn-a"}, sinkRel: "CAN_PERFORM", sink: "cr-a"},             // kept: A1-owned CloudResource sink
		{chain: []string{"fn-a"}, sinkRel: "TO", sink: "cidr-1"},                    // X5 withheld sink class
		{chain: []string{"fn-b", "fn-a"}, sinkRel: "EXECUTES_SHELL", sink: "sh-a"},  // repo-b source
	}
}

func exposureHandler(g *twoTenantGraph) *Handler {
	h := newTwoTenantHandler(g)
	h.Content = exposureTwoTenantStore{}
	return h
}

func exposureSinkIDs(t *testing.T, data map[string]any) []string {
	t.Helper()
	paths, _ := data["paths"].([]any)
	var out []string
	for _, raw := range paths {
		path, _ := raw.(map[string]any)
		sink, _ := path["sink"].(map[string]any)
		node, _ := sink["node"].(map[string]any)
		id, _ := node["entity_id"].(string)
		out = append(out, id)
	}
	return out
}

// X1: a source name that exists only in repo-b, with repo_id omitted or set
// to repo-b, and a repo-b source_entity_id, all render as not found.
func TestScopedTraceExposurePathForeignSourceIsNotFound(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	unknown := postImpact(t, exposureHandler(&twoTenantGraph{}), exposureRoute, `{"source_entity_id":"fn-absent"}`, &a)
	for _, body := range []string{
		`{"source":"helperB"}`,
		`{"source":"helperB","repo_id":"repo-b"}`,
		`{"source_entity_id":"fn-b"}`,
	} {
		g := &twoTenantGraph{exposureRows: exposureFixtureRows()}
		rec := postImpact(t, exposureHandler(g), exposureRoute, body, &a)
		data := testutil.DecodeImpactEnvelopeData(t, rec)
		want := testutil.DecodeImpactEnvelopeData(t, unknown)
		if data["state"] != want["state"] || len(exposureSinkIDs(t, data)) != 0 {
			t.Errorf("body=%s got %s, want the not-found shape %s", body, rec.Body.String(), unknown.Body.String())
		}
		if n := len(g.callsOf("exposure")); n != 0 {
			t.Errorf("body=%s issued %d exposure walks for a foreign source, want 0", body, n)
		}
		assertNoTenantB(t, rec.Body.String())
	}
}

// X2-X5: foreign interiors, a shared uid last written by repo-b, a foreign
// CloudResource sink, and withheld sink classes are dropped; owned paths stay.
func TestScopedTraceExposurePathFiltersPaths(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	g := &twoTenantGraph{exposureRows: exposureFixtureRows()}
	rec := postImpact(t, exposureHandler(g), exposureRoute, `{"source_entity_id":"fn-a"}`, &a)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	got := exposureSinkIDs(t, data)
	slices.Sort(got)
	if strings.Join(got, ",") != "cr-a,sh-a" {
		t.Fatalf("sinks = %v, want [sh-a cr-a] (X3 kept, A1-owned CloudResource kept)", got)
	}
	coverage, _ := data["coverage"].(map[string]any)
	reason, _ := coverage["unresolved_reason"].(string)
	for _, class := range []string{"SecretsIAMSecretMetadataPath", "CidrBlock"} {
		if !strings.Contains(reason, class) {
			t.Errorf("unresolved_reason %q does not name withheld sink class %s", reason, class)
		}
	}
	if data["scoped"] != true {
		t.Errorf("scoped = %#v, want true", data["scoped"])
	}
	assertNoTenantB(t, rec.Body.String())
}

// X6: truncation comes from the raw row count, not the filtered one.
func TestScopedTraceExposurePathTruncatedFromRawCount(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	rows := make([]twoTenantExposureRow, 0, exposurePathResultLimit)
	for range exposurePathResultLimit {
		rows = append(rows, twoTenantExposureRow{chain: []string{"fn-a", "fn-b"}, sinkRel: "EXECUTES_SHELL", sink: "sh-a"})
	}
	g := &twoTenantGraph{exposureRows: rows}
	rec := postImpact(t, exposureHandler(g), exposureRoute, `{"source_entity_id":"fn-a"}`, &a)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	coverage, _ := data["coverage"].(map[string]any)
	if coverage["truncated"] != true {
		t.Fatalf("truncated = %#v, want true (raw page hit the limit though every row was withheld)", coverage["truncated"])
	}
	if got := exposureSinkIDs(t, data); len(got) != 0 {
		t.Fatalf("sinks = %v, want none", got)
	}
	assertNoTenantB(t, rec.Body.String())
}

// T4 for exposure: an empty grant makes zero graph calls.
func TestScopedTraceExposurePathEmptyGrantMakesNoGraphCalls(t *testing.T) {
	t.Parallel()
	empty := testutil.ScopedTestAuthContext("tenant-none", nil)
	g := &twoTenantGraph{exposureRows: exposureFixtureRows()}
	rec := postImpact(t, exposureHandler(g), exposureRoute, `{"source_entity_id":"fn-a"}`, &empty)
	if n := len(g.callsOf("")); n != 0 {
		t.Fatalf("graph calls = %d, want 0", n)
	}
	if got := exposureSinkIDs(t, testutil.DecodeImpactEnvelopeData(t, rec)); len(got) != 0 {
		t.Fatalf("sinks = %v, want none", got)
	}
}

// P3-1 (#5167 review): a chain or sink node rendered as a map with nested
// `properties` (the shape the impact path decoder accepts) is decoded to its
// id and repo_id, so an owned node is not denied for an empty identity.
func TestExposureOwnershipNodesReadsNestedProperties(t *testing.T) {
	t.Parallel()
	fnA, _ := twoTenantNodeByKey("fn-a")
	row := map[string]any{
		"chain":     []any{fnA.graphNode()},
		"sink_node": map[string]any{"properties": map[string]any{"id": "sh-a", "uid": "sh-a", "repo_id": "repo-a"}, "labels": []any{"ShellCommand"}},
	}
	nodes := exposureOwnershipNodes(row)
	if len(nodes) != 2 || nodes[0].ID != "fn-a" || nodes[0].RepoID != "repo-a" || nodes[1].UID != "sh-a" || nodes[1].RepoID != "repo-a" {
		t.Fatalf("nodes = %+v, want fn-a and sh-a with repo-a", nodes)
	}
	if !slices.Equal(nodes[1].Labels, []string{"ShellCommand"}) {
		t.Fatalf("sink labels = %v", nodes[1].Labels)
	}
}
