// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// costRank returns an integer sort key for a catalog cost class. Known costs
// sort before unknown costs; within known costs, cheaper surfaces sort first.
func costRank(c catalog.CostClass) int {
	switch c {
	case catalog.CostLow:
		return 0
	case catalog.CostModerate:
		return 1
	case catalog.CostHigh:
		return 2
	default:
		// Unknown / not-in-catalog: sort after all known costs.
		return 3
	}
}

// schemaAsMap coerces v to map[string]any. If v already is a map[string]any it
// is returned directly. Any other value — including nil — yields an empty
// non-nil map. schemaAsMap never panics.
func schemaAsMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// Toolset builds a []provider.Tool from a slice of MCP tool definitions,
// ordered cheapest-first using the catalog. For each mcp.ToolDefinition, the
// corresponding provider.Tool carries the same Name and Description; InputSchema
// is converted from any to map[string]any (if the underlying value is already a
// map[string]any it is reused directly; any other value, including nil, yields an
// empty non-nil map — Toolset never panics on a non-map schema).
//
// Ordering: tools whose name appears in cat are ranked by cost (low < moderate <
// high), with ties broken by Name for determinism. Tools absent from the catalog
// are placed after all catalog-known entries, also sorted by Name. A nil catalog
// treats every tool as unknown-cost and returns tools ordered by Name.
func Toolset(cat *catalog.Catalog, defs []mcp.ToolDefinition) []provider.Tool {
	out := make([]provider.Tool, 0, len(defs))

	// Build a cost lookup from the catalog. A nil catalog leaves this empty so
	// every tool gets the unknown-cost rank (3).
	type rankEntry struct {
		rank int
	}
	ranks := make(map[string]rankEntry, len(defs))
	if cat != nil {
		for _, def := range defs {
			if entry, ok := cat.Lookup(def.Name); ok {
				ranks[def.Name] = rankEntry{rank: costRank(entry.Cost)}
			}
		}
	}

	for _, def := range defs {
		out = append(out, provider.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: schemaAsMap(def.InputSchema),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ri := 3
		if e, ok := ranks[out[i].Name]; ok {
			ri = e.rank
		}
		rj := 3
		if e, ok := ranks[out[j].Name]; ok {
			rj = e.rank
		}
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})

	return out
}
