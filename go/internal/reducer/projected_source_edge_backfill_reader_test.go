// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
)

// sequencedGraphQueryRunner returns one canned result per call, in call order,
// and records every (cypher, params) pair issued. It is used to prove
// ProjectedSourceEdgeBackfillReader.EnumerateProjectedSourceEdges issues one
// scan per distinct source-node label rather than a single query.
type sequencedGraphQueryRunner struct {
	calls   []stubGraphQueryCall
	results [][]map[string]any
	err     error
}

func (f *sequencedGraphQueryRunner) Run(
	_ context.Context, cypher string, params map[string]any,
) ([]map[string]any, error) {
	idx := len(f.calls)
	f.calls = append(f.calls, stubGraphQueryCall{cypher: cypher, params: params})
	if f.err != nil {
		return nil, f.err
	}
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return nil, nil
}

// TestProjectedSourceEdgeBackfillReaderScansBothSourceLabels proves the
// production reader (backed by GraphQueryRunner) enumerates BOTH the
// :CloudResource source-label family (AWS/Azure/GCP relationship edges,
// observability coverage edges, and the security-group reachability SG->rule
// edge) and the :SecurityGroupRule source-label family (the security-group
// reachability rule->endpoint TO edge, issue #4881), merging both scans' rows
// into one result set.
func TestProjectedSourceEdgeBackfillReaderScansBothSourceLabels(t *testing.T) {
	t.Parallel()

	graph := &sequencedGraphQueryRunner{
		results: [][]map[string]any{
			{
				{
					"source_uid":      "sg-uid-a",
					"scope_id":        "scope-1",
					"generation_id":   "gen-1",
					"evidence_source": securityGroupReachabilityEvidenceSource,
				},
			},
			{
				{
					"source_uid":      "rule-uid-a",
					"scope_id":        "scope-1",
					"generation_id":   "gen-1",
					"evidence_source": securityGroupReachabilityEvidenceSource,
				},
			},
		},
	}
	reader := ProjectedSourceEdgeBackfillReader{Graph: graph}

	rows, err := reader.EnumerateProjectedSourceEdges(context.Background(), []string{securityGroupReachabilityEvidenceSource})
	if err != nil {
		t.Fatalf("EnumerateProjectedSourceEdges returned error: %v", err)
	}
	if len(graph.calls) != 2 {
		t.Fatalf("graph.Run calls = %d, want 2 (one per source-node label)", len(graph.calls))
	}
	if !strings.Contains(graph.calls[0].cypher, "(s:CloudResource)-[r]->()") {
		t.Fatalf("first scan must enumerate :CloudResource sources:\n%s", graph.calls[0].cypher)
	}
	if !strings.Contains(graph.calls[1].cypher, "(s:SecurityGroupRule)-[r]->()") {
		t.Fatalf("second scan must enumerate :SecurityGroupRule sources:\n%s", graph.calls[1].cypher)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (merged from both source-label scans)", len(rows))
	}
	uids := map[string]bool{}
	for _, row := range rows {
		uids[row.SourceUID] = true
	}
	if !uids["sg-uid-a"] || !uids["rule-uid-a"] {
		t.Fatalf("rows = %+v, want both sg-uid-a (CloudResource scan) and rule-uid-a (SecurityGroupRule scan)", rows)
	}
}

// TestProjectedSourceEdgeBackfillReaderNilGraphErrors proves a nil Graph
// returns an error rather than panicking, mirroring the other reducer graph
// readers' nil-dependency guard.
func TestProjectedSourceEdgeBackfillReaderNilGraphErrors(t *testing.T) {
	t.Parallel()

	reader := ProjectedSourceEdgeBackfillReader{}
	if _, err := reader.EnumerateProjectedSourceEdges(context.Background(), []string{"evidence-1"}); err == nil {
		t.Fatal("expected an error for a nil Graph")
	}
}
