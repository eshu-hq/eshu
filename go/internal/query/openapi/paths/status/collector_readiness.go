// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// CollectorReadiness is the OpenAPI path fragment documenting the
// `/api/v0/collector-readiness`, `/api/v0/status/collector-readiness` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const CollectorReadiness = `
    "/api/v0/status/collector-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "Get collector family readiness",
        "description": "Returns per-collector-family promotion readiness across the full collector fleet: promotion state, reducer readback status, evidence counts, last proof time, blockers, and a recommended next action. An instance with a failed_terminal work item updated inside its window (3 x scan_interval, 30 minute floor) and no later completed work item of the same instance for that scope reports promotion_state failed with the blocker (terminal fetch failure: <class> (<operation>)) plus last_failure_class, last_failure_operation, and last_failure_at. A later completed work item of the same instance for that scope clears it, or an operator acknowledgement (see acknowledged_failure) replaces the failed state with partial; a later pending or retrying item does not clear it, and a failure no later run revisits ages out after the window. Redacts credentials and raw provider payloads: only bounded class and operation tokens appear, never a failure message or status code. An operator can acknowledge the failure (POST /api/v0/admin/collector-failures/acknowledge): the instance then reads promotion_state partial with the blocker (terminal fetch failure acknowledged: <class> (<operation>)) and an acknowledged_failure object, never implemented, until a failure of another class or a failure after a completed work item appears. Scoped callers receive the same failure fields, acknowledged_failure, and blocker with only instance_id withheld. The MCP tool get_collector_readiness returns the same shape. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.collector_readiness_status.",
        "operationId": "getCollectorReadiness",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                          "updated_at": {"type": "string", "format": "date-time", "nullable": true},
                          "last_failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"], "description": "Bounded class of the newest unresolved terminal fetch failure; omitted when there is none. Each collector kind reports the common classes, the extension host classes, and its own; any other persisted value reads unknown, and a class an extension defines reads extension_defined, whether the kind is not first-party or the instance is a component-activated extension carrying a first-party kind."},
                          "last_failure_operation": {"type": "string", "description": "Bounded snake_case operation token of that failure, or unknown; omitted when there is none."},
                          "last_failure_at": {"type": "string", "format": "date-time", "description": "When that failure became terminal; omitted when there is none."},
                          "acknowledged_failure": {
                            "type": "object",
                            "description": "An operator-acknowledged terminal failure (POST /api/v0/admin/collector-failures/acknowledge). The instance then reads promotion_state partial with the blocker (terminal fetch failure acknowledged: <class> (<operation>)), never implemented. Bounded tokens and times only; the operator's reason is never returned here. Omitted when none.",
                            "properties": {
                              "failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"]},
                              "failure_operation": {"type": "string"},
                              "failed_at": {"type": "string", "format": "date-time"},
                              "acknowledged_at": {"type": "string", "format": "date-time"}
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
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/collector-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "Get collector family readiness (alias)",
        "description": "Legacy compatibility alias for /api/v0/status/collector-readiness. Returns the same per-collector-family promotion readiness read model. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.collector_readiness_status.",
        "operationId": "getCollectorReadinessLegacy",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
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
