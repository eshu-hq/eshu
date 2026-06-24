// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestGraphValueFlowCloudSinkTargetLoaderLoadsCloudActionPermissions(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	graph := &recordingCloudSinkGraph{rows: []map[string]any{
		{
			"function_uid": "uid-handler",
			"sink_rel":     "CAN_PERFORM",
			"sink_labels":  []string{"CloudResource"},
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
		t.Fatalf("target = %+v, want correlated IAM cloud-action target", targets[0])
	}
	if len(graph.seenCyphers) != 1 {
		t.Fatalf("graph calls = %d, want one bridge-aware permission read", len(graph.seenCyphers))
	}
	cypher := graph.seenCyphers[0]
	for _, want := range []string{
		"MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)",
		"fn.uid IN $function_uids",
		"RUNS_IN",
		"INSTANCE_OF",
		"USES",
		"CAN_PERFORM",
		"WHERE size(workloads) = 1",
		"action.action IN sinkRel.actions",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cloud-action permission query missing %q:\n%s", want, cypher)
		}
	}
	if _, ok := graph.seenParams["sink_rels"]; ok {
		t.Fatalf("cloud-action permission query should not pass unused sink_rels param: %+v", graph.seenParams)
	}
}

func TestGraphValueFlowCloudSinkTargetLoaderDoesNotPromoteCatalogOnlyConfigAndIaCSinks(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	graph := &recordingCloudSinkGraph{rows: []map[string]any{
		{
			"function_uid": "uid-handler",
			"sink_rel":     "WRITES_CONFIG",
			"sink_labels":  []string{"ConfigKey"},
		},
		{
			"function_uid": "uid-handler",
			"sink_rel":     "DECLARES_IAC_MISCONFIG",
			"sink_labels":  []string{"TerraformResource"},
		},
	}}
	loader := GraphValueFlowCloudSinkTargetLoader{Graph: graph}

	targets, err := loader.LoadCloudSinkTargets(context.Background(), map[summary.FunctionID]string{fn: "uid-handler"})
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("catalog-only config/IaC sinks produced fixpoint targets: %+v", targets)
	}
	if _, ok := exposure.MatchSink("WRITES_CONFIG", "ConfigKey", map[string]string{"key": "tls.insecure_skip_verify"}); ok {
		t.Fatalf("%q must stay non-GraphBacked until #3191 adds a Function-anchored loader path", exposure.SinkConfigSecurityKey)
	}
	if _, ok := exposure.MatchSink("DECLARES_IAC_MISCONFIG", "TerraformResource", map[string]string{"acl": "public-read"}); ok {
		t.Fatalf("%q must stay non-GraphBacked until #3191 adds a Function-anchored loader path", exposure.SinkIaCMisconfiguration)
	}
}

func TestGraphValueFlowCloudSinkTargetLoaderSkipsAmbiguousGraphUID(t *testing.T) {
	t.Parallel()

	first := summary.NewFunctionID("repo-a", "pkg", "", "first")
	second := summary.NewFunctionID("repo-a", "pkg", "", "second")
	graph := &recordingCloudSinkGraph{rows: []map[string]any{
		{
			"function_uid": "uid-shared",
			"sink_rel":     "CAN_PERFORM",
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
