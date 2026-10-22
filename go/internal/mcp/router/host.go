// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"sync"
)

// Intercepts reports whether name is a router tool the mcp root must hand to
// Host.Serve instead of dispatchTool.
func Intercepts(name string) bool {
	return name == FindToolName || name == CallToolName
}

// Host is the router state one MCP server embeds. It builds the Engine on
// the first router request and serves find_tool and call_tool through it.
// The zero value is ready to use.
type Host struct {
	// Ranker orders find_tool candidates. Set it before the first request;
	// while it is nil find_tool answers mcp_router_ranker_unavailable.
	Ranker Ranker

	once   sync.Once
	engine *Engine
}

// Serve answers one find_tool or call_tool request. newConfig runs once, on
// the first call, to build the Engine; a nil Config.Ranker takes h.Ranker. A
// dispatched call_tool returns the Dispatcher's value untouched, so when the
// root's Dispatcher returns the result it renders for a direct tools/call, a
// routed result is byte-identical to the direct one. Every other outcome is
// a ToolResult.
func (h *Host) Serve(ctx context.Context, newConfig func() Config, req Request, name string, args map[string]any) any {
	h.once.Do(func() {
		cfg := newConfig()
		if cfg.Ranker == nil {
			cfg.Ranker = h.Ranker
		}
		h.engine = NewEngine(cfg)
	})
	if name == FindToolName {
		return h.engine.Find(ctx, req, args)
	}
	out := h.engine.Call(ctx, req, args)
	switch {
	case !out.Dispatched:
		return out.Result
	case out.Err != nil:
		return errorResult("mcp_router_dispatch_failed", out.Err.Error(), map[string]any{"tool": out.Target})
	default:
		return out.Value
	}
}
