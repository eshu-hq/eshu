// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchCypherQueryPreservesBoundedGraphReadErrorEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantCode query.ErrorCode
	}{
		{name: "unavailable", err: fmt.Errorf("private address: %w", query.ErrGraphUnavailable), wantCode: query.ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private query: %w", query.ErrGraphReadDeadline), wantCode: query.ErrorCodeBackendTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			(&query.CodeHandler{Neo4j: mcpGraphReadErrorReader{err: test.err}}).Mount(mux)
			result, err := dispatchTool(
				context.Background(),
				mux,
				"execute_cypher_query",
				map[string]any{"cypher_query": "RETURN 1", "limit": 10},
				"",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool() error = %v, want envelope result", err)
			}
			if result == nil || !result.IsError || result.Envelope == nil || result.Envelope.Error == nil {
				t.Fatalf("dispatch result = %#v, want canonical error envelope", result)
			}
			if result.Envelope.Error.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", result.Envelope.Error.Code, test.wantCode)
			}
			wantMessage := mcpGraphReadPublicMessage(test.wantCode)
			if result.Envelope.Error.Message != wantMessage {
				t.Fatalf("error message = %q, want %q", result.Envelope.Error.Message, wantMessage)
			}
		})
	}
}

type mcpGraphReadErrorReader struct{ err error }

func (r mcpGraphReadErrorReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, r.err
}

func (r mcpGraphReadErrorReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, r.err
}

func mcpGraphReadPublicMessage(code query.ErrorCode) string {
	if code == query.ErrorCodeBackendUnavailable {
		return query.ErrGraphUnavailable.Error()
	}
	return query.ErrGraphReadDeadline.Error()
}
