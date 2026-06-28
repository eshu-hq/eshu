// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsSurfaceInventory documents the surface inventory readiness route.
const openAPIPathsSurfaceInventory = `
    "/api/v0/surface-inventory": {
      "get": {
        "tags": ["capabilities"],
        "summary": "List the surface inventory readiness rows",
        "description": "Returns the generated surface inventory from the embedded artifact: every platform surface across six categories (command, collector, reducer_domain, api_route, mcp_tool, console_page) with its readiness lane, owner, promotion proof, docs, notes, and collector-only source provenance contracts. Collector contracts map emitted fact kinds to projection/read surfaces, proof gates, fixtures, and the truth profile that distinguishes deterministic, provider-gated, and optional semantic output. The read is static, bounded, and exact in every profile, and backs the MCP get_surface_inventory tool and the console surface inventory page. Supports optional category and readiness filters with deterministic limit/offset paging.",
        "operationId": "listSurfaceInventory",
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
                    "version": {"type": "string"},
                    "total": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "surfaces": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "category": {"type": "string"},
                          "name": {"type": "string"},
                          "readiness": {"type": "string"},
                          "owner": {"type": "string"},
                          "proof": {"type": "string"},
                          "docs": {"type": "array", "items": {"type": "string"}},
                          "notes": {"type": "string"},
                          "collector_contract": {
                            "type": "object",
                            "description": "Collector-only source-to-read-surface provenance contract. Omitted for non-collector surfaces.",
                            "properties": {
                              "fact_kinds": {"type": "array", "items": {"type": "string"}},
                              "projection_surfaces": {"type": "array", "items": {"type": "string"}},
                              "read_surfaces": {"type": "array", "items": {"type": "string"}},
                              "proof_gates": {"type": "array", "items": {"type": "string"}},
                              "fixture_refs": {"type": "array", "items": {"type": "string"}},
                              "truth_profile": {"type": "string", "enum": ["deterministic", "provider_gated", "optional_semantic"]}
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
