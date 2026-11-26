// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Operations documents the live operations board endpoint
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
const Operations = `
    "/api/v0/status/operations": {
      "get": {
        "tags": ["status"],
        "summary": "Get live operations board read model",
        "description": "Returns one bounded operator read model composing health, collector runtime status (with heartbeat), stage summaries, domain backlogs, and queue depth with live_activity: up to the limit query parameter's number of in-flight work items (claimed, running, retrying) joined to their originating repo, ordered by most-recently-updated first. source_display resolves the operator-facing repo name from the scope payload (repo_slug or repo_name), falling back to the raw source_key when neither is present. generation_state is 'active' or 'stale': a retrying row is 'stale' when it belongs to a scope_generations row older than the scope's current active generation (a superseded generation); claimed and running rows are always 'active' regardless of generation, since a live claim/lease stays operator-relevant even against a stale generation. Scoped callers are first restricted to their granted repositories/ingestion scopes: a scoped caller with no granted repository or ingestion scope always receives an empty live_activity array, never another tenant's rows. source_key, source_display (repo identity, raw and human-readable), and lease_owner (worker identity) are withheld from those rows. Because the status snapshot's health, collector, stage, domain, and queue aggregates are process-global rather than grant-scoped, scoped responses omit those five sections, report completeness_state=scoped_live_activity_only plus withheld_sections, and carry derived truth. All-scopes callers retain the complete aggregate board and exact truth. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read or the live-activity read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader or the live-activity reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 names capability operations.status in its error envelope.",
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
                    "version": {"type": "string", "description": "API binary version serving the board. Owner: query.StatusHandler listCollectors/operations board over buildinfo.AppVersion."},
                    "as_of": {"type": "string", "format": "date-time", "description": "When the board was read. Owner: query.StatusHandler operations board."},
                    "scoped": {"type": "boolean", "description": "True when the caller is scoped and the board omits process-global aggregates. Owner: query.StatusHandler operations board."},
                    "completeness_state": {"type": "string", "enum": ["scoped_live_activity_only"], "description": "Present only for scoped callers whose response omits process-global aggregates"},
                    "withheld_sections": {"type": "array", "items": {"type": "string", "enum": ["health", "collectors", "stage_summaries", "domain_backlogs", "queue"]}, "description": "Process-global sections omitted from a scoped response"},
                    "health": ` + healthSchema + `,
                    "collectors": {
                      "type": "array",
                      "description": "Collector runtime statuses. Scoped callers omit this section; see withheld_sections.",
                      "items": {
                        "type": "object",
                        "description": "One collector runtime status. Owner: query collectorRuntimeStatusToMap over collector.RuntimeStatuses.",
                        "properties": {
                          "instance_id": {"type": "string", "description": "Collector instance identity. Owner: query collectorRuntimeStatusToMap."},
                          "collector_kind": {"type": "string", "description": "Collector family. Owner: query collectorRuntimeStatusToMap."},
                          "mode": {"type": "string", "description": "Collector operating mode. Owner: query collectorRuntimeStatusToMap."},
                          "runtime_mode": {"type": "string", "description": "Collector runtime mode. Owner: query collectorRuntimeStatusToMap."},
                          "status_category": {"type": "string", "enum": ["coordinator_managed", "direct_mode", "profile_gated", "disabled", "unregistered"], "description": "How the collector is managed. Owner: query collectorRuntimeStatusToMap."},
                          "coordinator_registered": {"type": "boolean", "description": "Whether the coordinator registered the collector. Owner: query collectorRuntimeStatusToMap."},
                          "enabled": {"type": "boolean", "description": "Whether the collector is enabled. Owner: query collectorRuntimeStatusToMap."},
                          "bootstrap": {"type": "boolean", "description": "Whether the collector is a bootstrap instance. Owner: query collectorRuntimeStatusToMap."},
                          "claims_enabled": {"type": "boolean", "description": "Whether the collector may claim work. Owner: query collectorRuntimeStatusToMap."},
                          "display_name": {"type": "string", "description": "Human-readable collector name. Owner: query collectorRuntimeStatusToMap."},
                          "health": {"type": "string", "description": "Collector health token. Owner: query collectorRuntimeStatusToMap."},
                          "evidence_sources": {"type": "array", "description": "Evidence sources behind the runtime status. Owner: query collectorRuntimeStatusToMap.", "items": {"type": "string"}},
                          "source_systems": {"type": "array", "description": "Source systems the collector reads. Owner: query collectorRuntimeStatusToMap.", "items": {"type": "string"}},
                          "observation_count": {"type": "integer", "description": "Observations behind the runtime status. Owner: query collectorRuntimeStatusToMap."},
                          "last_observed_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Last observation time; null when the collector has no observation yet."},
                          "updated_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Last update time; null when the collector has no observation yet."},
                          "deactivated_at": {"type": ["string", "null"], "format": "date-time", "description": "Deactivation time, or null while the instance is active."},
                          "detail": {"type": "string", "description": "Human-readable collector detail. Owner: query collectorRuntimeStatusToMap."},
                          "last_failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "expired_lease_budget_exhausted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "ghsa_cache_replay", "ghsa_covered_through_out_of_range", "ghsa_window_held", "ghsa_window_no_progress", "ghsa_window_unarmed", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "nvd_baseline_seed_rollback", "nvd_baseline_window_writer_armed", "operator_retired_unclaimed", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "run_age_bound_exceeded", "source_state_record", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"], "description": "Bounded class of the newest unresolved terminal fetch failure; present only with last_failure_terminal. Each collector kind reports the common classes, the extension host classes, and its own; any other persisted value reads unknown, and a class an extension defines reads extension_defined, whether the kind is not first-party or the instance is a component-activated extension carrying a first-party kind."},
                          "last_failure_operation": {"type": "string", "description": "Bounded snake_case operation token of that failure, or unknown."},
                          "last_failure_terminal": {"type": "boolean", "description": "True when the collector carries an unresolved terminal fetch failure. Owner: query collectorRuntimeStatusToMap."},
                          "last_failure_at": {"type": "string", "format": "date-time", "description": "When that failure became terminal; present only with last_failure_terminal."},
                          "acknowledged_failure": {
                            "type": "object",
                            "description": "An operator-acknowledged terminal failure. Bounded tokens and times only; the operator's reason is never returned. Omitted when none.",
                            "properties": {
                              "failure_class": {"type": "string", "description": "Bounded class of the acknowledged terminal failure. Owner: query collectorRuntimeStatusToMap.", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "expired_lease_budget_exhausted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "ghsa_cache_replay", "ghsa_covered_through_out_of_range", "ghsa_window_held", "ghsa_window_no_progress", "ghsa_window_unarmed", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "nvd_baseline_seed_rollback", "nvd_baseline_window_writer_armed", "operator_retired_unclaimed", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "run_age_bound_exceeded", "source_state_record", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"]},
                              "failure_operation": {"type": "string", "description": "Bounded snake_case operation token of the acknowledged failure. Owner: query collectorRuntimeStatusToMap."},
                              "failed_at": {"type": "string", "format": "date-time", "description": "When the acknowledged failure became terminal. Owner: query collectorRuntimeStatusToMap."},
                              "acknowledged_at": {"type": "string", "format": "date-time", "description": "When the operator acknowledged the failure. Owner: query collectorRuntimeStatusToMap."}
                            }
                          }
                        }
                      }
                    },
                    "stage_summaries": {
                      "type": "array",
                      "description": "Per-stage queue depth. Scoped callers omit this section; see withheld_sections.",
                      "items": ` + stageSummaryItemSchema + `
                    },
                    "domain_backlogs": {
                      "type": "array",
                      "description": "Per-domain backlog depth with blocked shares. Scoped callers omit this section; see withheld_sections.",
                      "items": ` + domainBacklogItemSchema + `
                    },
                    "queue": ` + queueSnapshotSchema + `,
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                    "live_activity": {
                      "type": "array",
                      "description": "Bounded in-flight work items joined to their originating repo and worker, ordered by most-recently-updated first. Scoped callers see only grant-filtered rows with source_key, source_display, and lease_owner withheld. Owner: query liveActivityRowsToSlice.",
                      "items": {
                        "type": "object",
                        "description": "One in-flight work item. Owner: query liveActivityRowsToSlice over status.LiveActivityRow.",
                        "properties": {
                          "work_item_id": {"type": "string", "description": "Work item identity. Owner: query liveActivityRowsToSlice."},
                          "stage": {"type": "string", "description": "Pipeline stage the item sits on. Owner: query liveActivityRowsToSlice."},
                          "status": {"type": "string", "enum": ["claimed", "running", "retrying"], "description": "In-flight item status. Owner: query liveActivityRowsToSlice."},
                          "domain": {"type": "string", "description": "Reducer or projection domain owning the item. Owner: query liveActivityRowsToSlice."},
                          "lease_owner": {"type": "string", "description": "Lease holder identity; withheld from scoped callers. Owner: query liveActivityRowsToSlice."},
                          "claim_until": {"type": "string", "format": "date-time", "nullable": true, "description": "When the claim expires; null when the row carries none. Owner: query liveActivityRowsToSlice."},
                          "attempt_count": {"type": "integer", "description": "Attempts made on the item. Owner: query liveActivityRowsToSlice."},
                          "updated_at": {"type": "string", "format": "date-time", "nullable": true, "description": "When the item was last updated. Owner: query liveActivityRowsToSlice."},
                          "created_at": {"type": "string", "format": "date-time", "nullable": true, "description": "When the item was created. Owner: query liveActivityRowsToSlice."},
                          "age_seconds": {"type": "number", "description": "Item age in seconds at read time. Owner: query liveActivityRowsToSlice."},
                          "scope_kind": {"type": "string", "description": "Scope kind the item belongs to. Owner: query liveActivityRowsToSlice."},
                          "collector_kind": {"type": "string", "description": "Collector family behind the item. Owner: query liveActivityRowsToSlice."},
                          "source_system": {"type": "string", "description": "Source system behind the item. Owner: query liveActivityRowsToSlice."},
                          "source_key": {"type": "string", "description": "Raw repo identity; withheld from scoped callers. Owner: query liveActivityRowsToSlice."},
                          "source_display": {"type": "string", "description": "Operator-facing repo name; withheld from scoped callers. Owner: query liveActivityRowsToSlice."},
                          "generation_state": {"type": "string", "enum": ["active", "stale"], "description": "Whether the row belongs to the scope's current active generation. Owner: query liveActivityRowsToSlice."}
                        }
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when more in-flight rows exist past the limit. Owner: query.StatusHandler operations board."},
                    "limit": {"type": "integer", "description": "Maximum live_activity rows returned (default 100, max 500). Owner: query.StatusHandler operations board."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
