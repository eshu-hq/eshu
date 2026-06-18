package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

type recordingCloudSinkGraph struct {
	rows          []map[string]any
	rowsByCall    [][]map[string]any
	seenCypher    string
	seenCyphers   []string
	seenParams    map[string]any
	seenParamSets []map[string]any
}

func (g *recordingCloudSinkGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.seenCypher = cypher
	g.seenCyphers = append(g.seenCyphers, cypher)
	g.seenParams = params
	g.seenParamSets = append(g.seenParamSets, params)
	if len(g.rowsByCall) > 0 {
		rows := g.rowsByCall[0]
		g.rowsByCall = g.rowsByCall[1:]
		return append([]map[string]any(nil), rows...), nil
	}
	return append([]map[string]any(nil), g.rows...), nil
}

func TestGraphValueFlowCloudSinkTargetLoaderLoadsInvokesCloudActionBridge(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	graph := &recordingCloudSinkGraph{rows: []map[string]any{
		{
			"function_uid": "uid-handler",
			"sink_kind":    string(exposure.SinkIAMPrivilegedAction),
			"sink_label":   "IAM effective privileged action",
		},
	}}
	loader := GraphValueFlowCloudSinkTargetLoader{Graph: graph}

	targets, err := loader.LoadCloudSinkTargets(context.Background(), map[summary.FunctionID]string{fn: "uid-handler"})
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1: %+v", len(targets), targets)
	}
	if targets[0].FunctionID != fn || targets[0].Kind != string(exposure.SinkIAMPrivilegedAction) ||
		targets[0].Label != "IAM effective privileged action" {
		t.Fatalf("target = %+v, want IAM cloud-action bridge target for function", targets[0])
	}
	if strings.Contains(graph.seenCypher, "OPTIONAL MATCH") || strings.Contains(graph.seenCypher, "MATCH (fn)") {
		t.Fatalf("cloud sink query must stay anchored and scalar, got:\n%s", graph.seenCypher)
	}
	if !strings.Contains(graph.seenCypher, "MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)") {
		t.Fatalf("cloud sink query must use materialized cloud-action bridge:\n%s", graph.seenCypher)
	}
	if !strings.Contains(graph.seenCypher, "fn.uid IN $function_uids") {
		t.Fatalf("cloud sink query must stay bounded by function uid:\n%s", graph.seenCypher)
	}
	if _, ok := graph.seenParams["sink_rels"]; ok {
		t.Fatalf("cloud-action bridge query should not pass unused sink_rels param: %+v", graph.seenParams)
	}
}

func TestGraphValueFlowCloudSinkTargetLoaderSkipsAmbiguousGraphUID(t *testing.T) {
	t.Parallel()

	first := summary.NewFunctionID("repo-a", "pkg", "", "first")
	second := summary.NewFunctionID("repo-a", "pkg", "", "second")
	graph := &recordingCloudSinkGraph{rows: []map[string]any{
		{
			"function_uid": "uid-shared",
			"sink_rel":     "CAN_ASSUME",
			"sink_labels":  []string{"CloudResource"},
		},
	}}
	loader := GraphValueFlowCloudSinkTargetLoader{Graph: graph}

	targets, err := loader.LoadCloudSinkTargets(context.Background(), map[summary.FunctionID]string{
		first:  "uid-shared",
		second: "uid-shared",
	})
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("ambiguous graph uid produced targets: %+v", targets)
	}
	if len(graph.seenParamSets) != 0 {
		t.Fatalf("ambiguous graph uid should not issue graph query, params=%+v", graph.seenParamSets)
	}
}

func TestGraphValueFlowCloudSinkTargetLoaderChunksFunctionUIDs(t *testing.T) {
	t.Parallel()

	graphIDs := make(map[summary.FunctionID]string, valueFlowCloudSinkTargetBatchLimit+1)
	for i := 0; i < valueFlowCloudSinkTargetBatchLimit+1; i++ {
		graphIDs[summary.NewFunctionID("repo-a", "pkg", "", fmt.Sprintf("fn%d", i))] = fmt.Sprintf("uid-%d", i)
	}
	graph := &recordingCloudSinkGraph{}
	loader := GraphValueFlowCloudSinkTargetLoader{Graph: graph}

	if _, err := loader.LoadCloudSinkTargets(context.Background(), graphIDs); err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	if len(graph.seenParamSets) != 2 {
		t.Fatalf("graph calls = %d, want 2 chunks", len(graph.seenParamSets))
	}
	for _, params := range graph.seenParamSets {
		uids, ok := params["function_uids"].([]string)
		if !ok {
			t.Fatalf("function_uids param type = %T, want []string", params["function_uids"])
		}
		if len(uids) > valueFlowCloudSinkTargetBatchLimit {
			t.Fatalf("chunk size = %d, want <= %d", len(uids), valueFlowCloudSinkTargetBatchLimit)
		}
	}
}

func TestGraphValueFlowCloudSinkTargetLoaderEmptyAndNilGuards(t *testing.T) {
	t.Parallel()

	loader := GraphValueFlowCloudSinkTargetLoader{Graph: &recordingCloudSinkGraph{}}
	targets, err := loader.LoadCloudSinkTargets(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty graph id map returned error: %v", err)
	}
	if targets != nil {
		t.Fatalf("empty graph id map targets = %+v, want nil", targets)
	}

	nilGraph := GraphValueFlowCloudSinkTargetLoader{}
	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	if _, err := nilGraph.LoadCloudSinkTargets(context.Background(), map[summary.FunctionID]string{fn: "uid-handler"}); err == nil {
		t.Fatal("nil graph must error rather than silently drop cloud sinks")
	}
}

func valueFlowTestContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
