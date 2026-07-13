// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsFreshnessCausality documents the freshness causality read model
// endpoint. The response explains stale answers by closed cause, summarizes the
// generation lifecycle including retired generations, and reports pending
// projection work. Scoped tokens receive the same aggregate counts with raw
// scope/generation identifiers withheld from transitions.
const openAPIPathsFreshnessCausality = `
    "/api/v0/status/freshness-causality": {
      "get": {
        "tags": ["status"],
        "summary": "Get freshness causality read model",
        "description": "Returns why answers are stale by closed FreshnessCause (pending_repo_generation, reducer_backlog, dead_lettered_domain, missing_collector_completion, and the per-answer content_coverage_unavailable, unsupported_profile, retention_expired classes), each with runtime/per-answer observability and a bounded next-check. Also summarizes the generation lifecycle including retired (superseded) generations and pending projection work. Scoped tokens receive the same aggregate counts with raw scope/generation identifiers withheld from transitions.",
        "operationId": "getFreshnessCausality",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Freshness causality read model",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "as_of": {"type": "string", "format": "date-time"},
                    "scoped": {"type": "boolean"},
                    "state": {"type": "string", "enum": ["fresh", "building", "stale"]},
                    "causes": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "cause": {"type": "string"},
                          "observed": {"type": "boolean"},
                          "observability": {"type": "string", "enum": ["runtime", "per_answer"]},
                          "detail": {"type": "string"},
                          "next_check": {"type": "object"}
                        }
                      }
                    },
                    "generations": {"type": "object"},
                    "pending_projection": {"type": "object"},
                    "recent_transitions": {"type": "array", "items": {"type": "object"}}
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
