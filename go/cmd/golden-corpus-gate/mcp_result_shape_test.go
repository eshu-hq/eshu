// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// structuredShapeResponse renders the one-copy result shape (#8800): one text
// summary and structuredContent, with no embedded resource.
func structuredShapeResponse(t *testing.T, payload string) string {
	t.Helper()
	response, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": "Returned 1 result(s)."}},
			"structuredContent": json.RawMessage(payload),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(response)
}

// TestMCPCallToolReadsTheStructuredShape proves the structured-first reader
// path: with no resource block to fall back on, callTool returns the
// structuredContent payload. The text fallback has its own test below.
func TestMCPCallToolReadsTheStructuredShape(t *testing.T) {
	t.Parallel()
	payload := `{"data":{"count":1},"truth":{"level":"exact"},"error":null}`
	doer := &fakeMCPDoer{byTool: map[string]string{"list_indexed_repositories": structuredShapeResponse(t, payload)}}
	got, err := mcpClientWithDoer(doer).callTool(context.Background(), "list_indexed_repositories", nil, true)
	if err != nil {
		t.Fatalf("callTool error = %v", err)
	}
	var want, have any
	if json.Unmarshal([]byte(payload), &want) != nil || json.Unmarshal(got, &have) != nil || !jsonEqual(want, have) {
		t.Fatalf("callTool = %s, want %s", got, payload)
	}
}

// TestMCPCallToolReadsTextWhenNoStructuredContentOrResource proves the last
// fallback: a result that carries neither structuredContent nor a resource
// block still yields the JSON payload held in its text block, rather than
// passing as empty or failing.
func TestMCPCallToolReadsTextWhenNoStructuredContentOrResource(t *testing.T) {
	t.Parallel()
	payload := `{"data":{"count":1},"truth":{"level":"exact"},"error":null}`
	response, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": payload}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	doer := &fakeMCPDoer{byTool: map[string]string{"list_indexed_repositories": string(response)}}
	got, err := mcpClientWithDoer(doer).callTool(context.Background(), "list_indexed_repositories", nil, true)
	if err != nil {
		t.Fatalf("callTool error = %v", err)
	}
	var want, have any
	if json.Unmarshal([]byte(payload), &want) != nil || json.Unmarshal(got, &have) != nil || !jsonEqual(want, have) {
		t.Fatalf("callTool = %s, want %s", got, payload)
	}
}

func jsonEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// TestMCPClientAsksForTheConfiguredShape proves the gate can run against the
// structured shape without a second server: the shape rides ?shape= on the
// JSON-RPC endpoint, and an unset shape leaves the URL bare.
func TestMCPClientAsksForTheConfiguredShape(t *testing.T) {
	t.Parallel()
	for shape, want := range map[string]string{
		"":           "http://mcp.local/mcp/message",
		"dual":       "http://mcp.local/mcp/message?shape=dual",
		"structured": "http://mcp.local/mcp/message?shape=structured",
	} {
		doer := &fakeMCPDoer{byTool: map[string]string{"get_repo_summary": structuredShapeResponse(t, `{"ok":true}`)}}
		client := mcpClientWithDoer(doer)
		client.shape = shape
		if _, err := client.callTool(context.Background(), "get_repo_summary", nil, false); err != nil {
			t.Fatalf("shape %q: %v", shape, err)
		}
		if doer.lastURL != want {
			t.Fatalf("shape %q posted to %q, want %q", shape, doer.lastURL, want)
		}
	}
}

func TestParseFlagsMCPResultShape(t *testing.T) {
	t.Parallel()
	for value, ok := range map[string]bool{"": true, "dual": true, "structured": true, "text": false} {
		o, err := parseFlags([]string{"-mcp-result-shape=" + value})
		if (err == nil) != ok {
			t.Fatalf("-mcp-result-shape=%q error = %v, want ok=%v", value, err, ok)
		}
		if ok && o.mcpResultShape != value {
			t.Fatalf("mcpResultShape = %q, want %q", o.mcpResultShape, value)
		}
		if !ok && !strings.Contains(err.Error(), "dual") {
			t.Fatalf("error %q does not name the allowed shapes", err)
		}
	}
}

// TestParseFlagsMCPResultShapeTrimsPadding proves a padded value (a quoted
// env var forwarded by the shell driver) matches the server, which trims
// before parsing: the gate accepts it, stores the canonical lower-case value,
// and sends it to the server without the padding or the original case.
// Whitespace only means absent, and a genuinely unknown value is still
// rejected.
func TestParseFlagsMCPResultShapeTrimsPadding(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]string{
		" structured ": "structured",
		"\tdual\n":     "dual",
		"Structured":   "structured",
		" DUAL ":       "dual",
		"  ":           "",
	} {
		o, err := parseFlags([]string{"-mcp-result-shape=" + value})
		if err != nil {
			t.Fatalf("-mcp-result-shape=%q: %v, want accepted like the server", value, err)
		}
		if o.mcpResultShape != want {
			t.Fatalf("mcpResultShape = %q, want trimmed %q", o.mcpResultShape, want)
		}
		doer := &fakeMCPDoer{byTool: map[string]string{"get_repo_summary": structuredShapeResponse(t, `{"ok":true}`)}}
		client := mcpClientWithDoer(doer)
		client.shape = o.mcpResultShape
		if _, err := client.callTool(context.Background(), "get_repo_summary", nil, false); err != nil {
			t.Fatalf("shape %q: %v", value, err)
		}
		wantURL := "http://mcp.local/mcp/message"
		if want != "" {
			wantURL += "?shape=" + want
		}
		if doer.lastURL != wantURL {
			t.Fatalf("shape %q posted to %q, want %q", value, doer.lastURL, wantURL)
		}
	}
	if _, err := parseFlags([]string{"-mcp-result-shape= text "}); err == nil {
		t.Fatal("padded unknown shape accepted, want rejection")
	}
}

func TestCheckMCPQueryStructuredErrorDiagnostic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, payload, summary, expected string
		pass                             bool
	}{
		{"infra refusal", `{"data":null,"truth":null,"error":{"code":"mcp_dispatch_error","message":"HTTP 404: entity not found"}}`, "MCP dispatch failed; details in structuredContent.error", "entity not found", true},
		{"divergence refusal", `{"data":null,"truth":null,"error":{"code":"mcp_dispatch_error","message":"HTTP 404: divergence finding not found"}}`, "MCP dispatch failed; details in structuredContent.error", "divergence finding not found", true},
		{"unexpected refusal", `{"data":null,"truth":null,"error":{"code":"mcp_dispatch_error","message":"HTTP 500: backend unavailable"}}`, "entity not found", "entity not found", false},
		{"malformed structured error", `{"error":{"message":7}}`, "entity not found", "entity not found", false},
		{"malformed diagnostic is not a refusal", `{"error":{"message":7}}`, "bounded summary", "invalid structured error diagnostic", false},
		{"truth array", `{"data":null,"truth":[],"error":{"code":"mcp_dispatch_error","message":"entity not found"}}`, "bounded summary", "entity not found", false},
		{"truth string", `{"data":null,"truth":"exact","error":{"code":"mcp_dispatch_error","message":"entity not found"}}`, "bounded summary", "entity not found", false},
		{"missing data", `{"truth":null,"error":{"code":"mcp_dispatch_error","message":"entity not found"}}`, "bounded summary", "entity not found", false},
		{"missing truth", `{"data":null,"error":{"code":"mcp_dispatch_error","message":"entity not found"}}`, "bounded summary", "entity not found", false},
		{"missing code", `{"data":null,"truth":null,"error":{"message":"entity not found"}}`, "bounded summary", "entity not found", false},
		{"empty code", `{"data":null,"truth":null,"error":{"code":"","message":"entity not found"}}`, "bounded summary", "entity not found", false},
		{"null structured error", `null`, "entity not found", "entity not found", false},
		{"error text in data", `{"data":{"message":"entity not found"},"truth":null,"error":null}`, "bounded summary", "entity not found", false},
		{"unrequested error", `{"data":null,"truth":null,"error":{"message":"entity not found"}}`, "bounded summary", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": tc.summary}}, "structuredContent": json.RawMessage(tc.payload)}})
			if err != nil {
				t.Fatal(err)
			}
			doer := &fakeMCPDoer{byTool: map[string]string{"fixture": string(wire)}}
			snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{"fixture": {ExpectedErrorContains: tc.expected}}}}
			var report Report
			if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &report); err != nil {
				t.Fatal(err)
			}
			finding, ok := findingByCheck(report, "mcp:fixture")
			if !ok || finding.OK != tc.pass || !finding.Required {
				t.Fatalf("finding=%+v, present=%v, want pass=%v required", finding, ok, tc.pass)
			}
		})
	}
}

func TestCheckMCPQueryTransportErrorsCannotMatchExpectedRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, wire, expected string
		status               int
	}{
		{"HTTP", `{}`, "HTTP 500", 500},
		{"RPC", `{"error":{"code":-32603,"message":"backend unavailable"}}`, "backend unavailable", 200},
		{"invalid JSON", `{`, "decode tools/call", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doer := &fakeMCPDoer{status: tc.status, byTool: map[string]string{"fixture": tc.wire}}
			snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{"fixture": {ExpectedErrorContains: tc.expected}}}}
			var report Report
			if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &report); err != nil {
				t.Fatal(err)
			}
			finding, ok := findingByCheck(report, "mcp:fixture")
			if !ok || finding.OK || !finding.Required {
				t.Fatalf("transport/protocol error matched tool refusal: %+v", finding)
			}
		})
	}
}

func TestCheckMCPQueryErrorResourceBeforeText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, payload, summary string
		pass                   bool
	}{
		{"canonical resource", `{"data":null,"truth":null,"error":{"code":"not_found","message":"entity not found"}}`, "bounded summary", true},
		{"malformed resource", `{`, "entity not found", false},
		{"noncanonical resource", `{"error":{"code":"not_found","message":"entity not found"}}`, "entity not found", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": tc.summary}, map[string]any{"type": "resource", "resource": map[string]string{"mimeType": "application/eshu.envelope+json", "text": tc.payload}}}}})
			if err != nil {
				t.Fatal(err)
			}
			doer := &fakeMCPDoer{byTool: map[string]string{"fixture": string(wire)}}
			snap := Snapshot{QueryShapes: QueryShapes{MCP: map[string]QueryShape{"fixture": {ExpectedErrorContains: "entity not found"}}}}
			var report Report
			if err := checkMCPQuery(context.Background(), mcpClientWithDoer(doer), snap, &report); err != nil {
				t.Fatal(err)
			}
			finding, ok := findingByCheck(report, "mcp:fixture")
			if !ok || finding.OK != tc.pass || !finding.Required {
				t.Fatalf("resource finding=%+v, want pass=%v", finding, tc.pass)
			}
		})
	}
}
