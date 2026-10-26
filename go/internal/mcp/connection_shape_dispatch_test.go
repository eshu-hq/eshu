// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"net/http"
	"testing"
	"time"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// shapeCounts returns how many content blocks, embedded resources, and
// whether structuredContent a rendered result carries.
func shapeCounts(result mcpToolResult) (blocks, resources int, structured bool) {
	for _, content := range result.Content {
		if content.Type == "resource" {
			resources++
		}
	}
	return len(result.Content), resources, result.StructuredContent != nil
}

// TestDispatchDeadlinePartialFollowsConnectionShape drives the real dispatcher
// so the call-site shape handed to deadlinePartialDispatchResult is pinned,
// not only the helper.
func TestDispatchDeadlinePartialFollowsConnectionShape(t *testing.T) {
	t.Parallel()
	partial := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"deadline_truncated":true,"rows":[]},"truth":{"level":"partial"},"error":null}`))
		<-r.Context().Done()
	})
	cases := []struct {
		mode          resultshape.Mode
		wantBlocks    int
		wantResources int
	}{
		{resultshape.Structured, 1, 0},
		{resultshape.Dual, 2, 1},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			ctx := resultshape.WithOverride(context.Background(), tc.mode)
			result, err := dispatchToolWithOptions(
				ctx, partial, partialCapableToolName, map[string]any{"topic": "x"}, "", discardDispatchLogger(),
				dispatchOptions{timeout: 20 * time.Millisecond, responseByteBudget: defaultToolResponseByteBudget},
			)
			if err != nil || result == nil {
				t.Fatalf("dispatch = (%v, %v), want a bounded partial result", result, err)
			}
			blocks, resources, structured := shapeCounts(renderToolResult(partialCapableToolName, result))
			if blocks != tc.wantBlocks || resources != tc.wantResources || !structured {
				t.Fatalf("%s: blocks = %d, resources = %d, structured = %v, want %d, %d, true",
					tc.mode, blocks, resources, structured, tc.wantBlocks, tc.wantResources)
			}
		})
	}
}

// TestDispatchPlainTextBodyFollowsConnectionShape pins the non-JSON 2xx body
// path: a text body is one content block under structured and keeps the
// two-block dual layout otherwise.
func TestDispatchPlainTextBodyFollowsConnectionShape(t *testing.T) {
	t.Parallel()
	text := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json at all"))
	})
	cases := []struct {
		mode          resultshape.Mode
		wantBlocks    int
		wantResources int
	}{
		{resultshape.Structured, 1, 0},
		{resultshape.Dual, 2, 1},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			ctx := resultshape.WithOverride(context.Background(), tc.mode)
			result, err := dispatchToolWithOptions(
				ctx, text, "find_symbol", map[string]any{"symbol": "Handle", "limit": 5}, "", discardDispatchLogger(),
				dispatchOptions{responseByteBudget: defaultToolResponseByteBudget},
			)
			if err != nil || result == nil {
				t.Fatalf("dispatch = (%v, %v), want a plain-text result", result, err)
			}
			if result.Value != "not json at all" {
				t.Fatalf("Value = %#v, want the raw body", result.Value)
			}
			blocks, resources, _ := shapeCounts(renderToolResult("find_symbol", result))
			if blocks != tc.wantBlocks || resources != tc.wantResources {
				t.Fatalf("%s: blocks = %d, resources = %d, want %d and %d", tc.mode, blocks, resources, tc.wantBlocks, tc.wantResources)
			}
		})
	}
}

// TestDispatchCallCounterOutcomeIgnoresResultShape proves the result shape
// never changes which outcome the dispatch-entry counter records.
func TestDispatchCallCounterOutcomeIgnoresResultShape(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	partial := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"deadline_truncated":true,"rows":[]},"truth":{"level":"partial"},"error":null}`))
		<-r.Context().Done()
	})
	envelopeFail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query.WriteErrorEnvelope(w, r, http.StatusNotFound, &query.ErrorEnvelope{Code: query.ErrorCodeNotFound, Message: "missing"})
	})
	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	symbol := map[string]any{"symbol": "Handle", "limit": 5}
	budgeted := dispatchOptions{responseByteBudget: defaultToolResponseByteBudget}
	short := dispatchOptions{timeout: 20 * time.Millisecond, responseByteBudget: defaultToolResponseByteBudget}

	cases := []struct {
		name    string
		handler http.Handler
		tool    string
		args    map[string]any
		opts    dispatchOptions
		want    string
	}{
		{"ok", bigRowsHandler(t, 2, 16), "find_symbol", symbol, budgeted, "find_symbol/ok"},
		{"envelope failure", envelopeFail, "find_symbol", symbol, budgeted, "find_symbol/error"},
		{"over budget", bigRowsHandler(t, 200, 256), "find_symbol", symbol, dispatchOptions{responseByteBudget: 4 * 1024}, "find_symbol/over_budget"},
		{"timeout", hang, "find_symbol", symbol, short, "find_symbol/timeout"},
		{"partial timeout", partial, partialCapableToolName, map[string]any{"topic": "x"}, short, partialCapableToolName + "/partial_timeout"},
	}
	for _, tc := range cases {
		for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
			t.Run(tc.name+"/"+string(mode), func(t *testing.T) {
				reader := callMetricsForTest(t)
				ctx := resultshape.WithOverride(context.Background(), mode)
				_, _ = dispatchForCall(ctx, tc.handler, tc.tool, tc.args, tc.opts)
				requireToolCalls(t, reader, map[string]int64{tc.want: 1})
			})
		}
	}
}

// TestStructuredShapeKeepsNullAndTextOnlyErrors distinguishes a successful
// JSON null from failures that have only a text item.
func TestStructuredShapeKeepsNullAndTextOnlyErrors(t *testing.T) {
	t.Parallel()
	null := callShapedTool(t, fixedBodyHandler(http.StatusOK, "null"), resultshape.Structured)
	if len(null.Content) != 1 || null.Content[0].Type != "text" || null.StructuredContent == nil {
		t.Fatalf("null result = %#v, want one text item and structuredContent", null)
	}
	if got := string(marshalForCompare(t, null.StructuredContent)); got != `{"value":null}` {
		t.Fatalf("null structuredContent = %s, want wrapped null", got)
	}
	failed := callShapedTool(t, fixedBodyHandler(http.StatusInternalServerError, "boom"), resultshape.Structured)
	if len(failed.Content) != 1 || failed.Content[0].Type != "text" || failed.StructuredContent != nil || !failed.IsError {
		t.Fatalf("non-envelope HTTP failure = %#v, want text-only error", failed)
	}
	unknown := NewServer(fixedBodyHandler(http.StatusOK, "{}"), discardDispatchLogger())
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	got, ok := unknown.callTool(ctx, mcpToolCallParams{Name: "no_such_tool_a1b2c3", Arguments: map[string]any{}}, "").(mcpToolResult)
	if !ok || len(got.Content) != 1 || got.StructuredContent != nil || !got.IsError {
		t.Fatalf("unknown tool result = %#v (ok=%v), want one text item, an error, and no structuredContent", got, ok)
	}
	plain := mcpToolErrorResult(resultshape.Structured, errPlain("HTTP 404: not found"))
	if !plain.IsError || len(plain.Content) != 1 || plain.Content[0].Resource != nil || plain.StructuredContent != nil {
		t.Fatalf("plain dispatch error = %#v, want one text item, isError, and no structuredContent", plain)
	}
}
