// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsOperations documents the live operations board endpoint
// (#5137): one bounded read model combining health, collector runtime
// heartbeat, stage summaries, domain backlogs, and queue depth with
// live_activity, a bounded, separately-queried list of in-flight work items
// joined to their originating repo and worker. Scoped tokens receive the same
// aggregate sections; live_activity is first restricted to the caller's
// granted repositories/ingestion scopes (a scoped caller with NO grants sees
// an empty array, never another tenant's rows), then withholds
// source_key/source_display (repo identity, raw and human-readable) and
// lease_owner (worker identity) on every row it does return; collectors
// collapse to aggregate counts.
const openAPIPathsOperations = `
    "/api/v0/status/operations": {
      "get": {
        "tags": ["status"],
        "summary": "Get live operations board read model",
        "description": "Returns one bounded operator read model composing health, collector runtime status (with heartbeat), stage summaries, domain backlogs, and queue depth with live_activity: up to the limit query parameter's number of in-flight work items (claimed, running, retrying) joined to their originating repo, ordered by most-recently-updated first. source_display resolves the operator-facing repo name from the scope payload (repo_slug or repo_name), falling back to the raw source_key when neither is present. Scoped tokens are first restricted to their granted repositories/ingestion scopes: a scoped token with no granted repository or ingestion scope always receives an empty live_activity array, never another tenant's rows. Within that restricted set, scoped tokens receive the same aggregate sections with source_key, source_display (repo identity, raw and human-readable) and lease_owner (worker identity) withheld from live_activity rows, and collectors collapsed to aggregate counts.",
        "operationId": "getOperations",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {"type": "integer", "default": 100, "minimum": 1, "maximum": 500},
            "description": "Maximum live_activity rows to return (default 100, max 500)"
          }
        ],
        "responses": {
          "200": {
            "description": "Live operations board read model",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "as_of": {"type": "string", "format": "date-time"},
                    "scoped": {"type": "boolean"},
                    "health": {"type": "object"},
                    "collectors": {"type": "array", "items": {"type": "object"}},
                    "stage_summaries": {"type": "array", "items": {"type": "object"}},
                    "domain_backlogs": {"type": "array", "items": {"type": "object"}},
                    "queue": {"type": "object"},
                    "live_activity": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "work_item_id": {"type": "string"},
                          "stage": {"type": "string"},
                          "status": {"type": "string", "enum": ["claimed", "running", "retrying"]},
                          "domain": {"type": "string"},
                          "lease_owner": {"type": "string"},
                          "claim_until": {"type": "string", "format": "date-time", "nullable": true},
                          "attempt_count": {"type": "integer"},
                          "updated_at": {"type": "string", "format": "date-time", "nullable": true},
                          "created_at": {"type": "string", "format": "date-time", "nullable": true},
                          "age_seconds": {"type": "number"},
                          "scope_kind": {"type": "string"},
                          "collector_kind": {"type": "string"},
                          "source_system": {"type": "string"},
                          "source_key": {"type": "string"},
                          "source_display": {"type": "string"}
                        }
                      }
                    },
                    "truncated": {"type": "boolean"},
                    "limit": {"type": "integer"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
