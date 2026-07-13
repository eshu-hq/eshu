// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsOperatorControlPlane documents the unified operator read model
// endpoint. The response surfaces queue, reducer-domain, collector-family, and
// dead-letter state in one call; scoped tokens receive the same aggregate
// counts with raw correlation IDs and instance labels withheld.
const openAPIPathsOperatorControlPlane = `
    "/api/v0/status/operator-control-plane": {
      "get": {
        "tags": ["status"],
        "summary": "Get operator control-plane read model",
        "description": "Returns one operator read model combining queue depth with claim-latency and stuck-work signals, reducer-domain backlogs, collector-family promotion verdicts with the newest proof artifact, and dead-letter state classed by reducer domain and collector-generation commit. Correlation identifiers (scope_id, generation_id, domain, collector_kind, failure_class) match the runtime metric and span labels. Scoped tokens receive the same aggregate counts with raw work-item/scope/generation identifiers and instance-level labels withheld.",
        "operationId": "getOperatorControlPlane",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Operator control-plane read model",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "as_of": {"type": "string", "format": "date-time"},
                    "scoped": {"type": "boolean"},
                    "health": {"type": "object"},
                    "queue": {
                      "type": "object",
                      "properties": {
                        "total": {"type": "integer"},
                        "outstanding": {"type": "integer"},
                        "pending": {"type": "integer"},
                        "in_flight": {"type": "integer"},
                        "retrying": {"type": "integer"},
                        "dead_letter": {"type": "integer"},
                        "claim_latency": {"type": "object"},
                        "stuck": {"type": "object"}
                      }
                    },
                    "reducer_domains": {"type": "array", "items": {"type": "object"}},
                    "collector_families": {"type": "array", "items": {"type": "object"}},
                    "dead_letters": {
                      "type": "object",
                      "properties": {
                        "queue_dead_letter": {"type": "integer"},
                        "by_domain": {"type": "array", "items": {"type": "object"}},
                        "collector_generation": {"type": "object"},
                        "latest_failure": {"type": "object"}
                      }
                    },
                    "retry_policies": {"type": "array", "items": {"type": "object"}}
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
`
