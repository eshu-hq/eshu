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
)

// fakeMCPDoer returns a canned response per tool name (keyed by the tool name in
// the request body) and records the last request body so tests can assert the
// arguments were forwarded.
type fakeMCPDoer struct {
	byTool   map[string]string // tool name -> raw JSON-RPC response body
	status   int
	lastBody string
}

func (f *fakeMCPDoer) Do(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	f.lastBody = string(body)
	var parsed mcpJSONRPCRequest
	_ = json.Unmarshal(body, &parsed)
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	respBody := f.byTool[parsed.Params.Name]
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     make(http.Header),
	}, nil
}

func mcpClientWithDoer(d httpDoer) *mcpClient {
	return &mcpClient{baseURL: "http://mcp.local", doer: d}
}

func findingByCheck(r Report, check string) (Finding, bool) {
	for _, f := range r.Findings {
		if f.Check == check {
			return f, true
		}
	}
	return Finding{}, false
}

// TestCheckMCPQueryStructuredContentPasses proves the happy path: a tool whose
// structuredContent satisfies the shape produces a passing required finding, and
// the shape's arguments are forwarded in the tools/call request.
func TestCheckMCPQueryStructuredContentPasses(t *testing.T) {
	t.Parallel()

	doer := &fakeMCPDoer{byTool: map[string]string{
		"get_repo_summary": `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{}"}],"structuredContent":{"repository":"orders-api","file_count":12}}}`,
	}}
	snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{
		"get_repo_summary": {
			RequiredResponseFields: []string{"repository", "file_count"},
			Arguments:              map[string]any{"repo_name": "orders-api"},
		},
	}}}
	var r Report
	if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &r); err != nil {
		t.Fatalf("checkMCPQuery err = %v", err)
	}
	f, ok := findingByCheck(r, "mcp:get_repo_summary")
	if !ok {
		t.Fatal("missing mcp:get_repo_summary finding")
	}
	if !f.OK || !f.Required {
		t.Fatalf("structured-content shape must pass as required: %+v", f)
	}
	if !strings.Contains(doer.lastBody, `"repo_name":"orders-api"`) {
		t.Errorf("tools/call did not forward arguments: %s", doer.lastBody)
	}
	if !strings.Contains(doer.lastBody, `"method":"tools/call"`) {
		t.Errorf("request was not a tools/call: %s", doer.lastBody)
	}
}

func TestCheckMCPQueryCanAssertTruthEnvelope(t *testing.T) {
	t.Parallel()

	doer := &fakeMCPDoer{byTool: map[string]string{
		"find_cross_repo_dead_code": `{"jsonrpc":"2.0","id":1,"result":{"content":[],"structuredContent":{"data":{"query_shape":"bounded_cross_repo_dead_code","candidate_buckets":{"live_by_consumer":[{"consumer_evidence":[{"citation":"code_reachability_rows:scope/gen/consumer/root/entity"}]}]}},"truth":{"level":"derived","basis":"hybrid"},"error":null}}}`,
	}}
	snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{
		"find_cross_repo_dead_code": {
			Envelope:               true,
			RequiredResponseFields: []string{"data", "truth", "error"},
			RequiredJSONPaths: []string{
				"data.candidate_buckets.live_by_consumer[].consumer_evidence[].citation",
			},
			RequiredJSONValues: map[string]any{
				"truth.level":      "derived",
				"truth.basis":      "hybrid",
				"data.query_shape": "bounded_cross_repo_dead_code",
			},
			Arguments: map[string]any{"repo_id": "deadcode-producer", "language": "go", "limit": float64(20)},
		},
	}}}
	var r Report
	if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &r); err != nil {
		t.Fatalf("checkMCPQuery err = %v", err)
	}
	f, ok := findingByCheck(r, "mcp:find_cross_repo_dead_code")
	if !ok {
		t.Fatal("missing mcp:find_cross_repo_dead_code finding")
	}
	if !f.OK || !f.Required {
		t.Fatalf("enveloped dead-code shape must pass as required: %+v", f)
	}
	if !strings.Contains(doer.lastBody, `"repo_id":"deadcode-producer"`) {
		t.Errorf("tools/call did not forward dead-code arguments: %s", doer.lastBody)
	}
}

// TestCheckMCPQueryTextContentFallback proves the text-content fallback when a
// tool returns no structuredContent.
func TestCheckMCPQueryTextContentFallback(t *testing.T) {
	t.Parallel()

	doer := &fakeMCPDoer{byTool: map[string]string{
		"list_indexed_repositories": `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"repositories\":[{\"id\":\"r1\",\"name\":\"a\"}]}"}]}}`,
	}}
	snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{
		"list_indexed_repositories": {
			RequiredResponseFields:   []string{"repositories"},
			MinimumResults:           1,
			ResultItemRequiredFields: []string{"id", "name"},
		},
	}}}
	var r Report
	if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &r); err != nil {
		t.Fatalf("checkMCPQuery err = %v", err)
	}
	if f, _ := findingByCheck(r, "mcp:list_indexed_repositories"); !f.OK {
		t.Fatalf("text-content fallback shape must pass: %+v", f)
	}
}

// TestCheckMCPQueryToolErrorsAreRequiredFailures proves transport/JSON-RPC/tool
// errors each surface as a failing required finding (the gate blocks).
func TestCheckMCPQueryToolErrorsAreRequiredFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"is_error":  `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"boom"}]}}`,
		"rpc_error": `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`,
		"no_result": `{"jsonrpc":"2.0","id":1}`,
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doer := &fakeMCPDoer{byTool: map[string]string{"list_cloud_resource_inventory": resp}}
			snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{
				"list_cloud_resource_inventory": {RequiredResponseFields: []string{"resources"}, MinimumResults: 1},
			}}}
			var r Report
			if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &r); err != nil {
				t.Fatalf("checkMCPQuery err = %v", err)
			}
			f, ok := findingByCheck(r, "mcp:list_cloud_resource_inventory")
			if !ok {
				t.Fatal("missing finding")
			}
			if f.OK || !f.Required {
				t.Fatalf("tool error must be a failing required finding: %+v", f)
			}
		})
	}
}

// TestCheckMCPQueryHTTPErrorFails proves a non-2xx HTTP status fails the finding.
func TestCheckMCPQueryHTTPErrorFails(t *testing.T) {
	t.Parallel()

	doer := &fakeMCPDoer{status: http.StatusInternalServerError, byTool: map[string]string{
		"list_kubernetes_correlations": `{}`,
	}}
	snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{
		"list_kubernetes_correlations": {RequiredResponseFields: []string{"correlations"}, MinimumResults: 1},
	}}}
	var r Report
	if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &r); err != nil {
		t.Fatalf("checkMCPQuery err = %v", err)
	}
	if f, _ := findingByCheck(r, "mcp:list_kubernetes_correlations"); f.OK {
		t.Fatalf("HTTP 500 must fail the finding: %+v", f)
	}
}
