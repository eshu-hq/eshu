// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsOperations documents the live operations board endpoint
// (#5137): one bounded read model combining health, collector runtime
// heartbeat, stage summaries, domain backlogs, and queue depth with
// live_activity, a bounded, separately-queried list of in-flight work items
// joined to their originating repo and worker. For scoped callers,
// live_activity is first restricted to the caller's granted repositories/
// ingestion scopes (a scoped caller with NO grants sees an empty array, never
// another tenant's rows), then withholds
// source_key/source_display (repo identity, raw and human-readable) and
// lease_owner (worker identity) on every row it does return. The process-global
// health, collector, stage, domain, and queue aggregates are omitted because
// they cannot be attributed to those grants; completeness_state and
// withheld_sections make that partial projection explicit. Every row also
// carries generation_state
// (#5138): "active" or "stale", reporting whether a retrying row belongs to
// the scope's current active generation or a superseded one -- claimed/
// running rows are always "active" regardless of generation.
const openAPIPathsOperations = `
    "/api/v0/status/operations": {
      "get": {
        "tags": ["status"],
        "summary": "Get live operations board read model",
        "description": "Returns one bounded operator read model composing health, collector runtime status (with heartbeat), stage summaries, domain backlogs, and queue depth with live_activity: up to the limit query parameter's number of in-flight work items (claimed, running, retrying) joined to their originating repo, ordered by most-recently-updated first. source_display resolves the operator-facing repo name from the scope payload (repo_slug or repo_name), falling back to the raw source_key when neither is present. generation_state is 'active' or 'stale': a retrying row is 'stale' when it belongs to a scope_generations row older than the scope's current active generation (a superseded generation); claimed and running rows are always 'active' regardless of generation, since a live claim/lease stays operator-relevant even against a stale generation. Scoped callers are first restricted to their granted repositories/ingestion scopes: a scoped caller with no granted repository or ingestion scope always receives an empty live_activity array, never another tenant's rows. source_key, source_display (repo identity, raw and human-readable), and lease_owner (worker identity) are withheld from those rows. Because the status snapshot's health, collector, stage, domain, and queue aggregates are process-global rather than grant-scoped, scoped responses omit those five sections, report completeness_state=scoped_live_activity_only plus withheld_sections, and carry derived truth. All-scopes callers retain the complete aggregate board and exact truth.",
        "operationId": "getOperations",
        "x-scoped-token-support": true,
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
                    "completeness_state": {"type": "string", "enum": ["scoped_live_activity_only"], "description": "Present only for scoped callers whose response omits process-global aggregates"},
                    "withheld_sections": {"type": "array", "items": {"type": "string", "enum": ["health", "collectors", "stage_summaries", "domain_backlogs", "queue"]}, "description": "Process-global sections omitted from a scoped response"},
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
                          "source_display": {"type": "string"},
                          "generation_state": {"type": "string", "enum": ["active", "stale"]}
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
