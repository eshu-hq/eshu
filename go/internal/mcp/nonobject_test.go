// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"encoding/json"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
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
		{name: "null", body: `null`, encoding: "json"},
		{name: "false", body: `false`, encoding: "json"},
		{name: "large integer", body: `9007199254740993`, encoding: "json"},
		{name: "exponent", body: `1.234567890123456789e+30`, encoding: "json"},
		{name: "JSON string", body: `"hello"`, encoding: "json"},
		{name: "plain text", body: "hello\nworld", encoding: "text"},
		{name: "empty text", body: "", encoding: "text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := callShapedTool(t, fixedBodyHandler(200, tc.body), resultshape.Structured)
			if result.IsError {
				t.Fatal("successful payload became a tool error")
			}
			requireOneTextAndStructured(t, result)
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
