// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
)

const (
	shapeFixtureOK = `{"data":{"repositories":[{"id":"repo-1","name":"alpha"}],"count":1},` +
		`"truth":{"level":"exact","capability":"platform_impact.context_overview",` +
		`"profile":"local_authoritative","basis":"authoritative_graph","freshness":{"state":"fresh"}},"error":null}`
	shapeFixtureBad   = `{"data":null,"truth":null,"error":{"code":"internal","message":"boom","capability":"x"}}`
	shapeFixturePlain = `{"repos":["a","b"]}`
)

// fixedBodyHandler answers every request with one canned JSON body.
func fixedBodyHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// callShapedTool runs list_indexed_repositories through the production
// callTool path with shape as the connection's result shape.
func callShapedTool(t *testing.T, handler http.Handler, mode resultshape.Mode) mcpToolResult {
	t.Helper()
	s := NewServer(handler, discardDispatchLogger())
	ctx := context.Background()
	if mode != "" {
		ctx = resultshape.WithOverride(ctx, mode)
	}
	result, ok := s.callTool(ctx, mcpToolCallParams{Name: "list_indexed_repositories", Arguments: map[string]any{}}, "").(mcpToolResult)
	if !ok {
		t.Fatalf("callTool result is not an mcpToolResult")
	}
	return result
}

// requireOneTextAndStructured asserts the structured shape contract: exactly
// one text item, no embedded resource, and a structuredContent value.
func requireOneTextAndStructured(t *testing.T, result mcpToolResult) {
	t.Helper()
	if len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text == "" {
		t.Fatalf("content = %#v, want exactly one non-empty text item", result.Content)
	}
	if result.Content[0].Resource != nil {
		t.Fatalf("structured shape carries an embedded resource: %#v", result.Content[0].Resource)
	}
	if result.StructuredContent == nil {
		t.Fatal("structured shape lost structuredContent")
	}
}

func marshalForCompare(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}

// TestStructuredShapeSuccessSendsOneTextAndStructuredContent proves the
// one-copy shape and that its structuredContent is byte-identical to the
// envelope the dual shape embeds as a resource.
func TestStructuredShapeSuccessSendsOneTextAndStructuredContent(t *testing.T) {
	t.Parallel()
	dual := callShapedTool(t, fixedBodyHandler(200, shapeFixtureOK), resultshape.Dual)
	structured := callShapedTool(t, fixedBodyHandler(200, shapeFixtureOK), resultshape.Structured)

	requireOneTextAndStructured(t, structured)
	if structured.IsError {
		t.Fatal("structured success result is marked isError")
	}
	if got, want := string(marshalForCompare(t, structured.StructuredContent)), dual.Content[1].Resource.Text; got != want {
		t.Fatalf("structuredContent = %s, want the dual envelope bytes %s", got, want)
	}
	if structured.Content[0].Text != dual.Content[0].Text {
		t.Fatalf("summary = %q, want the dual summary %q", structured.Content[0].Text, dual.Content[0].Text)
	}
}

// TestStructuredShapeErrorResultsSendOneTextAndStructuredContent covers the
// isError envelope, a plain (non-envelope) payload, and the dispatch error
// result, none of which may carry the eshu://tool-error/dispatch resource.
func TestStructuredShapeErrorResultsSendOneTextAndStructuredContent(t *testing.T) {
	t.Parallel()

	t.Run("error envelope", func(t *testing.T) {
		t.Parallel()
		dual := callShapedTool(t, fixedBodyHandler(500, shapeFixtureBad), resultshape.Dual)
		structured := callShapedTool(t, fixedBodyHandler(500, shapeFixtureBad), resultshape.Structured)
		requireOneTextAndStructured(t, structured)
		if !structured.IsError {
			t.Fatal("structured error result lost isError")
		}
		if got, want := string(marshalForCompare(t, structured.StructuredContent)), dual.Content[1].Resource.Text; got != want {
			t.Fatalf("structuredContent = %s, want %s", got, want)
		}
	})

	t.Run("plain payload", func(t *testing.T) {
		t.Parallel()
		dual := callShapedTool(t, fixedBodyHandler(200, shapeFixturePlain), resultshape.Dual)
		structured := callShapedTool(t, fixedBodyHandler(200, shapeFixturePlain), resultshape.Structured)
		requireOneTextAndStructured(t, structured)
		if got, want := string(marshalForCompare(t, structured.StructuredContent)), dual.Content[1].Resource.Text; got != want {
			t.Fatalf("structuredContent = %s, want %s", got, want)
		}
	})

	t.Run("dispatch error", func(t *testing.T) {
		t.Parallel()
		err := dispatchContextError("list_indexed_repositories", 20*time.Millisecond, context.DeadlineExceeded, discardDispatchLogger())
		dual := mcpToolErrorResult(resultshape.Dual, err)
		structured := mcpToolErrorResult(resultshape.Structured, err)
		requireOneTextAndStructured(t, structured)
		if !structured.IsError {
			t.Fatal("structured dispatch error lost isError")
		}
		if got, want := string(marshalForCompare(t, structured.StructuredContent)), dual.Content[1].Resource.Text; got != want {
			t.Fatalf("structuredContent = %s, want the dual dispatch resource bytes %s", got, want)
		}
		if structured.Content[0].Text != dual.Content[0].Text {
			t.Fatalf("text = %q, want %q", structured.Content[0].Text, dual.Content[0].Text)
		}
	})

	t.Run("dispatch error without structured detail", func(t *testing.T) {
		t.Parallel()
		result := mcpToolErrorResult(resultshape.Structured, errPlain("HTTP 404: not found"))
		if !result.IsError || len(result.Content) != 1 || result.Content[0].Resource != nil {
			t.Fatalf("plain dispatch error = %#v, want one text item and isError", result)
		}
	})
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

// TestDualShapeIsByteForByteUnchanged pins the default shape to fixtures
// captured from the server before the structured shape existed.
func TestDualShapeIsByteForByteUnchanged(t *testing.T) {
	t.Parallel()
	err := dispatchContextError("list_indexed_repositories", 20*time.Millisecond, context.DeadlineExceeded, discardDispatchLogger())
	cases := []struct {
		fixture string
		got     mcpToolResult
	}{
		{"result_shape_dual_success.json", callShapedTool(t, fixedBodyHandler(200, shapeFixtureOK), "")},
		{"result_shape_dual_error.json", callShapedTool(t, fixedBodyHandler(500, shapeFixtureBad), resultshape.Dual)},
		{"result_shape_dual_plain.json", callShapedTool(t, fixedBodyHandler(200, shapeFixturePlain), resultshape.Dual)},
		{"result_shape_dual_dispatch_error.json", mcpToolErrorResult(resultshape.Dual, err)},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			want, readErr := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if readErr != nil {
				t.Fatalf("read fixture: %v", readErr)
			}
			got, marshalErr := json.MarshalIndent(tc.got, "", "  ")
			if marshalErr != nil {
				t.Fatalf("marshal: %v", marshalErr)
			}
			if !bytes.Equal(append(got, '\n'), want) {
				t.Fatalf("dual result drifted from %s:\n%s", tc.fixture, got)
			}
		})
	}
}

// TestStructuredOverBudgetEnvelopeSendsOneTextAndStructuredContent proves the
// bounded over-budget error keeps the one-copy shape.
func TestStructuredOverBudgetEnvelopeSendsOneTextAndStructuredContent(t *testing.T) {
	t.Parallel()
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	result, err := dispatchToolWithOptions(ctx, bigRowsHandler(t, 200, 256), "find_symbol",
		map[string]any{"symbol": "Handle", "limit": 5}, "", discardDispatchLogger(), dispatchOptions{responseByteBudget: 4 * 1024})
	if err != nil || result == nil || !result.IsError || result.Envelope == nil || result.Envelope.Error == nil {
		t.Fatalf("dispatch = (%+v, %v), want the over-budget error envelope", result, err)
	}
	if result.Envelope.Error.Code != errorCodeResponseOverBudget {
		t.Fatalf("error code = %q, want %q", result.Envelope.Error.Code, errorCodeResponseOverBudget)
	}
	rendered := renderToolResult("find_symbol", result)
	requireOneTextAndStructured(t, rendered)
	if !rendered.IsError {
		t.Fatal("over-budget result lost isError")
	}
	if got, want := string(marshalForCompare(t, rendered.StructuredContent)), string(marshalForCompare(t, result.Envelope)); got != want {
		t.Fatalf("structuredContent = %s, want the envelope %s", got, want)
	}
}

// TestRoutedCallKeepsTheConnectionShape proves call_tool returns the same
// bytes as the direct call under the structured shape: the router must not
// re-render a result or fall back to dual.
func TestRoutedCallKeepsTheConnectionShape(t *testing.T) {
	t.Parallel()
	s := newRouterTestServer(t, echoEnvelopeHandler())
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	direct := toolCall(t, s, ctx, "list_indexed_repositories", map[string]any{})
	routed := toolCall(t, s, ctx, "call_tool", map[string]any{"name": "list_indexed_repositories", "arguments": map[string]any{}})
	if !bytes.Equal(direct, routed) {
		t.Fatalf("routed result differs from direct:\ndirect: %s\nrouted: %s", direct, routed)
	}
	var decoded mcpToolResult
	if err := json.Unmarshal(direct, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	requireOneTextAndStructured(t, decoded)
}
