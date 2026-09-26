// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// requireDefaultResponseWithinBudget dispatches toolName through the real
// dispatcher with the default response budget and fails when the reply is the
// canonical mcp_response_over_budget error or is larger than the budget by the
// dispatcher's own estimateResponseBytes. It returns the dispatch result so a
// caller can also assert the payload's markers. Fixtures behind handler must be
// real query handlers over fake readers, never a synthetic oversized body.
func requireDefaultResponseWithinBudget(
	t *testing.T,
	toolName string,
	handler http.Handler,
	args map[string]any,
) *dispatchResult {
	t.Helper()

	result, err := dispatchTool(
		context.Background(),
		handler,
		toolName,
		args,
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(%s) error = %v, want nil", toolName, err)
	}
	if result == nil {
		t.Fatalf("dispatchTool(%s) = nil, want a result", toolName)
	}
	if result.IsError {
		code, details := "", map[string]any(nil)
		if result.Envelope != nil && result.Envelope.Error != nil {
			code, details = string(result.Envelope.Error.Code), result.Envelope.Error.Details
		}
		t.Fatalf("dispatchTool(%s) default-args reply is an error code=%q details=%v, want a success within budget",
			toolName, code, details)
	}
	size := estimateResponseBytes(result)
	if size > defaultToolResponseByteBudget {
		t.Fatalf("%s default-args response = %d bytes, want <= %d", toolName, size, defaultToolResponseByteBudget)
	}
	t.Logf("%s default-args response_bytes=%d budget=%d", toolName, size, defaultToolResponseByteBudget)
	return result
}
