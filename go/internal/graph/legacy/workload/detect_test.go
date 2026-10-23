// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fakeGraph answers the detection reads from in-memory rows and records every
// statement so tests can assert on the bound parameters.
type fakeGraph struct {
	candidates []map[string]any // id, name, repo_id
	edgeInst   []string
	propInst   []string
	twins      []map[string]any // id, repo_id
	rels       []map[string]any
	calls      []string
	failOn     string
}

func (g *fakeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.calls = append(g.calls, cypher)
	for _, key := range []string{"ids", "twin_ids"} {
		if v, ok := params[key]; ok {
			if list, isList := v.([]string); !isList || list == nil {
				return nil, fmt.Errorf("param %s must be a non-nil []string, got %T", key, v)
			}
		}
	}
	if g.failOn != "" && strings.Contains(cypher, g.failOn) {
		return nil, errors.New("boom")
	}
	switch cypher {
	case candidatePageCypher:
		after, _ := params["after"].(string)
		limit, _ := params["limit"].(int)
		var out []map[string]any
		for _, row := range g.candidates {
			if row["id"].(string) > after && len(out) < limit {
				out = append(out, row)
			}
		}
		return out, nil
	case candidatesByIDsCypher:
		ids := params["ids"].([]string)
		var out []map[string]any
		for _, row := range g.candidates {
			if slices.Contains(ids, row["id"].(string)) {
				out = append(out, row)
			}
		}
		return out, nil
	case instanceEdgeCypher:
		return idRows(g.edgeInst, params["ids"].([]string)), nil
	case instancePropertyCypher:
		return idRows(g.propInst, params["ids"].([]string)), nil
	case twinCypher:
		var out []map[string]any
		for _, row := range g.twins {
			if slices.Contains(params["twin_ids"].([]string), row["id"].(string)) {
				out = append(out, row)
			}
		}
		return out, nil
	case relationshipsCypher:
		var out []map[string]any
		for _, row := range g.rels {
			if slices.Contains(params["ids"].([]string), row["id"].(string)) {
				out = append(out, row)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected statement %q", cypher)
}

func idRows(have, asked []string) []map[string]any {
	var out []map[string]any
	for _, id := range have {
		if slices.Contains(asked, id) {
			out = append(out, map[string]any{"id": id})
		}
	}
	return out
}

func candRow(i int) map[string]any {
	name := fmt.Sprintf("svc%03d", i)
	return map[string]any{"id": "workload:" + name, "name": name, "repo_id": fmt.Sprintf("repository:r%03d", i)}
}

func twinRow(i int) map[string]any {
	name := fmt.Sprintf("svc%03d", i)
	repo := fmt.Sprintf("repository:r%03d", i)
	return map[string]any{"id": TwinID(repo, name), "repo_id": repo}
}

func definesRow(i int) map[string]any {
	c := candRow(i)
	return map[string]any{"id": c["id"], "type": "DEFINES", "outgoing": false, "other_id": c["repo_id"], "other_labels": []any{"Repository"}}
}

func TestDetectPagesTheWholeLabelAndEvaluatesEveryPage(t *testing.T) {
	g := &fakeGraph{}
	for i := 0; i < 25; i++ {
		g.candidates = append(g.candidates, candRow(i))
		g.twins = append(g.twins, twinRow(i))
		g.rels = append(g.rels, definesRow(i))
	}
	g.edgeInst = []string{"workload:svc003"}
	g.propInst = []string{"workload:svc004"}

	report, err := Detect(context.Background(), g, Options{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Verdicts) != 25 || report.Truncated {
		t.Fatalf("verdicts=%d truncated=%v, want 25 and not truncated", len(report.Verdicts), report.Truncated)
	}
	if got := report.Eligible(); got != 23 {
		t.Fatalf("eligible = %d, want 23 (two blocked by instances)", got)
	}
	if got := report.WithScopedTwin(); got != 23 {
		t.Fatalf("WithScopedTwin = %d, want 23", got)
	}
}

func TestDetectTruncatesAtTheExaminedCap(t *testing.T) {
	g := &fakeGraph{}
	for i := 0; i < 25; i++ {
		g.candidates = append(g.candidates, candRow(i))
	}
	report, err := Detect(context.Background(), g, Options{PageSize: 10, MaxExamined: 15})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Verdicts) != 15 || !report.Truncated {
		t.Fatalf("verdicts=%d truncated=%v, want 15 and truncated", len(report.Verdicts), report.Truncated)
	}
}

func TestDetectStopsAtMaxEligibleButKeepsBlockedOnes(t *testing.T) {
	g := &fakeGraph{}
	for i := 0; i < 30; i++ {
		g.candidates = append(g.candidates, candRow(i))
		if i >= 12 { // the first twelve have no twin and block ahead of the eligible ones
			g.twins = append(g.twins, twinRow(i))
			g.rels = append(g.rels, definesRow(i))
		}
	}
	report, err := Detect(context.Background(), g, Options{PageSize: 10, MaxEligible: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Eligible(); got < 5 {
		t.Fatalf("eligible = %d, want at least 5 past the twelve blocked head nodes", got)
	}
	if !report.Truncated {
		t.Fatal("more candidates remain: want truncated")
	}
}

func TestDetectByIDsOnlyEvaluatesNamedNodesAndIgnoresShapeMismatch(t *testing.T) {
	g := &fakeGraph{}
	for i := 0; i < 5; i++ {
		g.candidates = append(g.candidates, candRow(i))
		g.twins = append(g.twins, twinRow(i))
		g.rels = append(g.rels, definesRow(i))
	}
	// A row whose id is not the exact handle shape must never become a candidate.
	g.candidates = append(g.candidates, map[string]any{"id": "workload:repository:r:svc", "name": "svc", "repo_id": "repository:r"})
	report, err := Detect(context.Background(), g, Options{IDs: []string{"workload:svc002", "workload:svc002", "workload:repository:r:svc"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Verdicts) != 1 || report.Verdicts[0].ID != "workload:svc002" || !report.Verdicts[0].Eligible {
		t.Fatalf("verdicts = %+v, want exactly the eligible svc002", report.Verdicts)
	}
}

func TestDetectBindsNonNilListsEvenWhenNothingMatches(t *testing.T) {
	g := &fakeGraph{}
	g.candidates = append(g.candidates, map[string]any{"id": "workload:x", "name": "x", "repo_id": ""})
	// fakeGraph rejects nil/null parameters, so a clean run proves every list
	// is bound non-nil even for a candidate with no twin id.
	report, err := Detect(context.Background(), g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Verdicts) != 1 || report.Verdicts[0].BlockedReason != BlockedNoTwin {
		t.Fatalf("verdicts = %+v, want one no_twin", report.Verdicts)
	}
	for _, call := range g.calls {
		if call == twinCypher {
			t.Fatal("twin read must be skipped when no candidate can have a twin id")
		}
	}
}

func TestDetectEmptyGraphIsCleanAndErrorsPropagate(t *testing.T) {
	report, err := Detect(context.Background(), &fakeGraph{}, Options{})
	if err != nil || len(report.Verdicts) != 0 || report.Truncated {
		t.Fatalf("clean graph: report=%+v err=%v", report, err)
	}
	g := &fakeGraph{candidates: []map[string]any{candRow(1)}, failOn: "INSTANCE_OF"}
	if _, err := Detect(context.Background(), g, Options{}); err == nil {
		t.Fatal("want the instance-edge read failure to propagate")
	}
	if _, err := Detect(context.Background(), nil, Options{}); err == nil {
		t.Fatal("want an error for a nil reader")
	}
}
