// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestStructuredGenericDispatchErrorsCarryErrorEnvelope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		tool    string
		status  int
		body    string
		message string
	}{
		{"unknown tool", "missing_fixture_tool", http.StatusOK, `{}`, "unknown tool: missing_fixture_tool"},
		{"plain HTTP failure", "list_indexed_repositories", http.StatusInternalServerError, "boom", "HTTP 500: boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewServer(fixedBodyHandler(tc.status, tc.body), discardDispatchLogger())
			ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
			result, ok := s.callTool(ctx, mcpToolCallParams{Name: tc.tool, Arguments: map[string]any{}}, "").(mcpToolResult)
			if !ok {
				t.Fatal("callTool did not return an MCP tool result")
			}
			requireOneTextAndStructured(t, result)
			if !result.IsError {
				t.Fatal("dispatch failure lost isError")
			}
			var envelope query.ResponseEnvelope
			if err := json.Unmarshal(marshalForCompare(t, result.StructuredContent), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != "mcp_dispatch_error" || envelope.Error.Message != tc.message {
				t.Fatalf("error = %#v, want original dispatch diagnostic", envelope.Error)
			}
			if envelope.Error.Capability != "mcp.dispatch" || envelope.Data != nil || envelope.Truth != nil {
				t.Fatalf("envelope = %#v, want unavailable data/truth and dispatch capability", envelope)
			}
		})
	}
}

func TestStructuredGenericHTTPErrorRoutesIdentically(t *testing.T) {
	t.Parallel()
	s := newRouterTestServer(t, fixedBodyHandler(http.StatusInternalServerError, "boom"))
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	direct := toolCall(t, s, ctx, "list_indexed_repositories", map[string]any{})
	routed := toolCall(t, s, ctx, "call_tool", map[string]any{
		"name": "list_indexed_repositories", "arguments": map[string]any{},
	})
	if !bytes.Equal(direct, routed) {
		t.Fatalf("direct = %s, routed = %s", direct, routed)
	}
	var result mcpToolResult
	if err := json.Unmarshal(direct, &result); err != nil {
		t.Fatal(err)
	}
	requireOneTextAndStructured(t, result)
}

func TestDualGenericDispatchErrorsKeepGoldenBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		tool   string
		status int
		body   string
		want   string
	}{
		{"unknown", "missing_fixture_tool", 200, `{}`, `{"content":[{"type":"text","text":"unknown tool: missing_fixture_tool"}],"isError":true}`},
		{"HTTP failure", "list_indexed_repositories", 500, "boom", `{"content":[{"type":"text","text":"HTTP 500: boom"}],"isError":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewServer(fixedBodyHandler(tc.status, tc.body), discardDispatchLogger())
			result, ok := s.callTool(context.Background(), mcpToolCallParams{Name: tc.tool, Arguments: map[string]any{}}, "").(mcpToolResult)
			if !ok {
				t.Fatal("callTool did not return a tool result")
			}
			if got := string(marshalForCompare(t, result)); got != tc.want {
				t.Fatalf("dual wire = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestStructuredGenericErrorBudgetCountsFullWire(t *testing.T) {
	t.Parallel()
	const summary = "MCP dispatch failed; details in structuredContent.error"
	baseBody := strings.Repeat("\"\\\n", 12000)
	makeResult := func(body string) mcpToolResult {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: summary}},
			StructuredContent: &query.ResponseEnvelope{Error: &query.ErrorEnvelope{
				Code: "mcp_dispatch_error", Message: "HTTP 500: " + body, Capability: "mcp.dispatch",
			}},
			IsError: true,
		}
	}
	baseSize := len(marshalForCompare(t, makeResult(baseBody)))
	if baseSize >= defaultToolResponseByteBudget {
		t.Fatalf("base error is already %d bytes", baseSize)
	}
	fitBody := baseBody + strings.Repeat("x", defaultToolResponseByteBudget-baseSize)
	if got := len(marshalForCompare(t, makeResult(fitBody))); got != defaultToolResponseByteBudget {
		t.Fatalf("boundary fixture = %d bytes, want %d", got, defaultToolResponseByteBudget)
	}
	fit := callShapedTool(t, fixedBodyHandler(500, fitBody), resultshape.Structured)
	if got := len(marshalForCompare(t, fit)); got != defaultToolResponseByteBudget {
		t.Fatalf("at-budget wire = %d bytes, want full %d-byte error", got, defaultToolResponseByteBudget)
	}
	var fitEnvelope query.ResponseEnvelope
	if err := json.Unmarshal(marshalForCompare(t, fit.StructuredContent), &fitEnvelope); err != nil {
		t.Fatal(err)
	}
	if fitEnvelope.Error == nil || fitEnvelope.Error.Message != "HTTP 500: "+fitBody {
		t.Fatal("at-budget error lost original diagnostic")
	}
	if fit.Content[0].Text != summary || fit.Content[0].Resource != nil {
		t.Fatalf("at-budget summary = %#v, want one bounded pointer", fit.Content)
	}
	refused := callShapedTool(t, fixedBodyHandler(500, fitBody+"x"), resultshape.Structured)
	if !refused.IsError || len(marshalForCompare(t, refused)) > defaultToolResponseByteBudget {
		t.Fatalf("over-budget response was not bounded: %#v", refused)
	}
	var refusedEnvelope query.ResponseEnvelope
	if err := json.Unmarshal(marshalForCompare(t, refused.StructuredContent), &refusedEnvelope); err != nil {
		t.Fatal(err)
	}
	if refusedEnvelope.Error == nil || refusedEnvelope.Error.Code != errorCodeResponseOverBudget {
		t.Fatalf("over-budget error = %#v", refusedEnvelope.Error)
	}
	if got := refusedEnvelope.Error.Details["response_bytes"]; got != float64(defaultToolResponseByteBudget+1) {
		t.Fatalf("measured attempted wire bytes = %#v, want %d", got, defaultToolResponseByteBudget+1)
	}
}

func TestStructuredGenericErrorCountsOnceWithBoundedToolLabels(t *testing.T) {
	// Not parallel: this test installs a process-global meter provider.
	reader := callMetricsForTest(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	unknown := "missing_" + strings.Repeat("x", defaultToolResponseByteBudget)
	s := NewServer(fixedBodyHandler(200, `{}`), logger)
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	result, ok := s.callTool(ctx, mcpToolCallParams{Name: unknown, Arguments: map[string]any{}}, "").(mcpToolResult)
	if !ok || !result.IsError || len(marshalForCompare(t, result)) > defaultToolResponseByteBudget {
		t.Fatalf("unknown-tool result: ok=%v, isError=%v, wire=%d bytes", ok, result.IsError, len(marshalForCompare(t, result)))
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(marshalForCompare(t, result.StructuredContent), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != errorCodeResponseOverBudget || envelope.Error.Details["tool"] != unknownToolLabel {
		t.Fatalf("unknown-tool over-budget envelope = %#v", envelope.Error)
	}
	if strings.Contains(logs.String(), unknown) || logs.Len() > 4096 {
		t.Fatalf("unknown tool leaked into bounded log (%d bytes)", logs.Len())
	}
	if over, count, _ := collectBudgetMetrics(t, reader, unknownToolLabel); over != 1 || count != 1 {
		t.Fatalf("unknown-tool budget metrics = (over=%d,count=%d), want (1,1)", over, count)
	}
	requireToolCalls(t, reader, map[string]int64{"unknown/unknown_tool": 1})

	registered := NewServer(fixedBodyHandler(500, "boom"), logger)
	res, ok := registered.callTool(ctx, mcpToolCallParams{Name: "list_indexed_repositories", Arguments: map[string]any{}}, "").(mcpToolResult)
	if !ok || !res.IsError {
		t.Fatal("registered HTTP failure did not return a tool error")
	}
	if over, count, sum := collectBudgetMetrics(t, reader, "list_indexed_repositories"); over != 0 || count != 1 || sum != int64(len(marshalForCompare(t, res))) {
		t.Fatalf("registered error budget metrics = (over=%d,count=%d,sum=%d), want one exact wire sample", over, count, sum)
	}
	requireToolCalls(t, reader, map[string]int64{
		"unknown/unknown_tool": 1, "list_indexed_repositories/error": 1,
	})
}

func BenchmarkGenericHTTPErrorToolCall(b *testing.B) {
	s := NewServer(fixedBodyHandler(http.StatusInternalServerError, "boom"), discardDispatchLogger())
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	params := mcpToolCallParams{Name: "list_indexed_repositories", Arguments: map[string]any{}}
	b.ReportAllocs()
	for range b.N {
		wire, err := json.Marshal(s.callTool(ctx, params, ""))
		if err != nil || len(wire) == 0 {
			b.Fatal(err)
		}
	}
}
