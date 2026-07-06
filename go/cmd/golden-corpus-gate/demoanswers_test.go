// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/demospec"
)

// fakeDemoDoer answers both the MCP JSON-RPC endpoint (POST /mcp/message,
// keyed by tool name) and HTTP query routes (keyed by "METHOD request-uri"),
// so one doer drives both the queryClient and mcpClient the demo-answers phase
// uses. It records each call so a test can assert which surface was invoked.
type fakeDemoDoer struct {
	mcpByTool  map[string]string
	httpByPath map[string]string
	calls      []string
}

func (f *fakeDemoDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/mcp/message") {
		raw, _ := io.ReadAll(req.Body)
		var parsed mcpJSONRPCRequest
		_ = json.Unmarshal(raw, &parsed)
		f.calls = append(f.calls, "mcp:"+parsed.Params.Name)
		return demoResp(http.StatusOK, f.mcpByTool[parsed.Params.Name]), nil
	}
	key := req.Method + " " + req.URL.RequestURI()
	f.calls = append(f.calls, "http:"+key)
	body, ok := f.httpByPath[key]
	if !ok {
		return demoResp(http.StatusNotFound, `{}`), nil
	}
	return demoResp(http.StatusOK, body), nil
}

func demoResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func demoClients(d httpDoer) (*queryClient, *mcpClient) {
	return &queryClient{baseURL: "http://api.local", doer: d},
		&mcpClient{baseURL: "http://mcp.local", doer: d}
}

// demoQuestionFixtures returns one question of each executable shape: an mcp
// surface, an http surface, a playbook whose execute is mcp, and a playbook
// whose execute is http.
func demoQuestionFixtures() []demospec.Question {
	return []demospec.Question{
		{
			ID:      "q_mcp",
			Surface: demospec.Surface{Kind: demospec.SurfaceKindMCP, Ref: "list_kubernetes_correlations", Arguments: map[string]any{"cluster_id": "supply-chain-demo"}},
			ExpectedAnswer: demospec.ExpectedAnswer{
				RequiredResponseFields: []string{"correlations", "count"}, MinimumResults: 2,
			},
		},
		{
			ID:      "q_http",
			Surface: demospec.Surface{Kind: demospec.SurfaceKindHTTP, Ref: "GET /api/v0/observability/coverage/correlations?provider=tempo&limit=50"},
			ExpectedAnswer: demospec.ExpectedAnswer{
				RequiredResponseFields: []string{"correlations", "count"}, MinimumResults: 1,
			},
		},
		{
			ID: "q_pb_mcp",
			Surface: demospec.Surface{
				Kind: demospec.SurfaceKindPlaybook, Ref: "service_story_citation",
				Execute: &demospec.ExecuteTarget{Kind: demospec.SurfaceKindMCP, Ref: "get_service_story", Arguments: map[string]any{"workload_id": "workload:api-svc"}},
			},
			ExpectedAnswer: demospec.ExpectedAnswer{RequiredResponseFields: []string{"service_name"}, MinimumResults: 0},
		},
		{
			ID: "q_pb_http",
			Surface: demospec.Surface{
				Kind: demospec.SurfaceKindPlaybook, Ref: "incident_context_evidence_path",
				Execute: &demospec.ExecuteTarget{Kind: demospec.SurfaceKindHTTP, Ref: "GET /api/v0/incidents/PSCD1/context"},
			},
			ExpectedAnswer: demospec.ExpectedAnswer{RequiredResponseFields: []string{"incident", "evidence_path"}, MinimumResults: 1},
		},
	}
}

func populatedDemoDoer() *fakeDemoDoer {
	return &fakeDemoDoer{
		mcpByTool: map[string]string{
			"list_kubernetes_correlations": `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"correlations":[{},{}],"count":2}}}`,
			"get_service_story":            `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"service_name":"api-svc"}}}`,
		},
		httpByPath: map[string]string{
			"GET /api/v0/observability/coverage/correlations?provider=tempo&limit=50": `{"correlations":[{}],"count":1}`,
			"GET /api/v0/incidents/PSCD1/context":                                     `{"incident":{},"evidence_path":[{}]}`,
		},
	}
}

// TestAssertDemoAnswersPopulatedPasses proves every executable shape (mcp/http
// surface, playbook-via-execute-mcp, playbook-via-execute-http) produces a
// passing required "demo:<id>" finding when the live answer is populated.
func TestAssertDemoAnswersPopulatedPasses(t *testing.T) {
	t.Parallel()
	doer := populatedDemoDoer()
	qc, mc := demoClients(doer)
	var r Report
	assertDemoAnswers(context.Background(), qc, mc, demoQuestionFixtures(), &r)

	for _, id := range []string{"q_mcp", "q_http", "q_pb_mcp", "q_pb_http"} {
		f, ok := findingByCheck(r, "demo:"+id)
		if !ok {
			t.Fatalf("missing finding demo:%s", id)
		}
		if !f.OK || !f.Required {
			t.Errorf("demo:%s must pass as required: %+v", id, f)
		}
	}
	// The playbook question must call its execute target, not its playbook id.
	joined := strings.Join(doer.calls, ",")
	if !strings.Contains(joined, "mcp:get_service_story") {
		t.Errorf("playbook q_pb_mcp did not invoke its execute tool get_service_story: %v", doer.calls)
	}
	if strings.Contains(joined, "service_story_citation") {
		t.Errorf("playbook id service_story_citation must never be called directly: %v", doer.calls)
	}
}

// TestAssertDemoAnswersEmptyResultFails proves the value-level guard: an answer
// whose result array regresses below minimum_results is a failing required
// finding, so the gate turns red rather than passing on an empty demo answer.
func TestAssertDemoAnswersEmptyResultFails(t *testing.T) {
	t.Parallel()
	doer := populatedDemoDoer()
	doer.mcpByTool["list_kubernetes_correlations"] = `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"correlations":[],"count":0}}}`
	qc, mc := demoClients(doer)
	var r Report
	assertDemoAnswers(context.Background(), qc, mc, demoQuestionFixtures(), &r)

	f, ok := findingByCheck(r, "demo:q_mcp")
	if !ok {
		t.Fatal("missing finding demo:q_mcp")
	}
	if f.OK || !f.Required {
		t.Fatalf("empty result below minimum_results must be a failing required finding: %+v", f)
	}
	if !strings.Contains(f.Detail, "results, want >=") {
		t.Errorf("failure detail should name the result-count shortfall, got %q", f.Detail)
	}
}

// TestAssertDemoAnswersSurfaceErrorFails proves a transport/HTTP error on a
// question surfaces as a failing required finding rather than being swallowed.
func TestAssertDemoAnswersSurfaceErrorFails(t *testing.T) {
	t.Parallel()
	// httpByPath has no entry for the incident route -> 404 -> failing finding.
	doer := &fakeDemoDoer{
		mcpByTool: map[string]string{
			"list_kubernetes_correlations": `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"correlations":[{},{}],"count":2}}}`,
			"get_service_story":            `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"service_name":"api-svc"}}}`,
		},
		httpByPath: map[string]string{},
	}
	qc, mc := demoClients(doer)
	var r Report
	assertDemoAnswers(context.Background(), qc, mc, demoQuestionFixtures(), &r)

	f, ok := findingByCheck(r, "demo:q_pb_http")
	if !ok {
		t.Fatal("missing finding demo:q_pb_http")
	}
	if f.OK || !f.Required {
		t.Fatalf("HTTP 404 on a demo question must be a failing required finding: %+v", f)
	}
}
