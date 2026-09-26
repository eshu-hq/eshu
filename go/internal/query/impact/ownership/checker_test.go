// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// recordingGraph answers owner statements: every asked key in admit is
// returned with repo-a as its owner (and, when foreign is set, also with
// repo-b), and each statement's params are recorded.
type recordingGraph struct {
	admit   map[string]bool
	foreign map[string]bool // asked keys owned only by repo-b
	extra   []string        // keys returned though never asked (a misbehaving backend)
	calls   []map[string]any
	texts   []string
}

func (g *recordingGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.calls = append(g.calls, params)
	g.texts = append(g.texts, cypher)
	var rows []map[string]any
	uids, _ := params["uids"].([]string)
	for _, uid := range uids {
		if g.admit[uid] {
			rows = append(rows, map[string]any{"uid": uid, "repo_id": "repo-a"})
		}
		if g.foreign[uid] {
			rows = append(rows, map[string]any{"uid": uid, "repo_id": "repo-b"}, map[string]any{"uid": uid, "repo_id": ""})
		}
	}
	for _, uid := range g.extra {
		rows = append(rows, map[string]any{"uid": uid, "repo_id": "repo-a"})
	}
	return rows, nil
}

func (g *recordingGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func grantA() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}, AllowedScopeIDs: []string{"scope-a"}}
}

func cr(uid string) Node { return Node{ID: uid, UID: uid, Labels: []string{"CloudResource"}} }

func TestClassOfPrecedenceAndDenyByDefault(t *testing.T) {
	t.Parallel()
	cases := map[string]Class{
		"Repository":                   ClassRepository,
		"WorkloadInstance":             ClassWorkloadInstance,
		"Function":                     ClassRepoOwned,
		"ShellCommand":                 ClassRepoOwned,
		"SqlTable":                     ClassRepoOwned,
		"CloudResource":                ClassCloudResource,
		"TerraformStateResource":       ClassTerraformStateResource,
		"Platform":                     ClassUngranted,
		"CidrBlock":                    ClassUngranted,
		"SecretsIAMSecretMetadataPath": ClassUngranted,
		"Endpoint":                     ClassUngranted,
		"":                             ClassUngranted,
	}
	for label, want := range cases {
		if got := ClassOf([]string{label}); got != want {
			t.Errorf("ClassOf(%q) = %v, want %v", label, got, want)
		}
	}
	if got := ClassOf([]string{"CloudResource", "Repository"}); got != ClassRepository {
		t.Errorf("multi-label precedence = %v, want Repository", got)
	}
}

func TestCheckUnscopedAndEmptyGrantMakeNoGraphCalls(t *testing.T) {
	t.Parallel()
	g := &recordingGraph{admit: map[string]bool{"cr-1": true}}
	c := Checker{Graph: g}
	verdict, err := c.Check(context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, []Node{cr("cr-1")})
	if err != nil || !verdict.Admits(cr("cr-9")) {
		t.Fatalf("unscoped verdict must admit everything: %v", err)
	}
	verdict, err = c.Check(context.Background(), querycontract.RepositoryAccessFilter{}, []Node{cr("cr-1")})
	if err != nil || verdict.Admits(cr("cr-1")) {
		t.Fatalf("empty grant must admit nothing: %v", err)
	}
	if len(g.calls) != 0 {
		t.Fatalf("graph calls = %d, want 0", len(g.calls))
	}
}

func TestCheckGoClassesAndSkipsClassesWithoutKeys(t *testing.T) {
	t.Parallel()
	g := &recordingGraph{}
	verdict, err := Checker{Graph: g}.Check(context.Background(), grantA(), []Node{
		{ID: "repo-a", Labels: []string{"Repository"}},
		{ID: "fn-1", RepoID: "repo-a", Labels: []string{"Function"}},
		{ID: "wi-a", RepoID: "scope-a", Labels: []string{"WorkloadInstance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 0 {
		t.Fatalf("graph calls = %d, want 0 when every node is Go-decided", len(g.calls))
	}
	for _, node := range []Node{
		{ID: "repo-a", Labels: []string{"Repository"}},
		{ID: "fn-1", RepoID: "repo-a", Labels: []string{"Function"}},
		{ID: "wi-a", RepoID: "scope-a", Labels: []string{"WorkloadInstance"}},
	} {
		if !verdict.Admits(node) {
			t.Errorf("Admits(%+v) = false, want true", node)
		}
	}
	for _, node := range []Node{
		{ID: "repo-b", Labels: []string{"Repository"}},
		{ID: "fn-2", RepoID: "repo-b", Labels: []string{"Function"}},
		{ID: "fn-3", Labels: []string{"Function"}},
		{ID: "p", Labels: []string{"Platform"}},
		{},
	} {
		if verdict.Admits(node) {
			t.Errorf("Admits(%+v) = true, want false", node)
		}
	}
}

func TestCheckDedupesChunksAndIgnoresUnaskedRows(t *testing.T) {
	t.Parallel()
	admit := map[string]bool{}
	var nodes []Node
	for i := range 1200 {
		uid := fmt.Sprintf("cr-%04d", i)
		admit[uid] = i%2 == 0
		nodes = append(nodes, cr(uid), cr(uid)) // every key twice
	}
	foreign := map[string]bool{}
	for uid, owned := range admit {
		foreign[uid] = !owned
	}
	g := &recordingGraph{admit: admit, foreign: foreign, extra: []string{"cr-foreign"}}
	verdict, err := Checker{Graph: g}.Check(context.Background(), grantA(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	if want := (1200 + ChunkSize - 1) / ChunkSize; len(g.calls) != want {
		t.Fatalf("statements = %d, want %d chunks of <= %d", len(g.calls), want, ChunkSize)
	}
	total := 0
	for i, call := range g.calls {
		uids := call["uids"].([]string)
		if len(uids) > ChunkSize {
			t.Fatalf("chunk %d carries %d keys", i, len(uids))
		}
		total += len(uids)
		if _, ok := call["grant_ids"]; ok {
			t.Fatalf("owner statement %d carries the grant; the grant is applied in Go", i)
		}
		if g.texts[i] != CloudResourceOwnerCypher {
			t.Fatalf("statement %d is not the CloudResource owner statement: %s", i, g.texts[i])
		}
	}
	if total != 1200 {
		t.Fatalf("checked keys = %d, want the 1200 distinct keys", total)
	}
	if !verdict.Admits(cr("cr-0000")) || verdict.Admits(cr("cr-0001")) {
		t.Fatal("verdict does not follow the owner repository against the grant (repo-b and empty owners must not admit)")
	}
	if verdict.Admits(cr("cr-foreign")) {
		t.Fatal("a key the backend returned but was never asked about must not be admitted")
	}
}

func TestCheckCapLeavesTailUngrantedAndCapped(t *testing.T) {
	t.Parallel()
	grant := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}
	for i := range 999 {
		grant.AllowedRepositoryIDs = append(grant.AllowedRepositoryIDs, fmt.Sprintf("repo-%d", i))
	}
	limit := CheckedKeyCap(len(grant.AllowedRepositoryIDs))
	admit := map[string]bool{}
	var nodes []Node
	for i := range limit + 5 {
		uid := fmt.Sprintf("cr-%d", i)
		admit[uid] = true
		nodes = append(nodes, cr(uid))
	}
	g := &recordingGraph{admit: admit}
	checker := Checker{Graph: g}
	verdict, err := checker.Check(context.Background(), grant, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Capped() {
		t.Fatal("Capped() = false past the cap")
	}
	if !verdict.Admits(nodes[0]) || verdict.Admits(nodes[len(nodes)-1]) {
		t.Fatal("keys inside the cap must be judged, keys past it ungranted")
	}
	paths := make([][]Node, 0, len(nodes)+1)
	for _, node := range nodes {
		paths = append(paths, []Node{node})
	}
	paths = append(paths, nil)
	filter, err := checker.FilterPaths(context.Background(), grant, paths)
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Capped || filter.Kept() != limit || !filter.Keep[0] || filter.Keep[len(nodes)-1] || filter.Keep[len(nodes)] {
		t.Fatalf("FilterPaths kept %d (capped=%v), want the first %d kept, the tail and the empty path withheld", filter.Kept(), filter.Capped, limit)
	}
	if got := (Checker{}).withholdReason(verdict, []Node{nodes[len(nodes)-1]}); got != ReasonUncheckedOverCap {
		t.Fatalf("withhold reason = %q, want %q", got, ReasonUncheckedOverCap)
	}
}

func TestCheckedKeyCapIsGrantIndependent(t *testing.T) {
	t.Parallel()
	cases := map[int]int{0: 0, 1: MaxCheckedKeys, 8: MaxCheckedKeys, 128: MaxCheckedKeys, 1_000_000: MaxCheckedKeys}
	for grant, want := range cases {
		if got := CheckedKeyCap(grant); got != want {
			t.Errorf("CheckedKeyCap(%d) = %d, want %d", grant, got, want)
		}
	}
}

func TestWorkloadInstanceRescueUsesIDKey(t *testing.T) {
	t.Parallel()
	g := &recordingGraph{admit: map[string]bool{"wi-rescued": true}}
	wi := Node{ID: "wi-rescued", RepoID: "repo-b", Labels: []string{"WorkloadInstance"}}
	verdict, err := Checker{Graph: g}.Check(context.Background(), grantA(), []Node{wi})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 1 || g.texts[0] != WorkloadInstanceOwnerCypher || !strings.Contains(g.texts[0], "wi.id IN $uids") {
		t.Fatalf("rescue statement = %v, want one DEPLOYMENT_SOURCE owner statement on wi.id", g.texts)
	}
	if !verdict.Admits(wi) {
		t.Fatal("DEPLOYMENT_SOURCE-rescued instance not admitted")
	}
}

func TestCheckRowLimitLeavesChunkUndecidedAndCapped(t *testing.T) {
	t.Parallel()
	g := &fanInGraph{}
	nodes := []Node{cr("cr-owned-late"), cr("cr-2")}
	verdict, err := Checker{Graph: g}.Check(context.Background(), grantA(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	if g.rowLimit != RowLimit {
		t.Fatalf("row_limit param = %d, want %d", g.rowLimit, RowLimit)
	}
	if !verdict.Capped() || verdict.Admits(nodes[0]) || verdict.Admits(nodes[1]) {
		t.Fatal("a chunk that hit RowLimit before any granted owner must leave its keys ungranted and the verdict capped")
	}
	if got := (Checker{}).withholdReason(verdict, nodes[:1]); got != ReasonUncheckedOverCap {
		t.Fatalf("withhold reason = %q, want %q", got, ReasonUncheckedOverCap)
	}
}

// fanInGraph returns RowLimit foreign-owner rows, as a widely shared node
// would, so the granted owner never makes the page.
type fanInGraph struct{ rowLimit int }

func (g *fanInGraph) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	g.rowLimit, _ = params["row_limit"].(int)
	rows := make([]map[string]any, 0, g.rowLimit)
	for i := range g.rowLimit {
		rows = append(rows, map[string]any{"uid": "cr-owned-late", "repo_id": fmt.Sprintf("repo-x%04d", i)})
	}
	return rows, nil
}

func (g *fanInGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// B1: on the exposure route a path whose sink is a withheld sink class is
// withheld_sink_class, even past an unowned interior; on another route the
// same path is ungranted_node, and an owned-sink exposure path with an
// unowned interior stays ungranted_node.
func TestWithholdReasonNamesWithheldSinkClassOnExposureRoute(t *testing.T) {
	t.Parallel()
	verdict := Verdict{access: grantA(), admitted: map[Class]map[string]struct{}{}}
	foreign := Node{ID: "fn-b", RepoID: "repo-b", Labels: []string{"Function"}}
	for _, label := range WithheldSinkLabels {
		path := []Node{foreign, {ID: "sink-1", Labels: []string{label}}}
		if got := (Checker{Route: RouteTraceExposurePath}).withholdReason(verdict, path); got != ReasonWithheldSinkClass {
			t.Errorf("%s sink on exposure route: reason = %q, want %q", label, got, ReasonWithheldSinkClass)
		}
		if got := (Checker{Route: RouteTraceResourceToCode}).withholdReason(verdict, path); got != ReasonUngrantedNode {
			t.Errorf("%s node on trace route: reason = %q, want %q", label, got, ReasonUngrantedNode)
		}
	}
	owned := []Node{foreign, {ID: "fn-a", RepoID: "repo-a", Labels: []string{"Function"}}}
	if got := (Checker{Route: RouteTraceExposurePath}).withholdReason(verdict, owned); got != ReasonUngrantedNode {
		t.Errorf("owned sink, foreign interior: reason = %q, want %q", got, ReasonUngrantedNode)
	}
}
