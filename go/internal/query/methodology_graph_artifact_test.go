// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestMethodologyEmptyGrantHTTPDoesNotReadGraph(t *testing.T) {
	t.Parallel()
	graph := &methodologyRejectGraph{}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/v0/code/imports/investigate", strings.NewReader(`{"query_type":"imports_by_file","source_file":"src/proof.py"}`))
	request.Header.Set("Accept", EnvelopeMIMEType)
	request = request.WithContext(ContextWithAuthContext(request.Context(), testutil.CodeGrantScopedAuthContext(nil)))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || graph.calls != 0 {
		t.Fatalf("empty-grant HTTP status=%d graph calls=%d body=%s", response.Code, graph.calls, response.Body.String())
	}
	var envelope struct {
		Data struct {
			SourceBackend string `json:"source_backend"`
			Count         int    `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.SourceBackend != "no_backend_read" || envelope.Data.Count != 0 {
		t.Fatalf("empty grant truth=%+v", envelope.Data)
	}
}

func TestMethodologyRequestQueryCountBudget(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 3} {
		if err := methodologyRequestQueryCountWithinBudget(count, 3); err != nil {
			t.Fatalf("count %d within budget: %v", count, err)
		}
	}
	if err := methodologyRequestQueryCountWithinBudget(4, 3); err == nil {
		t.Fatal("a fourth graph query must exceed the request budget")
	}
}

type methodologyRejectGraph struct{ calls int }

func (g *methodologyRejectGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	g.calls++
	return nil, errors.New("empty grant reached graph")
}

func (g *methodologyRejectGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	g.calls++
	return nil, errors.New("empty grant reached graph")
}

func TestMethodologyGraphStatementOracle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		entry  string
		params map[string]any
		want   []string
	}{
		{
			name:   "duplicate and null import attributes stay distinct",
			entry:  "QP-CODE-IMPORT-ROWS-REPOSITORY",
			params: map[string]any{"repo_id": "proof-repository", "limit": 11, "offset": 0},
			want: []string{
				"proof-repository|src/proof.py|proof.target|1",
				"proof-repository|src/proof.py|proof.target|3",
				"proof-repository|src/proof.py|proof.target|4",
				"proof-repository|src/target.py|proof.source|2",
			},
		},
		{
			name:   "grant precedes limit",
			entry:  "QP-CODE-IMPORT-ROWS-REPOSITORY",
			params: map[string]any{"source_file": "src/proof.py", "allowed_repository_ids": []string{"proof-repository"}, "allowed_scope_ids": []string{"proof-scope"}, "limit": 2, "offset": 0},
			want:   []string{"proof-repository|src/proof.py|proof.target|1", "proof-repository|src/proof.py|proof.target|3"},
		},
		{
			name:   "module membership is its own intermediate result",
			entry:  "QP-CODE-IMPORT-SOURCE-MODULE-FILES",
			params: map[string]any{"source_module": "proof.source", "repo_id": "proof-repository", "scan_limit": 25001},
			want:   []string{"proof-repository|/proof/src/proof.py|proof.source"},
		},
		{
			name:   "two endpoint grants on a call",
			entry:  "QP-CODE-IMPORT-CROSS-MODULE-CALLS",
			params: map[string]any{"source_file": "src/proof.py", "allowed_repository_ids": []string{"proof-repository"}, "allowed_scope_ids": []string{"proof-scope"}, "scan_limit": 25001},
			want:   []string{"proof-repository|/proof/src/proof.py|proof-repository|/proof/src/target.py|fn-proof|fn-target"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := methodologyOracleStatementIDs(tc.entry, tc.params)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("oracle IDs=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestMethodologyCycleEdgeOracleKeepsDirectionalCandidates(t *testing.T) {
	t.Parallel()
	ids := methodologyOracleStatementIDs("QP-CODE-IMPORT-CYCLE-EDGES", map[string]any{
		"source_file": "src/proof.py", "target_module": "proof.target", "scan_limit": 25001,
	})
	if len(ids) != 134 {
		t.Fatalf("cycle edge ledger=%d, want all 134 before directional reconstruction", len(ids))
	}
	if !slices.Contains(ids, "proof-repository|src/target.py|proof.source|2") || !slices.Contains(ids, "other-repository|src/skew-128.py|proof.target|128") {
		t.Fatalf("cycle edge ledger lost directional candidates: %v", ids)
	}
}

func TestMethodologyGraphOracleEmptyPagesOnPopulatedFixture(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		params map[string]any
	}{
		{"missing file", map[string]any{"source_file": "src/missing.py", "allowed_repository_ids": []string{"proof-repository"}, "allowed_scope_ids": []string{"proof-scope"}, "limit": 11, "offset": 0}},
		{"after final offset", map[string]any{"repo_id": "proof-repository", "limit": 11, "offset": 10000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := methodologyOracleStatementIDs("QP-CODE-IMPORT-ROWS-REPOSITORY", tc.params); len(got) != 0 {
				t.Fatalf("expected empty physical page from populated fixture, got %v", got)
			}
		})
	}
}

func TestMethodologyGraphStatementCapturePreservesExtraColumn(t *testing.T) {
	t.Parallel()
	keys := []string{"repo_id", "source_file", "target_module", "line_number"}
	values := []any{"proof", "source", "target", int64(1)}
	narrow := methodologyRecordRows([]*neo4j.Record{{Keys: keys, Values: values}})
	wide := methodologyRecordRows([]*neo4j.Record{{Keys: append(slices.Clone(keys), "extra"), Values: append(slices.Clone(values), "more data")}})
	ids := methodologyStatementIDs("QP-CODE-IMPORT-ROWS-REPOSITORY", narrow)
	if !slices.Equal(ids, []string{"proof|source|target|1"}) || !slices.Equal(ids, methodologyStatementIDs("QP-CODE-IMPORT-ROWS-REPOSITORY", wide)) {
		t.Fatalf("extra column changed statement identities: %v", ids)
	}
	paired := methodologyPairedRun{
		ResultIDs: ids,
		StatementResults: []json.RawMessage{
			methodologyJSON(t, narrow), methodologyJSON(t, narrow),
			methodologyJSON(t, narrow), methodologyJSON(t, wide),
		},
	}
	run := methodologyGraphPilotRun(t, paired)
	if string(run.Result) != `["proof|source|target|1"]` || len(run.StatementResults[3]) <= len(run.StatementResults[0]) {
		t.Fatalf("full row payload lost while identity stayed fixed: result=%s rows=%s", run.Result, run.StatementResults[3])
	}
	if !strings.Contains(string(run.StatementResults[3]), `"extra":"more data"`) {
		t.Fatalf("extra statement column was omitted: %s", run.StatementResults[3])
	}
}
