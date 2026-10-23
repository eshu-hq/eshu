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
// structuredContent payload, and a result with neither structuredContent nor a
// resource still reads its text rather than passing as empty.
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
