// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsFreshnessGenerations = `
    "/api/v0/freshness/generations": {
      "get": {
        "tags": ["freshness"],
        "summary": "Drill into scope generation lifecycle history",
        "description": "Returns a bounded, ordered page of scope generation lifecycle rows joined with their owning scope identity, the per-generation fact_work_items queue status, and the latest per-generation failure. Filter by scope id, repository, collector kind, source system, generation id, or status. A named scope/repository/generation selector that matches nothing returns an explicit not-found instead of an empty list. Generation lifecycle is durable persisted truth, not graph-materialized correlation.",
        "operationId": "listGenerationLifecycle",
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
                  "properties": {
                    "generations": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "scope_kind": {"type": "string"},
                          "source_system": {"type": "string"},
                          "collector_kind": {"type": "string"},
                          "current_active_generation_id": {"type": "string"},
                          "is_active": {"type": "boolean"},
                          "trigger_kind": {"type": "string"},
                          "freshness_hint": {"type": "string"},
                          "status": {"type": "string", "enum": ["pending", "active", "superseded", "completed", "failed"]},
                          "observed_at": {"type": "string"},
                          "ingested_at": {"type": "string"},
                          "activated_at": {"type": "string"},
                          "superseded_at": {"type": "string"},
                          "queue_status": {
                            "type": "object",
                            "properties": {
                              "total": {"type": "integer"},
                              "outstanding": {"type": "integer"},
                              "in_flight": {"type": "integer"},
                              "retrying": {"type": "integer"},
                              "succeeded": {"type": "integer"},
                              "failed": {"type": "integer"},
                              "dead_letter": {"type": "integer"}
                            },
                            "required": ["total", "outstanding", "in_flight", "retrying", "succeeded", "failed", "dead_letter"]
                          },
                          "latest_failure": {
                            "type": "object",
                            "properties": {
                              "failure_class": {"type": "string"},
                              "failure_message": {"type": "string"},
                              "work_item_status": {"type": "string"},
                              "observed_at": {"type": "string"}
                            },
                            "required": ["failure_class"]
                          }
                        },
                        "required": ["scope_id", "generation_id", "scope_kind", "source_system", "collector_kind", "is_active", "trigger_kind", "status", "queue_status"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  },
                  "required": ["generations", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
