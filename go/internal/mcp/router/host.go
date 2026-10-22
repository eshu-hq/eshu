// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"log/slog"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
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
	// Ranker orders find_tool candidates. Set it before the first request,
	// directly or through NewRanker and Prepare; while it is nil find_tool
	// answers mcp_router_ranker_unavailable.
	Ranker Ranker
	// NewRanker, when set, builds Ranker once in Prepare. cmd/mcp-server sets
	// it to the lexical ranker (router/rank) through mcp.WithRouterRanker
	// (#8771).
	NewRanker RankerBuilder

	// entries is the catalog map Prepare built Ranker over. The Engine gets
	// this same map, so every Rank call sees the inputs the index was built
	// from; nil until Prepare builds a ranker.
	entries map[string]catalog.Entry
	once    sync.Once
	engine  *Engine
}

// RankerBuilder builds a Ranker over the tools find_tool ranks and their
// catalog entries. It runs once per server, at startup. The Engine hands Rank
// that same slice and map on every call, so a ranker may index them here and
// reuse the index; it must not keep or edit them in any other way.
type RankerBuilder func(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) Ranker

// Prepare builds Ranker with NewRanker at server start, before any request.
// tools must be the slice the root passes as Request.Visible on every call
// (the profile-filtered registry), so the ranker's index matches what Rank is
// handed. It loads the catalog entries once and keeps them for the Engine.
// Prepare does nothing when NewRanker is nil or Ranker is already set, and
// is not safe to call concurrently with Serve. A nil logger uses
// slog.Default. A NewRanker that returns nil leaves find_tool unavailable;
// the caller decides whether that is fatal (cmd/mcp-server refuses to start
// on the router and core_router surfaces).
func (h *Host) Prepare(tools []toolcontract.ToolDefinition, logger *slog.Logger) {
	if h.NewRanker == nil || h.Ranker != nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	h.entries = loadCatalogEntries(logger)
	h.Ranker = h.NewRanker(tools, h.entries)
}

// Serve answers one find_tool or call_tool request. newConfig runs once, on
// the first call, to build the Engine; a nil Config.Ranker takes h.Ranker and
// a nil Config.Entries takes the map Prepare built the ranker over. A
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
		if cfg.Entries == nil {
			cfg.Entries = h.entries
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
