// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"

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
		"POST /api/v0/relationships/catalog": {
			Method: http.MethodPost, Path: "/api/v0/relationships/catalog", Body: `{}`, Expect: "relationships_catalog",
		},
		"MCP get_index_status": {
			Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_index_status",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_index_status","arguments":{}}}`,
		},
	}
	available := map[string]bool{}
	for _, surface := range inv.Surfaces {
		if surface.Category == capabilitycatalog.SurfaceAPIRoute && surface.Readiness == capabilitycatalog.ReadinessImplemented {
			available[surface.Name] = true
		}
	}
	for id := range operations {
		if id != "MCP get_index_status" && !available[id] {
			return nil, fmt.Errorf("pilot operation %s is absent from the implemented route inventory", id)
		}
	}
	return operations, nil
}
