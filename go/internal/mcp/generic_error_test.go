// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
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
