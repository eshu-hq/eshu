// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func resourceOnlyResponse(t *testing.T, mimeType, payload string) string {
	t.Helper()
	response, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"isError": false,
			"content": []any{
				map[string]any{"type": "text", "text": "Returned 25 result(s)."},
				map[string]any{"type": "resource", "resource": map[string]any{
					"uri": "eshu://tool-result/envelope", "mimeType": mimeType, "text": payload,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(response)
}

func TestMCPCallToolResourceOnlyPreservesFullResult(t *testing.T) {
	t.Parallel()

	findings := make([]map[string]any, 25)
	for i := range findings {
		findings[i] = map[string]any{"line": i + 1, "redacted_excerpt": strings.Repeat("x", 5600)}
	}
	data, err := json.Marshal(map[string]any{"findings": findings})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(map[string]any{
		"data": json.RawMessage(data), "truth": map[string]any{"level": "derived"}, "error": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := resourceOnlyResponse(t, "application/eshu.envelope+json", string(envelope))
	var rpc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(response), &rpc); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(rpc["result"], &result); err != nil {
		t.Fatal(err)
	}
	resourceBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	result["structuredContent"] = json.RawMessage(envelope)
	dualBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(resourceBytes) >= 256<<10 || len(dualBytes) <= 256<<10 {
		t.Fatalf("test premise: resource-only=%d dual-copy=%d", len(resourceBytes), len(dualBytes))
	}

	doer := &fakeMCPDoer{byTool: map[string]string{"investigate_hardcoded_secrets": response}}
	client := mcpClientWithDoer(doer)
	whole, err := client.callTool(context.Background(), "investigate_hardcoded_secrets", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(whole, envelope) {
		t.Fatalf("resource-only canonical envelope was not preserved: got %d bytes, want %d", len(whole), len(envelope))
	}
	unwrapped, err := client.callTool(context.Background(), "investigate_hardcoded_secrets", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unwrapped, data) {
		t.Fatalf("resource-only data was not unwrapped: got %d bytes, want %d", len(unwrapped), len(data))
	}
	snapshot := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{
		"investigate_hardcoded_secrets": {
			Envelope:               true,
			RequiredResponseFields: []string{"data", "truth", "error"},
			RequiredJSONPaths:      []string{"data.findings[].redacted_excerpt"},
		},
	}}}
	var report Report
	if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snapshot, &report); err != nil {
		t.Fatal(err)
	}
	if finding, ok := findingByCheck(report, "mcp:investigate_hardcoded_secrets"); !ok || !finding.OK {
		t.Fatalf("golden MCP shape did not accept the full resource-only envelope: %+v", finding)
	}
}

func TestMCPCallToolResourceOnlyPlainJSON(t *testing.T) {
	t.Parallel()
	payload := `{"count":1,"results":[{"id":"r1"}]}`
	doer := &fakeMCPDoer{byTool: map[string]string{
		"list_indexed_repositories": resourceOnlyResponse(t, "application/json", payload),
	}}
	got, err := mcpClientWithDoer(doer).callTool(context.Background(), "list_indexed_repositories", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("plain JSON resource = %s, want %s", got, payload)
	}
}

func TestMCPCallToolPrefersStructuredContentToResource(t *testing.T) {
	t.Parallel()
	var rpc map[string]any
	if err := json.Unmarshal([]byte(resourceOnlyResponse(t, "application/json", `{"source":"resource"}`)), &rpc); err != nil {
		t.Fatal(err)
	}
	result := rpc["result"].(map[string]any)
	result["structuredContent"] = map[string]any{"source": "structured"}
	response, err := json.Marshal(rpc)
	if err != nil {
		t.Fatal(err)
	}
	doer := &fakeMCPDoer{byTool: map[string]string{"get_repo_summary": string(response)}}
	got, err := mcpClientWithDoer(doer).callTool(context.Background(), "get_repo_summary", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"source":"structured"}` {
		t.Fatalf("structuredContent lost precedence: %s", got)
	}
}

func TestMCPCallToolRejectsMalformedJSONResource(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"invalid_json":        `{bad`,
		"missing_truth":       `{"data":{},"error":null}`,
		"missing_error_field": `{"data":{},"truth":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doer := &fakeMCPDoer{byTool: map[string]string{
				"investigate_hardcoded_secrets": resourceOnlyResponse(t, "application/eshu.envelope+json", payload),
			}}
			got, err := mcpClientWithDoer(doer).callTool(context.Background(), "investigate_hardcoded_secrets", nil, true)
			if err == nil {
				t.Fatalf("malformed resource returned success: %q", got)
			}
		})
	}
}
