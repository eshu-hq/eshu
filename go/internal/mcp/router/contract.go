// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Anchors narrows a find_tool request to the entities the caller already
// knows. Rankers use a non-empty anchor to prefer tools whose input schema
// accepts the matching property.
type Anchors struct {
	// RepoID is a repository identifier.
	RepoID string
	// ServiceID is a service identifier.
	ServiceID string
	// WorkloadID is a workload identifier.
	WorkloadID string
	// Environment is a deployment environment name.
	Environment string
}

// Query is one parsed find_tool request.
type Query struct {
	// Intent is the caller's free-text description of what it wants to do.
	Intent string
	// Anchors holds optional entity identifiers the caller already has.
	Anchors Anchors
	// Limit is the maximum number of candidates to return, 1 to MaxLimit.
	// The dispatch lane applies DefaultLimit when the caller omits it and
	// rejects values outside the range, so a Ranker receives a valid Limit. A
	// Ranker handed Limit below 1 returns no candidates.
	Limit int
}

// Candidate is one ranked tool offered to the caller by find_tool.
type Candidate struct {
	// Name is the registered MCP tool name to pass to call_tool.
	Name string
	// Why is a short, deterministic explanation of the match.
	Why string
	// Cost is the catalog cost class of the tool.
	Cost catalog.CostClass
	// InputSchema is the target tool's input schema, so the caller can build
	// arguments without a second lookup. It aliases the registry definition:
	// treat it as read-only or clone before changing it.
	InputSchema map[string]any
	// Example is a sample arguments object for the tool, or nil when none is
	// curated. Treat it as read-only.
	Example map[string]any
	// Score is the ranker's relative score; higher is a better match. It is
	// comparable only within one result.
	Score float64
}

// Ranker orders registered tools against a Query. Implementations must be
// deterministic: the same inputs produce the same ordered result.
type Ranker interface {
	// Rank returns at most q.Limit candidates, best first, drawn from tools.
	// entries maps a tool name to its catalog entry and may lack a tool.
	// allowed reports whether the caller may see and call a tool by name;
	// Rank must never return a tool for which allowed is false. Treat tools
	// and entries as read-only: the engine passes the server-shared visible
	// slice and its own entries map to concurrent find_tool calls, so sorting
	// or filtering either in place is a data race.
	Rank(ctx context.Context, q Query, tools []toolcontract.ToolDefinition,
		entries map[string]catalog.Entry, allowed func(name string) bool) ([]Candidate, error)
}

// Dispatcher runs one registered tool and returns its result. The mcp root
// injects it, closed over the per-request auth and logger, so this package
// never imports the root (which would be an import cycle). It must route
// through the same dispatch as a direct tools/call, so auth, timeouts,
// response budgets, and envelopes are unchanged. The value is opaque to this
// package: the root returns the tool result it renders for a direct
// tools/call, so the envelope and truth labels pass through untouched.
type Dispatcher func(ctx context.Context, name string, arguments map[string]any) (any, error)
