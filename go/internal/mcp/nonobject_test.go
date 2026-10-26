// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestStructuredNonObjectResultsHaveLosslessObjectPayload(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		body     string
		encoding string
	}{
		{name: "empty array", body: `[]`, encoding: "json"},
		{name: "nested array", body: `[1,{"nested":[false,null]}]`, encoding: "json"},
		{name: "nested large integer", body: `[9007199254740993,{"n":9223372036854775807}]`, encoding: "json"},
		{name: "null", body: `null`, encoding: "json"},
		{name: "false", body: `false`, encoding: "json"},
		{name: "large integer", body: `9007199254740993`, encoding: "json"},
		{name: "exponent", body: `1.234567890123456789e+30`, encoding: "json"},
		{name: "JSON string", body: `"hello"`, encoding: "json"},
		{name: "plain text", body: "hello\nworld", encoding: "text"},
		{name: "plain text escapes", body: "a\t\"b\"\\c\nλ", encoding: "text"},
		{name: "malformed object", body: `{"broken":`, encoding: "text"},
		{name: "trailing JSON", body: `{"ok":1} trailing`, encoding: "text"},
		{name: "empty text", body: "", encoding: "text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := callShapedTool(t, fixedBodyHandler(200, tc.body), resultshape.Structured)
			if result.IsError {
				t.Fatal("successful payload became a tool error")
			}
			requireOneTextAndStructured(t, result)
			if got := result.Content[0].Text; got != "list_indexed_repositories result is in structuredContent.value" {
				t.Fatalf("summary = %q, want value pointer", got)
			}
			var wire struct {
				StructuredContent map[string]json.RawMessage `json:"structuredContent"`
				Meta              map[string]string          `json:"_meta"`
			}
			if err := json.Unmarshal(marshalForCompare(t, result), &wire); err != nil {
				t.Fatalf("strict object client cannot decode result: %v", err)
			}
			if len(wire.StructuredContent) != 1 {
				t.Fatalf("structuredContent = %s, want one value field", marshalForCompare(t, wire.StructuredContent))
			}
			if got := wire.Meta["eshu/nonObjectEncoding"]; got != tc.encoding {
				t.Fatalf("non-object encoding = %q, want %q", got, tc.encoding)
			}
			want := []byte(tc.body)
			if tc.encoding == "text" {
				want = marshalForCompare(t, tc.body)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, want); err != nil {
				t.Fatal(err)
			}
			if got := wire.StructuredContent["value"]; !bytes.Equal(got, compact.Bytes()) {
				t.Fatalf("wrapped value = %s, want exact JSON value %s", got, compact.Bytes())
			}
		})
	}
}

func TestStructuredNonObjectRoutingAndRendering(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`[9007199254740993]`, `null`, `false`, `"text"`, "plain\ntext"} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			s := newRouterTestServer(t, fixedBodyHandler(200, body))
			ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
			direct := toolCall(t, s, ctx, "list_indexed_repositories", map[string]any{})
			routed := toolCall(t, s, ctx, "call_tool", map[string]any{"name": "list_indexed_repositories", "arguments": map[string]any{}})
			if !bytes.Equal(direct, routed) {
				t.Fatalf("direct = %s, routed = %s", direct, routed)
			}
			result, err := dispatchToolWithOptions(ctx, fixedBodyHandler(200, body),
				"list_indexed_repositories", map[string]any{}, "", discardDispatchLogger(),
				dispatchOptions{responseByteBudget: 0})
			if err != nil {
				t.Fatal(err)
			}
			first := marshalForCompare(t, renderToolResult(result.ToolName, result))
			second := marshalForCompare(t, renderToolResult(result.ToolName, result))
			if !bytes.Equal(first, second) {
				t.Fatalf("render changed: %s, then %s", first, second)
			}
		})
	}
}

func TestStructuredWrapperOnlyOnNonObjectSuccess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"plain object", 200, `{"value":7}`},
		{"canonical success", 200, shapeFixtureOK},
		{"canonical error", 500, shapeFixtureBad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := callShapedTool(t, fixedBodyHandler(tc.status, tc.body), resultshape.Structured)
			if result.Meta != nil {
				t.Fatalf("unexpected encoding marker: %#v", result.Meta)
			}
			if got := string(marshalForCompare(t, result.StructuredContent)); got == `{"value":{"value":7}}` {
				t.Fatal("plain value object was wrapped again")
			}
		})
	}
}

func TestStructuredNonObjectBudgetCountsExactWire(t *testing.T) {
	t.Parallel()
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	body := `[9007199254740993,{"n":1.234567890123456789e+30}]`
	get := func(budget int) *dispatchResult {
		t.Helper()
		result, err := dispatchToolWithOptions(ctx, fixedBodyHandler(200, body),
			"list_indexed_repositories", map[string]any{}, "", discardDispatchLogger(),
			dispatchOptions{responseByteBudget: budget})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := get(0)
	first := marshalForCompare(t, renderToolResult(result.ToolName, result))
	second := marshalForCompare(t, renderToolResult(result.ToolName, result))
	if !bytes.Equal(first, second) || estimateResponseBytes(result) != len(first) {
		t.Fatalf("render or size changed: first=%s second=%s estimate=%d", first, second, estimateResponseBytes(result))
	}
	if fit := get(len(first)); fit.IsError || estimateResponseBytes(fit) != len(first) {
		t.Fatalf("exact-budget result = %#v, want complete wrapped response", fit)
	}
	if refused := get(len(first) - 1); !refused.IsError || refused.Envelope == nil || refused.Envelope.Error == nil || refused.Envelope.Error.Code != errorCodeResponseOverBudget {
		t.Fatalf("one byte under budget = %#v, want bounded over-budget error", refused)
	}
}

func TestDualNonObjectGoldenBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"array", `[9007199254740993]`, "{\"content\":[{\"type\":\"text\",\"text\":\"Returned 1 result(s).\"},{\"type\":\"resource\",\"resource\":{\"uri\":\"eshu://tool-result/payload\",\"mimeType\":\"application/json\",\"text\":\"[9007199254740992]\"}}],\"structuredContent\":[9007199254740992]}"},
		{"null", `null`, "{\"content\":[{\"type\":\"text\",\"text\":\"Eshu query completed.\"},{\"type\":\"resource\",\"resource\":{\"uri\":\"eshu://tool-result/payload\",\"mimeType\":\"application/json\",\"text\":\"null\"}}]}"},
		{"plain text", "plain\ntext", "{\"content\":[{\"type\":\"text\",\"text\":\"Eshu query completed.\"},{\"type\":\"resource\",\"resource\":{\"uri\":\"eshu://tool-result/payload\",\"mimeType\":\"application/json\",\"text\":\"\\\"plain\\\\ntext\\\"\"}}],\"structuredContent\":\"plain\\ntext\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callShapedTool(t, fixedBodyHandler(200, tc.body), resultshape.Dual)
			if got.Meta != nil {
				t.Fatalf("dual gained encoding marker: %#v", got.Meta)
			}
			if wire := string(marshalForCompare(t, got)); wire != tc.want {
				t.Fatalf("dual wire = %s", wire)
			}
		})
	}
}

func BenchmarkToolResultRendering(b *testing.B) {
	envelope := mustBenchmarkEnvelope(b)
	for _, tc := range []struct {
		name  string
		value any
		shape resultshape.Mode
	}{
		{"canonical_dual", envelope, resultshape.Dual},
		{"canonical_structured", envelope, resultshape.Structured},
		{"plain_object_dual", map[string]any{"results": []any{"a", "b"}}, resultshape.Dual},
		{"plain_object_structured", map[string]any{"results": []any{"a", "b"}}, resultshape.Structured},
		{"array_dual", []any{1.0, "a", false}, resultshape.Dual},
		{"array_structured", []any{1.0, "a", false}, resultshape.Structured},
		{"array_wrapped", json.RawMessage(`[1,"a",false]`), resultshape.Structured},
	} {
		b.Run(tc.name, func(b *testing.B) {
			result := &dispatchResult{Value: tc.value, Shape: tc.shape, ToolName: "list_indexed_repositories"}
			if tc.name == "canonical_dual" || tc.name == "canonical_structured" {
				result.Envelope = envelope
			}
			if tc.name == "array_wrapped" {
				result.NonObjectEncoding = "json"
			}
			b.ReportAllocs()
			for range b.N {
				wire, err := json.Marshal(renderToolResult(result.ToolName, result))
				if err != nil || len(wire) == 0 {
					b.Fatal(err)
				}
			}
		})
	}
}

func mustBenchmarkEnvelope(b *testing.B) *query.ResponseEnvelope {
	b.Helper()
	envelope, ok := parseCanonicalEnvelope([]byte(shapeFixtureOK))
	if !ok {
		b.Fatal("canonical benchmark fixture did not parse")
	}
	return envelope
}
