// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

// Causality documents the freshness causality read model
// endpoint. The response explains stale answers by closed cause, summarizes the
// generation lifecycle including retired generations, and reports pending
// projection work. Scoped tokens receive the same aggregate counts with raw
// scope/generation identifiers withheld from transitions.
const Causality = `
    "/api/v0/status/freshness-causality": {
      "get": {
        "tags": ["status"],
        "summary": "Get freshness causality read model",
        "description": "Returns why answers are stale by closed FreshnessCause (pending_repo_generation, reducer_backlog, dead_lettered_domain, missing_collector_completion, and the per-answer content_coverage_unavailable, unsupported_profile, retention_expired classes), each with runtime/per-answer observability and a bounded next-check. Also summarizes the generation lifecycle including retired (superseded) generations and pending projection work. Scoped tokens receive the same aggregate counts with raw scope/generation identifiers withheld from transitions. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.freshness_causality_status.",
        "operationId": "getFreshnessCausality",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Freshness causality read model",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Freshness causality read model projected by freshnessCausalityToMap.",
                  "properties": {
                    "version": {"type": "string", "description": "Build version stamped by freshnessCausalityToMap."},
                    "as_of": {"type": "string", "format": "date-time", "description": "Snapshot instant the read model was projected at, per freshnessCausalityToMap."},
                    "scoped": {"type": "boolean", "description": "True when the caller is scoped (raw scope/generation identifiers withheld from transitions), per freshnessCausalityToMap."},
                    "state": {"type": "string", "enum": ["fresh", "building", "stale"], "description": "Aggregate freshness state projected by freshnessCausalityToMap."},
                    "causes": {
                      "type": "array",
                      "description": "Staleness causes by closed FreshnessCause (freshness.FreshnessCauseStatus) projected by freshnessCausesToSlice.",
                      "items": {
                        "type": "object",
                        "description": "One staleness cause with runtime/per-answer observability and a bounded next check.",
                        "properties": {
                          "cause": {"type": "string", "description": "Closed FreshnessCause value projected by freshnessCausesToSlice."},
                          "observed": {"type": "boolean", "description": "Whether the cause is currently observed, per freshnessCausesToSlice."},
                          "observability": {"type": "string", "enum": ["runtime", "per_answer"], "description": "Where the cause is observable, per freshnessCausesToSlice."},
                          "detail": {"type": "string", "description": "Human-readable cause detail projected by freshnessCausesToSlice."},
                          "next_check": {"type": "object", "description": "Bounded follow-up call that drills into the cause (answer.FreshnessNextCheckAsRecommendedCall): only the non-empty tool, route, reason, and params keys are emitted.",
                            "properties": {
                              "tool": {"type": "string", "description": "Tool that drills into the cause, per FreshnessNextCheckAsRecommendedCall."},
                              "route": {"type": "string", "description": "Route that drills into the cause, per FreshnessNextCheckAsRecommendedCall."},
                              "reason": {"type": "string", "description": "Why the check drills into the cause, per FreshnessNextCheckAsRecommendedCall."},
                              "params": {"type": "object", "description": "Check parameters keyed by parameter name, per FreshnessNextCheckAsRecommendedCall.", "additionalProperties": true}
                            }}
                        },
                        "required": ["cause", "observed", "observability", "detail", "next_check"]
                      }
                    },
                    "generations": {"type": "object", "description": "Generation lifecycle summary (freshness.Generations) projected by freshnessGenerationsToMap.",
                      "properties": {
                        "active": {"type": "integer", "description": "Active generations counted by freshnessGenerationsToMap."},
                        "pending": {"type": "integer", "description": "Pending generations counted by freshnessGenerationsToMap."},
                        "completed": {"type": "integer", "description": "Completed generations counted by freshnessGenerationsToMap."},
                        "superseded": {"type": "integer", "description": "Retired (superseded) generations counted by freshnessGenerationsToMap."},
                        "failed": {"type": "integer", "description": "Failed generations counted by freshnessGenerationsToMap."}
                      },
                      "required": ["active", "pending", "completed", "superseded", "failed"]
                    },
                    "pending_projection": {"type": "object", "description": "Projection work still owed before the graph catches up (freshness.PendingProjection), per freshnessPendingProjectionToMap.",
                      "properties": {
                        "outstanding": {"type": "integer", "description": "Outstanding projection rows counted by freshnessPendingProjectionToMap."},
                        "dead_letter": {"type": "integer", "description": "Dead-lettered projection rows counted by freshnessPendingProjectionToMap."},
                        "domains": {"type": "integer", "description": "Domains with owed projection work counted by freshnessPendingProjectionToMap."}
                      },
                      "required": ["outstanding", "dead_letter", "domains"]
                    },
                    "recent_transitions": {"type": "array", "description": "Recent generation lifecycle rows (freshness.Transition) projected by freshnessTransitionsToSlice; raw scope/generation identifiers are withheld from scoped callers.",
                      "items": {"type": "object", "description": "One lifecycle row: status stays visible while identifiers are withheld from scoped callers.",
                        "properties": {
                          "scope_id": {"type": "string", "description": "Scope the transition belongs to, per freshnessTransitionsToSlice; withheld from scoped callers."},
                          "generation_id": {"type": "string", "description": "Generation the transition belongs to, per freshnessTransitionsToSlice; withheld from scoped callers."},
                          "status": {"type": "string", "description": "Lifecycle status projected by freshnessTransitionsToSlice."},
                          "trigger_kind": {"type": "string", "description": "What triggered the generation, per freshnessTransitionsToSlice."},
                          "freshness_hint": {"type": "string", "description": "Freshness hint for the transition, per freshnessTransitionsToSlice."},
                          "observed_at": {"type": "string", "format": "date-time", "description": "When the generation was observed, per freshnessTransitionsToSlice."},
                          "superseded_at": {"type": "string", "format": "date-time", "description": "When the generation was superseded, per freshnessTransitionsToSlice."}
                        },
                        "required": ["status", "trigger_kind", "freshness_hint", "observed_at", "superseded_at"]
                      }},
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource", "description": "Active work source attached by withActiveWorkSource."}
                  },
                  "required": ["version", "as_of", "scoped", "state", "causes", "generations", "pending_projection", "recent_transitions", "active_work_source"]
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
