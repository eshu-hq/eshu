// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// SurfaceInventory documents the surface inventory readiness route.
const SurfaceInventory = `
    "/api/v0/surface-inventory": {
      "get": {
        "tags": ["capabilities"],
        "summary": "List the surface inventory readiness rows",
        "description": "Returns the generated surface inventory from the embedded artifact: every platform surface across six categories (command, collector, reducer_domain, api_route, mcp_tool, console_page) with its readiness lane, owner, promotion proof, docs, notes, and collector-only source provenance contracts. Collector contracts map emitted fact kinds to projection/read surfaces, proof gates, fixtures, and the truth profile that distinguishes deterministic, provider-gated, and optional semantic output. The read is static, bounded, and exact in every profile, and backs the MCP get_surface_inventory tool and the console surface inventory page. Supports optional category and readiness filters with deterministic limit/offset paging.",
        "operationId": "listSurfaceInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "category", "in": "query", "required": false, "schema": {"type": "string", "enum": ["command", "collector", "reducer_domain", "api_route", "mcp_tool", "console_page"]}, "description": "Optional surface category filter."},
          {"name": "readiness", "in": "query", "required": false, "schema": {"type": "string", "enum": ["implemented", "partial", "gated", "foundation_only", "fixture_only", "research_only", "not_implemented", "unsupported"]}, "description": "Optional readiness lane filter."},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 1000, "default": 200}, "description": "Maximum number of surfaces to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of surfaces to skip for paging."}
        ],
        "responses": {
          "200": {
            "description": "Surface inventory page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string", "description": "Embedded surface inventory artifact version. Owner: query.SurfaceInventoryHandler over capabilitycatalog.SurfaceInventory."},
                    "total": {"type": "integer", "description": "Surfaces matching the filters before paging. Owner: query.SurfaceInventoryHandler."},
                    "limit": {"type": "integer", "description": "Maximum surfaces returned. Owner: query.SurfaceInventoryHandler."},
                    "offset": {"type": "integer", "description": "Surfaces skipped for paging. Owner: query.SurfaceInventoryHandler."},
                    "truncated": {"type": "boolean", "description": "True when more surfaces matched than this page carries. Owner: query.SurfaceInventoryHandler."},
                    "surfaces": {
                      "type": "array",
                      "description": "Surface rows in the page. Owner: query.SurfaceInventoryHandler over capabilitycatalog.SurfaceInventory.",
                      "items": {
                        "type": "object",
                        "description": "One platform surface with its readiness lane, owner, promotion proof, docs, notes, and collector contract. Owner: query.SurfaceInventoryHandler over capabilitycatalog.SurfaceRecord.",
                        "properties": {
                          "category": {"type": "string", "description": "Surface category (command, collector, reducer_domain, api_route, mcp_tool, or console_page). Owner: query.SurfaceInventoryHandler."},
                          "name": {"type": "string", "description": "Surface name. Owner: query.SurfaceInventoryHandler."},
                          "readiness": {"type": "string", "description": "Readiness lane of the surface. Owner: query.SurfaceInventoryHandler."},
                          "owner": {"type": "string", "description": "Owning team or system. Owner: query.SurfaceInventoryHandler."},
                          "proof": {"type": "string", "description": "Promotion proof behind the readiness lane. Owner: query.SurfaceInventoryHandler."},
                          "docs": {"type": "array", "description": "Documentation references for the surface. Owner: query.SurfaceInventoryHandler.", "items": {"type": "string"}},
                          "notes": {"type": "string", "description": "Operator notes for the surface. Owner: query.SurfaceInventoryHandler."},
                          "collector_contract": {
                            "type": "object",
                            "description": "Collector-only source-to-read-surface provenance contract. Omitted for non-collector surfaces. Owner: capabilitycatalog collector contract.",
                            "properties": {
                              "fact_kinds": {"type": "array", "description": "Fact kinds the collector emits. Owner: capabilitycatalog collector contract.", "items": {"type": "string"}},
                              "projection_surfaces": {"type": "array", "description": "Projection surfaces fed by the fact kinds. Owner: capabilitycatalog collector contract.", "items": {"type": "string"}},
                              "read_surfaces": {"type": "array", "description": "Read surfaces fed by the fact kinds. Owner: capabilitycatalog collector contract.", "items": {"type": "string"}},
                              "proof_gates": {"type": "array", "description": "Proof gates behind the contract. Owner: capabilitycatalog collector contract.", "items": {"type": "string"}},
                              "fixture_refs": {"type": "array", "description": "Fixture references behind the contract. Owner: capabilitycatalog collector contract.", "items": {"type": "string"}},
                              "truth_profile": {"type": "string", "enum": ["deterministic", "provider_gated", "optional_semantic"], "description": "Truth profile distinguishing deterministic, provider-gated, and optional semantic output. Owner: capabilitycatalog collector contract."}
                            }
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
