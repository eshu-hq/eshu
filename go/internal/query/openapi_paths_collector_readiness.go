// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCollectorReadiness = `
    "/api/v0/status/collector-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "Get collector family readiness",
        "description": "Returns per-collector-family promotion readiness across the full collector fleet: promotion state, reducer readback status, evidence counts, last proof time, blockers, and a recommended next action. Redacts credentials and raw provider payloads. The MCP tool get_collector_readiness returns the same shape.",
        "operationId": "getCollectorReadiness",
        "responses": {
          "200": {
            "description": "Collector family readiness",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "generated_at": {"type": "string", "format": "date-time"},
                    "count": {"type": "integer"},
                    "readiness": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "required": ["collector_kind", "promotion_state", "reducer_readback", "claim_driven", "recommended_next_action"],
                        "properties": {
                          "collector_kind": {"type": "string"},
                          "instance_id": {"type": "string"},
                          "display_name": {"type": "string"},
                          "promotion_state": {"type": "string", "enum": ["implemented", "partial", "failed", "stale", "gated", "disabled", "permission_hidden", "unsupported"]},
                          "runtime_category": {"type": "string"},
                          "health": {"type": "string"},
                          "claim_driven": {"type": "boolean"},
                          "claim_state": {"type": "string"},
                          "source_scope": {"type": "string"},
                          "fixture_only": {"type": "boolean"},
                          "evidence_sources": {"type": "array", "items": {"type": "string"}},
                          "source_systems": {"type": "array", "items": {"type": "string"}},
                          "observation_count": {"type": "integer"},
                          "reducer_readback": {"type": "string", "enum": ["available", "pending", "unavailable"]},
                          "telemetry_handles": {"type": "array", "items": {"type": "string"}},
                          "blockers": {"type": "array", "items": {"type": "string"}},
                          "recommended_next_action": {"type": "string"},
                          "last_proof_at": {"type": "string", "format": "date-time", "nullable": true},
                          "updated_at": {"type": "string", "format": "date-time", "nullable": true}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/collector-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "Get collector family readiness (alias)",
        "description": "Legacy compatibility alias for /api/v0/status/collector-readiness. Returns the same per-collector-family promotion readiness read model.",
        "operationId": "getCollectorReadinessLegacy",
        "responses": {
          "200": {
            "description": "Collector family readiness",
            "content": {
              "application/json": {
                "schema": {"type": "object"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
