// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// PilotOperations returns the additional read shapes backed by the seeded
// corpus and the production MCP tool catalog. IDs match inventory routes
// except for MCP, whose ID names the actual tools/call target.
func PilotOperations(inv capabilitycatalog.SurfaceInventory) (map[string]Operation, error) {
	operations := map[string]Operation{
		"GET /api/v0/status/ingesters/{ingester}": {
			Method: http.MethodGet, Path: "/api/v0/status/ingesters/repository", Expect: "ingester_status",
		},
		// #7009 residual rows with no gate coverage: the plain
		// ingester-detail route shares the status variant's handler and
		// payload family, and the three status-proxy MCP tools each front
		// an already-budgeted HTTP route through the MCP envelope.
		"GET /api/v0/ingesters/{ingester}": {
			Method: http.MethodGet, Path: "/api/v0/ingesters/repository", Expect: "ingester_status",
		},
		"MCP get_hosted_readiness": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_hosted_readiness",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_hosted_readiness","arguments":{}}}`,
		},
		"MCP list_ingesters": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_ingesters_list",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_ingesters","arguments":{}}}`,
		},
		"MCP get_operator_control_plane": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_operator_controlplane",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_operator_control_plane","arguments":{}}}`,
		},
		"POST /api/v0/relationships/catalog": {
			Method: http.MethodPost, Path: "/api/v0/relationships/catalog", Body: `{}`, Expect: "relationships_catalog",
		},
		"MCP get_index_status": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_index_status",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_index_status","arguments":{}}}`,
		},
		// #7009: the repository story on the seeded anchor plus related set
		// (path-parameter route the no-arg sweep cannot reach) and the two
		// unscoped MCP search tools over the seeded content corpus. The
		// story path names the seeded anchor id; the searches carry no
		// repo_ids so they run the bounded unscoped walk, not the scoped
		// control.
		"GET /api/v0/repositories/{repo_id}/story": {
			Method: http.MethodGet, Path: "/api/v0/repositories/" + SeedStoryAnchorID + "/story", Expect: "repository_story",
		},
		"MCP search_file_content": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_file_search",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_file_content","arguments":{"pattern":"` + SeedSearchFileToken + `","limit":10}}}`,
		},
		"MCP search_entity_content": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_entity_search",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_entity_content","arguments":{"pattern":"` + SeedEntityToken + `","limit":10}}}`,
		},
	}
	available := map[string]bool{}
	for _, surface := range inv.Surfaces {
		if surface.Category == capabilitycatalog.SurfaceAPIRoute && surface.Readiness == capabilitycatalog.ReadinessImplemented {
			available[surface.Name] = true
		}
	}
	for id := range operations {
		// MCP ids name tools/call targets, not inventory routes, so only
		// HTTP operation ids validate against the inventory.
		if strings.HasPrefix(id, "MCP ") {
			continue
		}
		if !available[id] {
			return nil, fmt.Errorf("pilot operation %s is absent from the implemented route inventory", id)
		}
	}
	return operations, nil
}
