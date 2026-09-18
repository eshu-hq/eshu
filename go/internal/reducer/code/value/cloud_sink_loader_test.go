// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// statementCloudSinkGraph answers the two cloud sink statements separately so a
// test can seed the raw workload rows and the sink rows independently, and can
// see exactly which pairs the loader sent to the second statement.
type statementCloudSinkGraph struct {
	workloadRows []map[string]any
	sinkRows     []map[string]any
	calls        []cloudSinkGraphCall
}

type cloudSinkGraphCall struct {
	cypher string
	params map[string]any
}

func (g *statementCloudSinkGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.calls = append(g.calls, cloudSinkGraphCall{cypher: cypher, params: params})
	switch cypher {
	case CloudSinkWorkloadRowsCypher:
		return append([]map[string]any(nil), g.workloadRows...), nil
	case CloudSinkTargetsByPairCypher:
		return append([]map[string]any(nil), g.sinkRows...), nil
	default:
		return nil, fmt.Errorf("unexpected cypher:\n%s", cypher)
	}
}

func (g *statementCloudSinkGraph) callsFor(cypher string) []cloudSinkGraphCall {
	var out []cloudSinkGraphCall
	for _, call := range g.calls {
		if call.cypher == cypher {
			out = append(out, call)
		}
	}
	return out
}

// sentPairs flattens every pair the loader bound to the second statement.
func (g *statementCloudSinkGraph) sentPairs(t *testing.T) []cloudSinkPair {
	t.Helper()
	var pairs []cloudSinkPair
	for _, call := range g.callsFor(CloudSinkTargetsByPairCypher) {
		raw, ok := call.params["pairs"].([]map[string]any)
		if !ok {
			t.Fatalf("pairs param type = %T, want []map[string]any", call.params["pairs"])
		}
		for _, p := range raw {
			pairs = append(pairs, cloudSinkPair{
				FunctionUID: p["function_uid"].(string),
				Action:      p["action"].(string),
				WorkloadID:  p["workload_id"].(string),
			})
		}
	}
	return pairs
}

func workloadRow(uid, action, workloadID string) map[string]any {
	return map[string]any{"function_uid": uid, "action": action, "workload_id": workloadID}
}

func iamSinkRow(uid string) map[string]any {
	return map[string]any{"function_uid": uid, "sink_rel": "CAN_PERFORM", "sink_labels": []string{"CloudResource"}}
}

func TestCloudSinkLoaderResolvesSingleWorkloadFunction(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	graph := &statementCloudSinkGraph{
		workloadRows: []map[string]any{workloadRow("uid-handler", "s3:GetObject", "wl-1")},
		sinkRows:     []map[string]any{iamSinkRow("uid-handler")},
	}
	targets, err := GraphCloudSinkTargetLoader{Graph: graph}.LoadCloudSinkTargets(
		context.Background(), map[summary.FunctionID]string{fn: "uid-handler"})
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	want := []CloudSinkTarget{{
		FunctionID: fn,
		Kind:       string(exposure.SinkIAMPrivilegedAction),
		Label:      "IAM effective privileged action",
	}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %+v, want %+v", targets, want)
	}
	if got, want := graph.sentPairs(t), []cloudSinkPair{{"uid-handler", "s3:GetObject", "wl-1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairs sent = %+v, want %+v", got, want)
	}
	uidCalls := graph.callsFor(CloudSinkWorkloadRowsCypher)
	if len(uidCalls) != 1 {
		t.Fatalf("workload-row calls = %d, want 1", len(uidCalls))
	}
	if uids, ok := uidCalls[0].params["function_uids"].([]string); !ok || !reflect.DeepEqual(uids, []string{"uid-handler"}) {
		t.Fatalf("function_uids = %#v, want []string{\"uid-handler\"}", uidCalls[0].params["function_uids"])
	}
}

// TestCloudSinkLoaderSelectsPairsFailClosed covers the Go-side replacement for
// the old collect(DISTINCT workload) / size(workloads) = 1 filter.
func TestCloudSinkLoaderSelectsPairsFailClosed(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		// One workload, reached through two RUNS_IN rows: still one workload.
		workloadRow("uid-dup", "s3:GetObject", "wl-1"),
		workloadRow("uid-dup", "s3:GetObject", "wl-1"),
		// Two workloads: ambiguous, so excluded.
		workloadRow("uid-two", "s3:GetObject", "wl-1"),
		workloadRow("uid-two", "s3:GetObject", "wl-2"),
		// A workload with no id cannot be anchored, and makes the pair
		// ambiguous even alongside an identified workload.
		workloadRow("uid-blank", "s3:GetObject", ""),
		workloadRow("uid-mixed", "s3:GetObject", "wl-1"),
		workloadRow("uid-mixed", "s3:GetObject", " "),
		// An action with no name can never match an allowed-action list.
		workloadRow("uid-noaction", "", "wl-1"),
		// Two actions on one workload are two independent pairs.
		workloadRow("uid-multi", "sqs:SendMessage", "wl-3"),
		workloadRow("uid-multi", "s3:PutObject", "wl-3"),
		// Values reach the second statement exactly as stored; only the
		// emptiness test trims.
		workloadRow("uid-padded", " s3:Padded ", " wl-4"),
	}
	pairs, stats := selectCloudSinkPairs(rows)
	want := []cloudSinkPair{
		{"uid-dup", "s3:GetObject", "wl-1"},
		{"uid-multi", "s3:PutObject", "wl-3"},
		{"uid-multi", "sqs:SendMessage", "wl-3"},
		{"uid-padded", " s3:Padded ", " wl-4"},
	}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("pairs = %+v, want %+v", pairs, want)
	}
	wantStats := cloudSinkPairStats{MultiWorkloadDropped: 1, UnresolvedDropped: 3}
	if stats != wantStats {
		t.Fatalf("stats = %+v, want %+v", stats, wantStats)
	}
}

func TestCloudSinkLoaderSkipsSinkQueryWhenNoPairSurvives(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	graph := &statementCloudSinkGraph{
		workloadRows: []map[string]any{
			workloadRow("uid-handler", "s3:GetObject", "wl-1"),
			workloadRow("uid-handler", "s3:GetObject", "wl-2"),
		},
		sinkRows: []map[string]any{iamSinkRow("uid-handler")},
	}
	targets, err := GraphCloudSinkTargetLoader{Graph: graph}.LoadCloudSinkTargets(
		context.Background(), map[summary.FunctionID]string{fn: "uid-handler"})
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("a function in two workloads produced targets: %+v", targets)
	}
	if calls := graph.callsFor(CloudSinkTargetsByPairCypher); len(calls) != 0 {
		t.Fatalf("sink statement ran %d times with no surviving pair", len(calls))
	}
}

func TestCloudSinkLoaderDoesNotPromoteCatalogOnlyConfigAndIaCSinks(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	graph := &statementCloudSinkGraph{
		workloadRows: []map[string]any{workloadRow("uid-handler", "s3:GetObject", "wl-1")},
		sinkRows: []map[string]any{
			{"function_uid": "uid-handler", "sink_rel": "WRITES_CONFIG", "sink_labels": []string{"ConfigKey"}},
			{"function_uid": "uid-handler", "sink_rel": "DECLARES_IAC_MISCONFIG", "sink_labels": []string{"TerraformResource"}},
		},
	}
	targets, err := GraphCloudSinkTargetLoader{Graph: graph}.LoadCloudSinkTargets(
		context.Background(), map[summary.FunctionID]string{fn: "uid-handler"})
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

func TestCloudSinkLoaderSkipsAmbiguousGraphUID(t *testing.T) {
	t.Parallel()

	first := summary.NewFunctionID("repo-a", "pkg", "", "first")
	second := summary.NewFunctionID("repo-a", "pkg", "", "second")
	graph := &statementCloudSinkGraph{}
	targets, err := GraphCloudSinkTargetLoader{Graph: graph}.LoadCloudSinkTargets(context.Background(), map[summary.FunctionID]string{
		first:  "uid-shared",
		second: "uid-shared",
	})
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("ambiguous graph uid produced targets: %+v", targets)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("ambiguous graph uid should not issue graph queries, calls=%d", len(graph.calls))
	}
}

func TestCloudSinkLoaderChunksBothStatements(t *testing.T) {
	t.Parallel()

	n := valueFlowCloudSinkTargetBatchLimit + 1
	graphIDs := make(map[summary.FunctionID]string, n)
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		uid := fmt.Sprintf("uid-%04d", i)
		graphIDs[summary.NewFunctionID("repo-a", "pkg", "", fmt.Sprintf("fn%d", i))] = uid
		rows = append(rows, workloadRow(uid, "s3:GetObject", "wl-1"))
	}
	graph := &statementCloudSinkGraph{workloadRows: rows}
	if _, err := (GraphCloudSinkTargetLoader{Graph: graph}).LoadCloudSinkTargets(context.Background(), graphIDs); err != nil {
		t.Fatalf("LoadCloudSinkTargets returned error: %v", err)
	}
	uidCalls := graph.callsFor(CloudSinkWorkloadRowsCypher)
	if len(uidCalls) != 2 {
		t.Fatalf("workload-row calls = %d, want 2 chunks", len(uidCalls))
	}
	for _, call := range uidCalls {
		if uids := call.params["function_uids"].([]string); len(uids) > valueFlowCloudSinkTargetBatchLimit {
			t.Fatalf("uid chunk size = %d, want <= %d", len(uids), valueFlowCloudSinkTargetBatchLimit)
		}
	}
	// The fake returns every row on each chunk call, so both chunks yield the
	// same uids; the loader must still send each pair once.
	pairCalls := graph.callsFor(CloudSinkTargetsByPairCypher)
	if len(pairCalls) != 2 {
		t.Fatalf("pair calls = %d, want 2 chunks", len(pairCalls))
	}
	if got := len(graph.sentPairs(t)); got != n {
		t.Fatalf("pairs sent = %d, want %d", got, n)
	}
}

func TestCloudSinkLoaderEmptyAndNilGuards(t *testing.T) {
	t.Parallel()

	loader := GraphCloudSinkTargetLoader{Graph: &statementCloudSinkGraph{}}
	targets, err := loader.LoadCloudSinkTargets(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty graph id map returned error: %v", err)
	}
	if targets != nil {
		t.Fatalf("empty graph id map targets = %+v, want nil", targets)
	}

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	if _, err := (GraphCloudSinkTargetLoader{}).LoadCloudSinkTargets(context.Background(), map[summary.FunctionID]string{fn: "uid-handler"}); err == nil {
		t.Fatal("nil graph must error rather than silently drop cloud sinks")
	}
}

// TestCloudSinkStatementsAvoidTheShapesNornicDBMisanswers pins the two
// statements away from the shapes measured wrong on NornicDB v1.3.3 (#6690):
// in-query aggregation and list subscripts in the first, and multi-hop MATCH
// clauses in the second. The backend-conformance corpus runs these exact
// statements live; this guard catches a rewrite before it gets that far.
func TestCloudSinkStatementsAvoidTheShapesNornicDBMisanswers(t *testing.T) {
	t.Parallel()

	for _, banned := range []string{"collect(", "DISTINCT", "size(", "[0]", "WITH "} {
		if strings.Contains(CloudSinkWorkloadRowsCypher, banned) {
			t.Errorf("workload-row statement contains %q:\n%s", banned, CloudSinkWorkloadRowsCypher)
		}
	}
	if !strings.HasPrefix(CloudSinkTargetsByPairCypher, "UNWIND $pairs AS pair\n") {
		t.Errorf("sink statement must start with UNWIND $pairs:\n%s", CloudSinkTargetsByPairCypher)
	}
	for _, line := range strings.Split(CloudSinkTargetsByPairCypher, "\n") {
		if strings.HasPrefix(line, "MATCH ") && strings.Count(line, "]-") > 1 {
			t.Errorf("sink statement has a multi-hop MATCH: %q", line)
		}
	}
}
