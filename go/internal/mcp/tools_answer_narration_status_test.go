// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestAnswerNarrationRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_answer_narration_status", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/answer-narration"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestDispatchToolAnswerNarrationStatusAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	statusHandler := &query.StatusHandler{
		StatusReader: fakeMCPStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 14, 6, 40, 0, 0, time.UTC),
			},
		},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:        query.AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	result, err := dispatchTool(
		context.Background(),
		handler,
		"get_answer_narration_status",
		map[string]any{},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want answer narration status envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "answer_narration.status" {
		t.Fatalf("truth = %#v, want answer narration status truth", result.Envelope.Truth)
	}
}
