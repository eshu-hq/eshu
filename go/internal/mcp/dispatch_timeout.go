// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"time"
)

const defaultToolDispatchTimeout = 30 * time.Second

type dispatchOptions struct {
	timeout time.Duration
	// responseByteBudget caps the serialized size of the tool response handed
	// back to the client. Zero or negative disables the guard. dispatchTool
	// seeds it with defaultToolResponseByteBudget so every default dispatch is
	// hub-throttled; tests override it to exercise the boundary.
	responseByteBudget int
}

type dispatchContextErr struct {
	toolName string
	timeout  time.Duration
	err      error
}

func (e *dispatchContextErr) Error() string {
	if errors.Is(e.err, context.DeadlineExceeded) {
		return fmt.Sprintf("MCP tool %q dispatch deadline exceeded: %v", e.toolName, e.err)
	}
	return fmt.Sprintf("MCP tool %q dispatch canceled: %v", e.toolName, e.err)
}

func (e *dispatchContextErr) Unwrap() error {
	return e.err
}

func dispatchContextError(toolName string, timeout time.Duration, err error, logger *slog.Logger) error {
	if logger != nil {
		logger.Warn("mcp tool dispatch context ended", "tool", toolName, "timeout", timeout.String(), "err", err)
	}
	return &dispatchContextErr{
		toolName: toolName,
		timeout:  timeout,
		err:      err,
	}
}

// deadlinePartialDispatchResult surfaces a bounded deadline-partial
// envelope (#7008) when the dispatch deadline fired after the handler already
// answered with one: the recorder holds a canonical envelope whose data
// carries deadline_truncated. It reports false for anything else — a missing
// or unparseable body, a non-envelope payload, or an envelope without the
// marker — so those keep the data-nil timeout verdict. A parent cancellation
// (not a deadline) never takes this path: the client is gone, so there is no
// one to read the partial.
func deadlinePartialDispatchResult(rec *httptest.ResponseRecorder, toolName string, responseByteBudget int, logger *slog.Logger) (*dispatchResult, bool) {
	if rec == nil {
		return nil, false
	}
	envelope, ok := parseCanonicalEnvelope(rec.Body.Bytes())
	if !ok || envelope == nil {
		return nil, false
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return nil, false
	}
	marked, _ := data["deadline_truncated"].(bool)
	if !marked {
		return nil, false
	}
	if logger != nil {
		logger.Debug("mcp tool dispatch deadline exceeded; surfacing bounded partial", "tool", toolName)
	}
	return applyResponseBudget(&dispatchResult{
		Value:    envelope,
		Envelope: envelope,
		IsError:  rec.Code >= 400,
	}, toolName, responseByteBudget, logger), true
}

func dispatchErrorStructuredContent(err error) (map[string]any, bool) {
	var dispatchErr *dispatchContextErr
	if !errors.As(err, &dispatchErr) {
		return nil, false
	}
	code := "mcp_dispatch_canceled"
	if errors.Is(dispatchErr.err, context.DeadlineExceeded) {
		code = "mcp_dispatch_timeout"
	}
	return map[string]any{
		"data":  nil,
		"truth": nil,
		"error": map[string]any{
			"code":       code,
			"message":    dispatchErr.Error(),
			"capability": "mcp.dispatch",
			"details": map[string]any{
				"tool":               dispatchErr.toolName,
				"configured_timeout": dispatchErr.timeout.String(),
			},
		},
	}, true
}
