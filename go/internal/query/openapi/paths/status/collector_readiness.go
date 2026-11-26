// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// collectorReadinessResponseSchema is the 200-response schema for the
// collector-readiness read model emitted by StatusHandler.getCollectorReadiness
// (go/internal/query/status_collector_readiness.go: collectorReadinessEntry
// plus the version/generated_at/readiness/count payload assembly). Both the
// canonical route and its legacy alias splice this constant so the two cannot
// drift; the lane-5 contract test pins them identical.
const collectorReadinessResponseSchema = `{
                  "type": "object",
                  "properties": {
                    "version": {"type": "string", "description": "Serving binary version from buildinfo; identical for scoped and unscoped callers."},
                    "generated_at": {"type": "string", "format": "date-time", "description": "Snapshot time of this payload in UTC; the readiness cache keys freshness off it."},
                    "count": {"type": "integer", "description": "Number of entries in readiness."},
                    "readiness": {
                      "type": "array",
                      "description": "One redacted readiness record per collector family or instance, ordered as the promotion proofs project them.",
                      "items": {
                        "type": "object",
                        "description": "One redacted collector readiness record. Owner: collector readiness handler promotion proofs.",
                        "required": ["collector_kind", "promotion_state", "reducer_readback", "claim_driven", "recommended_next_action"],
                        "properties": {
                          "collector_kind": {"type": "string", "description": "Collector family kind, for example security_alert."},
                          "instance_id": {"type": "string", "description": "Collector instance id; withheld from scoped callers, who see family-level readiness."},
                          "display_name": {"type": "string", "description": "Operator-facing display name of the instance."},
                          "promotion_state": {"type": "string", "enum": ["implemented", "partial", "failed", "stale", "gated", "disabled", "permission_hidden", "unsupported"], "description": "Promotion state projected from the collector promotion proofs."},
                          "runtime_category": {"type": "string", "description": "Runtime category of the collector."},
                          "health": {"type": "string", "description": "Instance health readback; an acknowledged instance reads at least partial."},
                          "claim_driven": {"type": "boolean", "description": "Whether the instance runs claim-driven."},
                          "claim_state": {"type": "string", "description": "Current claim state of the instance, when claimed."},
                          "source_scope": {"type": "string", "description": "Scope the instance collects for."},
                          "fixture_only": {"type": "boolean", "description": "True when the instance exists only in fixture corpora."},
                          "evidence_sources": {"type": "array", "items": {"type": "string"}, "description": "Evidence source names backing the readiness verdict."},
                          "source_systems": {"type": "array", "items": {"type": "string"}, "description": "Source systems the instance reads."},
                          "observation_count": {"type": "integer", "description": "Count of observations behind the readiness verdict."},
                          "reducer_readback": {"type": "string", "enum": ["available", "pending", "unavailable"], "description": "Reducer readback state for the instance."},
                          "telemetry_handles": {"type": "array", "items": {"type": "string"}, "description": "Telemetry handles identifying the instance signals."},
                          "blockers": {"type": "array", "items": {"type": "string"}, "description": "Human-readable blockers preventing promotion; a failed instance carries the terminal fetch failure blocker."},
                          "recommended_next_action": {"type": "string", "description": "Recommended operator action for the instance state."},
                          "last_proof_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Last evidence time; null when the instance was never observed."},
                          "updated_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Last update time of the instance record; null when unknown."},
                          "last_failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "expired_lease_budget_exhausted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "ghsa_cache_replay", "ghsa_covered_through_out_of_range", "ghsa_window_held", "ghsa_window_no_progress", "ghsa_window_unarmed", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "nvd_baseline_seed_rollback", "nvd_baseline_window_writer_armed", "operator_retired_unclaimed", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "run_age_bound_exceeded", "source_state_record", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"], "description": "Bounded class of the newest unresolved terminal fetch failure; omitted when there is none. Each collector kind reports the common classes, the extension host classes, and its own; any other persisted value reads unknown, and a class an extension defines reads extension_defined, whether the kind is not first-party or the instance is a component-activated extension carrying a first-party kind."},
                          "last_failure_operation": {"type": "string", "description": "Bounded snake_case operation token of that failure, or unknown; omitted when there is none."},
                          "last_failure_at": {"type": "string", "format": "date-time", "description": "When that failure became terminal; omitted when there is none."},
                          "acknowledged_failure": {
                            "type": "object",
                            "description": "An operator-acknowledged terminal failure (POST /api/v0/admin/collector-failures/acknowledge). The instance then reads promotion_state partial with the blocker (terminal fetch failure acknowledged: <class> (<operation>)), never implemented. Bounded tokens and times only; the operator's reason is never returned here. Omitted when none.",
                            "properties": {
                              "failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "expired_lease_budget_exhausted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "ghsa_cache_replay", "ghsa_covered_through_out_of_range", "ghsa_window_held", "ghsa_window_no_progress", "ghsa_window_unarmed", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "nvd_baseline_seed_rollback", "nvd_baseline_window_writer_armed", "operator_retired_unclaimed", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "run_age_bound_exceeded", "source_state_record", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"], "description": "Bounded class of the acknowledged failure."},
                              "failure_operation": {"type": "string", "description": "Bounded operation token of the acknowledged failure."},
                              "failed_at": {"type": "string", "format": "date-time", "description": "When the acknowledged failure became terminal."},
                              "acknowledged_at": {"type": "string", "format": "date-time", "description": "When the operator acknowledged the failure."}
                            }
                          }
                        }
                      }
                    }
                  }
                }`

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
                "schema": ` + collectorReadinessResponseSchema + `
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
                "schema": ` + collectorReadinessResponseSchema + `
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
