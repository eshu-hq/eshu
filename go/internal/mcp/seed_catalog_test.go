// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// seedRouterCatalogRow is one router_catalog seed row per MCP tool: the cost
// class the in-tree ranker scores, the canonical-in-family flag that wins
// exact-score ties, and the curated intent phrases that carry the strongest
// field weight. The satellite renders these rows into the tool_definitions
// contract; tools with no row fall back to its catalog-free behavior.
type seedRouterCatalogRow struct {
	Cost      string   `json:"cost"`
	Canonical bool     `json:"canonical"`
	Intents   []string `json:"intents"`
}

// buildSeedRouterCatalog builds the router_catalog seed rows from the
// annotated catalog, using the same construction the in-tree ranker consumes
// (router.loadCatalogEntries): parse the embedded surface inventory, apply
// the curated overlay with Annotate, and keep the KindMCPTool entries. API
// routes are planner data, not router data, so they are not seeded. The
// emitter and the coverage test below share this helper so they cannot
// diverge.
func buildSeedRouterCatalog(t *testing.T) map[string]seedRouterCatalogRow {
	t.Helper()
	cat, err := catalog.Parse(capabilitycatalog.RawSurfaceArtifact())
	if err != nil {
		t.Fatalf("parse surface inventory: %v", err)
	}
	cat.Annotate()
	rows := make(map[string]seedRouterCatalogRow)
	for _, entry := range cat.Entries() {
		if entry.Kind != catalog.KindMCPTool {
			continue
		}
		rows[entry.Name] = seedRouterCatalogRow{
			Cost:      string(entry.Cost),
			Canonical: entry.Canonical,
			Intents:   append([]string(nil), entry.Intents...),
		}
	}
	return rows
}

// TestSeedRouterCatalogCoversReadOnlyTools is the always-on half of the fix-07
// part-B proof: every tool ReadOnlyTools serves must yield a catalog row with
// a closed-set cost and a non-empty intent list. It runs on every test pass
// without ESHU_SEED_OUT, while the emitter proves the same rows serialize
// byte-stable into the seed facts.
func TestSeedRouterCatalogCoversReadOnlyTools(t *testing.T) {
	t.Parallel()
	rows := buildSeedRouterCatalog(t)
	for _, tool := range ReadOnlyTools() {
		row, ok := rows[tool.Name]
		if !ok {
			t.Errorf("router catalog has no row for ReadOnlyTools %q", tool.Name)
			continue
		}
		switch row.Cost {
		case string(catalog.CostLow), string(catalog.CostModerate), string(catalog.CostHigh):
		default:
			t.Errorf("router catalog row for %q has open-set cost %q, want low|moderate|high", tool.Name, row.Cost)
		}
		if len(row.Intents) == 0 {
			t.Errorf("router catalog row for %q has no intents", tool.Name)
		}
	}
}
