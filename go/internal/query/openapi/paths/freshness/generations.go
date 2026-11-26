// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

// Generations is the OpenAPI path fragment documenting the
// `/api/v0/freshness/generations` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Generations = `
    "/api/v0/freshness/generations": {
      "get": {
        "tags": ["freshness"],
        "summary": "Drill into scope generation lifecycle history",
        "description": "Returns a bounded, ordered page of scope generation lifecycle rows joined with their owning scope identity, the per-generation fact_work_items queue status, and the latest per-generation failure. Filter by scope id, repository, collector kind, source system, generation id, or status. A named scope/repository/generation selector that matches nothing returns an explicit not-found instead of an empty list. Generation lifecycle is durable persisted truth, not graph-materialized correlation. Scoped tokens receive only granted repositories and scopes; an ungranted selector returns not-found. An all-scope bearer token carries no grant for that filter to bind, so it is refused with a 403 under hosted_multi_tenant and under any unrecognized governance mode; local_no_policy, hosted_single_tenant, and an unset mode (which defaults to local_no_policy) admit it when it is bound to one tenant and workspace, and it then reads the whole corpus, as an admin credential does on every other route there.",
        "operationId": "listGenerationLifecycle",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Optional exact ingestion scope id."},
          {"name": "repository", "in": "query", "schema": {"type": "string"}, "description": "Optional canonical repository id (matches repository-kind scopes by source_key)."},
          {"name": "collector_kind", "in": "query", "schema": {"type": "string"}, "description": "Optional collector kind filter."},
          {"name": "source_system", "in": "query", "schema": {"type": "string"}, "description": "Optional source system filter."},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}, "description": "Optional exact generation id to drill into a single row."},
          {"name": "status", "in": "query", "schema": {"type": "string", "enum": ["pending", "active", "superseded", "completed", "failed"]}, "description": "Optional generation status filter."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 50}, "description": "Maximum generation lifecycle rows to return."}
        ],
        "responses": {
          "200": {
            "description": "Scope generation lifecycle rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bounded scope generation lifecycle page emitted by the generations handler.",
                  "properties": {
                    "generations": {
                      "type": "array",
                      "description": "Generation lifecycle rows joined with their owning scope identity, per the generations handler.",
                      "items": {
                        "type": "object",
                        "description": "One scope generation lifecycle row with fact_work_items queue status and the latest failure.",
                        "properties": {
                          "scope_id": {"type": "string", "description": "Ingestion scope the generation belongs to, per the generations handler."},
                          "generation_id": {"type": "string", "description": "Generation id, per the generations handler."},
                          "scope_kind": {"type": "string", "description": "Scope kind, per the generations handler."},
                          "source_system": {"type": "string", "description": "Source system that produced the generation, per the generations handler."},
                          "collector_kind": {"type": "string", "description": "Collector kind that produced the generation, per the generations handler."},
                          "current_active_generation_id": {"type": "string", "description": "Active generation id for the owning scope, per the generations handler."},
                          "is_active": {"type": "boolean", "description": "True when this row is the scope's active generation, per the generations handler."},
                          "trigger_kind": {"type": "string", "description": "What triggered the generation, per the generations handler."},
                          "freshness_hint": {"type": "string", "description": "Freshness hint for the generation, per the generations handler."},
                          "status": {"type": "string", "enum": ["pending", "active", "superseded", "completed", "failed"], "description": "Generation lifecycle status, per the generations handler."},
                          "observed_at": {"type": "string", "description": "When the generation was observed, per the generations handler."},
                          "ingested_at": {"type": "string", "description": "When the generation was ingested, per the generations handler."},
                          "activated_at": {"type": "string", "description": "When the generation activated, per the generations handler."},
                          "superseded_at": {"type": "string", "description": "When the generation was superseded, per the generations handler."},
                          "queue_status": {
                            "type": "object",
                            "description": "Per-generation fact_work_items queue status joined by the generations handler.",
                            "properties": {
                              "total": {"type": "integer", "description": "Total work items for the generation, per the generations handler."},
                              "outstanding": {"type": "integer", "description": "Outstanding work items for the generation, per the generations handler."},
                              "in_flight": {"type": "integer", "description": "In-flight work items for the generation, per the generations handler."},
                              "retrying": {"type": "integer", "description": "Retrying work items for the generation, per the generations handler."},
                              "succeeded": {"type": "integer", "description": "Succeeded work items for the generation, per the generations handler."},
                              "failed": {"type": "integer", "description": "Failed work items for the generation, per the generations handler."},
                              "dead_letter": {"type": "integer", "description": "Dead-lettered work items for the generation, per the generations handler."}
                            },
                            "required": ["total", "outstanding", "in_flight", "retrying", "succeeded", "failed", "dead_letter"]
                          },
                          "latest_failure": {
                            "type": "object",
                            "description": "Latest per-generation failure joined by the generations handler; absent when the generation never failed.",
                            "properties": {
                              "failure_class": {"type": "string", "description": "Failure class of the latest failure, per the generations handler."},
                              "failure_message": {"type": "string", "description": "Failure message of the latest failure, per the generations handler."},
                              "work_item_status": {"type": "string", "description": "Work item status carrying the failure, per the generations handler."},
                              "observed_at": {"type": "string", "description": "When the failure was observed, per the generations handler."},
                              "prior_failure": {
                                "type": "object",
                                "description": "The failure the newest failure row kept before a supersede, a stale-scope reclaim or an operator note rewrote its failure evidence. Absent when the row kept none. Its details text is not exposed.",
                                "properties": {
                                  "status": {"type": "string", "description": "Work item status of the prior failure, per the generations handler."},
                                  "failure_class": {"type": "string", "description": "Failure class of the prior failure, per the generations handler."},
                                  "failure_message": {"type": "string", "description": "Failure message of the prior failure, per the generations handler."},
                                  "updated_at": {"type": "string", "description": "When the prior failure was recorded, per the generations handler."}
                                }
                              }
                            },
                            "required": ["failure_class"]
                          }
                        },
                        "required": ["scope_id", "generation_id", "scope_kind", "source_system", "collector_kind", "is_active", "trigger_kind", "status", "queue_status"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page emitted by the generations handler."},
                    "limit": {"type": "integer", "description": "Page bound echoed by the generations handler."},
                    "truncated": {"type": "boolean", "description": "True when more lifecycle rows exist past this page, per the generations handler."}
                  },
                  "required": ["generations", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
