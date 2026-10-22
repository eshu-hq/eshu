// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestInterceptsOnlyRouterTools(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{FindToolName: true, CallToolName: true, "ask": false, "find_code": false, "": false} {
		if got := Intercepts(name); got != want {
			t.Errorf("Intercepts(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestHostBuildsTheEngineOnceAndServesBothTools(t *testing.T) {
	t.Parallel()

	var builds atomic.Int32
	host := &Host{Ranker: orderRanker{}}
	newConfig := func() Config {
		builds.Add(1)
		return Config{Registry: fixtureRegistry(), Resolve: fixtureResolve}
	}
	rendered := ToolResult{Content: []Content{{Type: "text", Text: "rendered by the root"}}}
	d := &recordingDispatcher{value: rendered}
	req := fixtureRequest(d)

	found, ok := host.Serve(context.Background(), newConfig, req, FindToolName, map[string]any{"intent": "x"}).(ToolResult)
	if !ok || found.IsError {
		t.Fatalf("Serve(find_tool) = %+v, want candidates", found)
	}
	got := host.Serve(context.Background(), newConfig, req, CallToolName, map[string]any{"name": "get_repo_summary", "arguments": map[string]any{}})
	if result, ok := got.(ToolResult); !ok || result.Content[0].Text != "rendered by the root" {
		t.Fatalf("Serve(call_tool) = %+v, want the dispatcher's rendered result untouched", got)
	}
	refused, _ := host.Serve(context.Background(), newConfig, req, CallToolName, map[string]any{"name": "ask", "arguments": map[string]any{}}).(ToolResult)
	if !refused.IsError || errorCode(t, refused) != "mcp_router_refused" {
		t.Fatalf("Serve(call_tool ask) = %+v, want a refusal", refused)
	}
	if n := builds.Load(); n != 1 {
		t.Fatalf("newConfig ran %d times, want once", n)
	}
}

func TestHostReportsADispatcherError(t *testing.T) {
	t.Parallel()

	host := &Host{Ranker: orderRanker{}}
	d := &recordingDispatcher{err: errors.New("boom")}
	got, _ := host.Serve(context.Background(), func() Config {
		return Config{Registry: fixtureRegistry(), Resolve: fixtureResolve}
	}, fixtureRequest(d), CallToolName, map[string]any{"name": "get_repo_summary", "arguments": map[string]any{}}).(ToolResult)
	if !got.IsError || errorCode(t, got) != "mcp_router_dispatch_failed" {
		t.Fatalf("Serve() with a dispatcher error = %+v, want mcp_router_dispatch_failed", got)
	}
}
