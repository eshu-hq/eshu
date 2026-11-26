// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// OperatorControlPlane documents the unified operator read model
// endpoint. The response surfaces queue, reducer-domain, collector-family, and
// dead-letter state in one call; scoped tokens receive the same aggregate
// counts with raw correlation IDs and instance labels withheld.
const OperatorControlPlane = `
    "/api/v0/status/operator-control-plane": {
      "get": {
        "tags": ["status"],
        "summary": "Get operator control-plane read model",
        "description": "Returns one operator read model combining queue depth with claim-latency and stuck-work signals, reducer-domain backlogs, collector-family promotion verdicts with the newest proof artifact (an instance with an unresolved terminal fetch failure makes its family promotion_state failed with the blocker (terminal fetch failure: <class> (<operation>)), the same rule as GET /api/v0/status/collector-readiness), and dead-letter state classed by reducer domain and collector-generation commit. Correlation identifiers (scope_id, generation_id, domain, collector_kind, failure_class) match the runtime metric and span labels. Scoped tokens receive the same aggregate counts with raw work-item/scope/generation identifiers and instance-level labels withheld. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state, such as a status reader that is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.operator_control_plane_status.",
        "operationId": "getOperatorControlPlane",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                    "health": ` + healthSchema + `,
                    "queue": {
                      "type": "object",
                      "description": "Operator queue view: depth plus claim-latency and stuck-work signals. Scoped callers receive the same counts with raw identifiers withheld elsewhere.",
                      "properties": {
                        "total": {"type": "integer", "description": "Work items known to the queue across all states."},
                        "outstanding": {"type": "integer", "description": "Work items not in a terminal state."},
                        "pending": {"type": "integer", "description": "Outstanding items with no current claim or lease."},
                        "in_flight": {"type": "integer", "description": "Items under an active claim or lease."},
                        "retrying": {"type": "integer", "description": "Items scheduled for another attempt after a counted failure."},
                        "dead_letter": {"type": "integer", "description": "Items in dead_letter after exhausting retries or a non-retryable failure."},
                        "claim_latency": {
                          "type": "object",
                          "description": "Claim latency signals over the outstanding set.",
                          "properties": {
                            "overdue_claims": {"type": "integer", "description": "Claims past their claim_until without a terminal update."},
                            "oldest_outstanding_age": {"type": "number", "description": "Age in seconds of the oldest outstanding item."},
                            "oldest_outstanding_age_ms": {"type": "integer", "description": "Age in whole milliseconds of the oldest outstanding item."},
                            "coordinator_oldest_pending": {"type": "number", "description": "Age in seconds of the oldest pending coordinator claim."}
                          }
                        },
                        "stuck": {
                          "type": "object",
                          "description": "Stuck-work signals: the oldest outstanding age beside the blocked-conflict count.",
                          "properties": {
                            "oldest_outstanding_age": {"type": "number", "description": "Age in seconds of the oldest outstanding item."},
                            "blocked_conflicts": {"type": "integer", "description": "Conflicts currently holding work items."}
                          }
                        }
                      }
                    },
                    "reducer_domains": {
                      "type": "array",
                      "description": "Per-domain backlog depth. Scoped callers receive the same counts with raw identifiers withheld.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "domain": {"type": "string"},
                          "outstanding": {"type": "integer"},
                          "in_flight": {"type": "integer"},
                          "retrying": {"type": "integer"},
                          "dead_letter": {"type": "integer"},
                          "failed": {"type": "integer"},
                          "oldest_age": {"type": "number", "description": "Age in seconds of the oldest backlog item."}
                        }
                      }
                    },
                    "collector_families": {
                      "type": "array",
                      "description": "Collector-family promotion verdicts. Scoped callers receive the same verdicts with display_name and blockers withheld.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "collector_kind": {"type": "string"},
                          "promotion_state": {"type": "string"},
                          "health": {"type": "string"},
                          "claim_state": {"type": "string"},
                          "reducer_readback": {"type": "string"},
                          "last_observed_at": {"type": ["string", "null"], "format": "date-time"},
                          "telemetry": {"type": "array", "items": {"type": "string"}, "description": "Stable metric and span names for diagnosing this family."},
                          "display_name": {"type": "string", "description": "Withheld from scoped callers."},
                          "blockers": {"type": "array", "items": {"type": "string"}, "description": "Instance-level blockers, including terminal fetch failures; withheld from scoped callers."}
                        }
                      }
                    },
                    "dead_letters": {
                      "type": "object",
                      "description": "Dead-letter state classed by reducer domain and collector-generation commit.",
                      "properties": {
                        "queue_dead_letter": {"type": "integer", "description": "Total dead-letter items across the queue."},
                        "by_domain": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "domain": {"type": "string"},
                              "dead_letter": {"type": "integer"},
                              "oldest_age": {"type": "number", "description": "Age in seconds of the oldest dead-letter item in this domain."}
                            }
                          }
                        },
                        "collector_generation": {
                          "type": "object",
                          "description": "Dead-letter state of the collector-generation commit path.",
                          "properties": {
                            "dead_letter": {"type": "integer"},
                            "replay_requested": {"type": "boolean"},
                            "replay_attempts": {"type": "integer"},
                            "oldest_dead_letter_age": {"type": "number", "description": "Age in seconds of the oldest dead-letter item."},
                            "oldest_dead_letter_age_ms": {"type": "integer", "description": "Age in whole milliseconds of the oldest dead-letter item."}
                          }
                        },
                        "latest_failure": {
                          "type": "object",
                          "nullable": true,
                          "description": "Newest dead-letter failure. Null when no failure exists so an all-empty object never reads as a failure record; raw work-item, scope, and generation ids are withheld from scoped callers.",
                          "properties": {
                            "failure_class": {"type": "string"},
                            "domain": {"type": "string"},
                            "at": {"type": ["string", "null"], "format": "date-time"},
                            "work_item_id": {"type": "string", "description": "Withheld from scoped callers."},
                            "scope_id": {"type": "string", "description": "Withheld from scoped callers."},
                            "generation_id": {"type": "string", "description": "Withheld from scoped callers."}
                          }
                        }
                      }
                    },
                    "retry_policies": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "stage": {"type": "string"},
                          "max_attempts": {"type": "integer"},
                          "retry_delay": {"type": "string", "description": "Go duration string of the retry delay."},
                          "retry_delay_ms": {"type": "integer"},
                          "retry_delay_seconds": {"type": "number"}
                        }
                      }
                    },
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"}
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
