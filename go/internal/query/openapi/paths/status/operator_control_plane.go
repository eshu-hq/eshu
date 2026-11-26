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
                  "description": "Operator control-plane read model. Owner: query.operatorControlPlaneToMap.",
                  "properties": {
                    "version": {"type": "string", "description": "API binary version. Owner: query.operatorControlPlaneToMap via buildinfo."},
                    "as_of": {"type": "string", "format": "date-time", "description": "Snapshot time in UTC. Owner: query.operatorControlPlaneToMap via ControlPlane AsOf."},
                    "scoped": {"type": "boolean", "description": "True when raw identifiers are withheld for a scoped caller. Owner: query.operatorControlPlaneToMap."},
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
                          "description": "Claim latency signals over the outstanding set. Owner: query.operatorQueueToMap.",
                          "properties": {
                            "overdue_claims": {"type": "integer", "description": "Claims past their claim_until without a terminal update. Owner: query.operatorQueueToMap via OverdueClaims."},
                            "oldest_outstanding_age": {"type": "number", "description": "Age in seconds of the oldest outstanding item. Owner: query.operatorQueueToMap via OldestOutstandingAge."},
                            "oldest_outstanding_age_ms": {"type": "integer", "description": "Age in whole milliseconds of the oldest outstanding item. Owner: query.operatorQueueToMap via OldestOutstandingAge."},
                            "coordinator_oldest_pending": {"type": "number", "description": "Age in seconds of the oldest pending coordinator claim. Owner: query.operatorQueueToMap via OldestPendingAge."}
                          }
                        },
                        "stuck": {
                          "type": "object",
                          "description": "Stuck-work signals: the oldest outstanding age beside the blocked-conflict count. Owner: query.operatorQueueToMap.",
                          "properties": {
                            "oldest_outstanding_age": {"type": "number", "description": "Age in seconds of the oldest outstanding item. Owner: query.operatorQueueToMap via OldestOutstandingAge."},
                            "blocked_conflicts": {"type": "integer", "description": "Conflicts currently holding work items. Owner: query.operatorQueueToMap via BlockedConflicts."}
                          }
                        }
                      }
                    },
                    "reducer_domains": {
                      "type": "array",
                      "description": "Per-domain backlog depth. Scoped callers receive the same counts with raw identifiers withheld. Owner: query.operatorReducerDomainsToSlice.",
                      "items": {
                        "type": "object",
                        "description": "One reducer-domain backlog row. Owner: query.operatorReducerDomainsToSlice via status.DomainBacklog.",
                        "properties": {
                          "domain": {"type": "string", "description": "Reducer or projection domain name. Owner: query.operatorReducerDomainsToSlice via Domain."},
                          "outstanding": {"type": "integer", "description": "Backlog items not in a terminal state. Owner: query.operatorReducerDomainsToSlice via Outstanding."},
                          "in_flight": {"type": "integer", "description": "Items under an active claim or lease. Owner: query.operatorReducerDomainsToSlice via InFlight."},
                          "retrying": {"type": "integer", "description": "Items scheduled for another attempt. Owner: query.operatorReducerDomainsToSlice via Retrying."},
                          "dead_letter": {"type": "integer", "description": "Terminal dead-letter items. Owner: query.operatorReducerDomainsToSlice via DeadLetter."},
                          "failed": {"type": "integer", "description": "Terminal failures outside dead_letter. Owner: query.operatorReducerDomainsToSlice via Failed."},
                          "oldest_age": {"type": "number", "description": "Age in seconds of the oldest backlog item. Owner: query.operatorReducerDomainsToSlice via OldestAge."}
                        }
                      }
                    },
                    "collector_families": {
                      "type": "array",
                      "description": "Collector-family promotion verdicts. Scoped callers receive the same verdicts with display_name and blockers withheld. Owner: query.operatorCollectorFamiliesToSlice.",
                      "items": {
                        "type": "object",
                        "description": "One collector-family promotion verdict. Owner: query.operatorCollectorFamiliesToSlice via status.OperatorCollectorFamily.",
                        "properties": {
                          "collector_kind": {"type": "string", "description": "Collector family kind. Owner: query.operatorCollectorFamiliesToSlice via CollectorKind."},
                          "promotion_state": {"type": "string", "description": "Family promotion verdict. Owner: query.operatorCollectorFamiliesToSlice via PromotionState."},
                          "health": {"type": "string", "description": "Family health token. Owner: query.operatorCollectorFamiliesToSlice via Health."},
                          "claim_state": {"type": "string", "description": "Family claim state. Owner: query.operatorCollectorFamiliesToSlice via ClaimState."},
                          "reducer_readback": {"type": "string", "description": "Reducer readback state for the family. Owner: query.operatorCollectorFamiliesToSlice via ReducerReadback."},
                          "last_observed_at": {"type": ["string", "null"], "format": "date-time", "description": "Last observation time, null when never observed. Owner: query.operatorCollectorFamiliesToSlice via LastObservedAt."},
                          "telemetry": {"type": "array", "items": {"type": "string"}, "description": "Stable metric and span names for diagnosing this family. Owner: query.operatorCollectorFamiliesToSlice via TelemetryHandles."},
                          "display_name": {"type": "string", "description": "Family display name. Withheld from scoped callers. Owner: query.operatorCollectorFamiliesToSlice via DisplayName."},
                          "blockers": {"type": "array", "items": {"type": "string"}, "description": "Instance-level blockers, including terminal fetch failures; withheld from scoped callers. Owner: query.operatorCollectorFamiliesToSlice via Blockers."}
                        }
                      }
                    },
                    "dead_letters": {
                      "type": "object",
                      "description": "Dead-letter state classed by reducer domain and collector-generation commit. Owner: query.operatorDeadLettersToMap.",
                      "properties": {
                        "queue_dead_letter": {"type": "integer", "description": "Total dead-letter items across the queue. Owner: query.operatorDeadLettersToMap via QueueDeadLetter."},
                        "by_domain": {
                          "type": "array",
                          "description": "Dead-letter items by reducer domain. Owner: query.operatorDeadLettersToMap.",
                          "items": {
                            "type": "object",
                            "description": "One domain's dead-letter row. Owner: query.operatorDeadLettersToMap.",
                            "properties": {
                              "domain": {"type": "string", "description": "Reducer or projection domain name. Owner: query.operatorDeadLettersToMap via Domain."},
                              "dead_letter": {"type": "integer", "description": "Terminal dead-letter items in this domain. Owner: query.operatorDeadLettersToMap via DeadLetter."},
                              "oldest_age": {"type": "number", "description": "Age in seconds of the oldest dead-letter item in this domain. Owner: query.operatorDeadLettersToMap via OldestAge."}
                            }
                          }
                        },
                        "collector_generation": {
                          "type": "object",
                          "description": "Dead-letter state of the collector-generation commit path. Owner: query.collectorGenerationDeadLettersToMap.",
                          "properties": {
                            "dead_letter": {"type": "integer", "description": "Collector-generation dead-letter items. Owner: query.collectorGenerationDeadLettersToMap via DeadLetter."},
                            "replay_requested": {"type": "integer", "description": "Collector-generation items awaiting replay after a dead-lettered commit. Owner: query.collectorGenerationDeadLettersToMap via ReplayRequested."},
                            "replay_attempts": {"type": "integer", "description": "Collector-generation replay attempts. Owner: query.collectorGenerationDeadLettersToMap via ReplayAttempts."},
                            "oldest_dead_letter_age": {"type": "number", "description": "Age in seconds of the oldest dead-letter item. Owner: query.collectorGenerationDeadLettersToMap via OldestDeadLetterAge."},
                            "oldest_dead_letter_age_ms": {"type": "integer", "description": "Age in whole milliseconds of the oldest dead-letter item. Owner: query.collectorGenerationDeadLettersToMap via OldestDeadLetterAge."}
                          }
                        },
                        "latest_failure": {
                          "type": "object",
                          "nullable": true,
                          "description": "Newest dead-letter failure. Null when no failure exists so an all-empty object never reads as a failure record; raw work-item, scope, and generation ids are withheld from scoped callers. Owner: query.operatorDeadLettersToMap.",
                          "properties": {
                            "failure_class": {"type": "string", "description": "Machine failure class of the newest dead letter. Owner: query.operatorDeadLettersToMap via LatestFailureClass."},
                            "domain": {"type": "string", "description": "Domain of the newest dead letter. Owner: query.operatorDeadLettersToMap via LatestFailureDomain."},
                            "at": {"type": ["string", "null"], "format": "date-time", "description": "Failure time, null when the failure carries no timestamp. Owner: query.operatorDeadLettersToMap via LatestFailureAt."},
                            "work_item_id": {"type": "string", "description": "Failed work-item id. Withheld from scoped callers. Owner: query.operatorDeadLettersToMap via LatestFailureWorkItemID."},
                            "scope_id": {"type": "string", "description": "Failed scope id. Withheld from scoped callers. Owner: query.operatorDeadLettersToMap via LatestFailureScopeID."},
                            "generation_id": {"type": "string", "description": "Failed generation id. Withheld from scoped callers. Owner: query.operatorDeadLettersToMap via LatestFailureGenerationID."}
                          }
                        }
                      }
                    },
                    "retry_policies": {
                      "type": "array",
                      "description": "Per-stage projection retry policies. Owner: query.retryPoliciesToSlice.",
                      "items": {
                        "type": "object",
                        "description": "One stage retry policy. Owner: query.retryPoliciesToSlice via status.RetryPolicySummary.",
                        "properties": {
                          "stage": {"type": "string", "description": "Pipeline stage name. Owner: query.retryPoliciesToSlice via Stage."},
                          "max_attempts": {"type": "integer", "description": "Maximum claim attempts for the stage. Owner: query.retryPoliciesToSlice via MaxAttempts."},
                          "retry_delay": {"type": "string", "description": "Go duration string of the retry delay. Owner: query.retryPoliciesToSlice via RetryDelay."},
                          "retry_delay_ms": {"type": "integer", "description": "Retry delay in whole milliseconds. Owner: query.retryPoliciesToSlice via RetryDelay."},
                          "retry_delay_seconds": {"type": "number", "description": "Retry delay in seconds. Owner: query.retryPoliciesToSlice via RetryDelay."}
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
