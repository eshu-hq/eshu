// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/mcp/router/rank"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// rankerBuild records the one startup build of the find_tool ranker for the
// startup log and the nil-ranker check.
type rankerBuild struct {
	built    bool
	tools    int
	entries  int
	duration time.Duration
}

// newLexicalRanker is the production find_tool ranker builder: the lexical
// BM25 ranker from router/rank (#8755).
func newLexicalRanker(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) router.Ranker {
	return rank.New(tools, entries)
}

// newMCPServer builds the MCP server with the production option set. The
// ranker is built once here, inside NewServer, over the profile-filtered tool
// list (#8771). A surface that advertises find_tool as its entry point
// (router, core_router) with no ranker is an error, so a misconfigured
// deployment fails at startup instead of answering
// mcp_router_ranker_unavailable on its first request. The full surface still
// builds the ranker: find_tool stays callable there and a per-request
// ?surface= override can select router.
func newMCPServer(
	handler http.Handler,
	logger *slog.Logger,
	transportAuth func(http.Handler) http.Handler,
	profile query.QueryProfile,
	surface router.Surface,
	newRanker router.RankerBuilder,
	extra ...mcp.ServerOption,
) (*mcp.Server, rankerBuild, error) {
	var build rankerBuild
	opts := []mcp.ServerOption{
		mcp.WithTransportAuth(transportAuth),
		mcp.WithQueryProfile(profile),
		mcp.WithToolSurface(surface),
	}
	if newRanker != nil {
		opts = append(opts, mcp.WithRouterRanker(func(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) router.Ranker {
			start := time.Now()
			ranker := newRanker(tools, entries)
			build = rankerBuild{built: ranker != nil, tools: len(tools), entries: len(entries), duration: time.Since(start)}
			return ranker
		}))
	}
	opts = append(opts, extra...)
	server := mcp.NewServer(handler, logger, opts...)
	if !build.built && (surface == router.SurfaceRouter || surface == router.SurfaceCoreRouter) {
		return nil, build, fmt.Errorf("tool surface %q serves find_tool, but no find_tool ranker was built", surface)
	}
	return server, build, nil
}

// logToolSurface writes the startup "mcp tool surface" line
// (event_name=mcp.tool_surface.configured): the process surface, how many
// tools its tools/list advertises and their byte size, and the find_tool
// ranker build. router_ranker_build_ms times the ranker build alone; the
// catalog-entry load that precedes it is not included.
func logToolSurface(logger *slog.Logger, server *mcp.Server, build rankerBuild) {
	surface, advertised, listBytes := server.ToolSurfaceSummary()
	logger.Info("mcp tool surface",
		telemetry.EventAttr("mcp.tool_surface.configured"),
		"surface", string(surface),
		"result_shape", string(server.ResultShape()),
		"tool_count", advertised,
		"tools_list_bytes", listBytes,
		"router_ranker", build.built,
		"router_ranker_tool_count", build.tools,
		"router_ranker_entry_count", build.entries,
		"router_ranker_build_ms", float64(build.duration.Microseconds())/1000,
	)
}
