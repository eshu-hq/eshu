// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsAdmin = `
    "/api/v0/admin/refinalize": {
      "post": {
        "tags": ["admin"],
        "summary": "Refinalize scopes",
        "responses": {
          "200": {"description": "Refinalize request accepted"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/reindex": {
      "post": {
        "tags": ["admin"],
        "summary": "Request a reindex",
        "responses": {
          "202": {"description": "Reindex request accepted"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/recover-generations": {
      "post": {
        "tags": ["admin"],
        "summary": "Recover wedged generations",
        "description": "Operator escape hatch for generations that wedge active without advancing past canonical-nodes-committed. Durably re-enqueues projector work for the named scopes through the same Go work queue refinalize uses (re-driving reduce -> readiness -> projection over existing facts, no re-clone) and records the action in the admin_replay_requests ledger. Requires an explicit reason and idempotency_key and an admin (all-scopes) token. Duplicate delivery of the same idempotency_key returns the prior outcome (duplicate=true) instead of re-enqueuing.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["scope_ids", "reason", "idempotency_key"],
                "properties": {
                  "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes whose wedged active generations should be re-driven."},
                  "reason": {"type": "string", "description": "Why the recovery is safe."},
                  "idempotency_key": {"type": "string", "description": "Makes the recovery safe under retries and concurrent delivery."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Recovery request results (duplicate=true when an idempotent prior outcome is returned)"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"description": "Recovery requires an admin (all-scopes) token"},
          "409": {"description": "Idempotency key already in progress or reused with different scope_ids"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/shared-projection/tuning-report": {
      "get": {
        "tags": ["admin"],
        "summary": "Get shared projection tuning guidance",
        "responses": {
          "200": {"description": "Shared projection tuning report"}
        }
      }
    },
    "/api/v0/admin/work-items/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query fact work items",
        "responses": {
          "200": {"description": "Admin work-item query results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/decisions/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query projection decisions",
        "responses": {
          "200": {"description": "Projection decision query results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/dead-letters/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query dead-letter work items",
        "description": "Returns a bounded deterministic page of durable fact_work_items dead letters. Requires limit and timeout_ms, supports failure_class, domain, scope_id, collector_kind, and updated_at window filters, and returns truncated=true when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes.",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["limit", "timeout_ms"],
                "properties": {
                  "failure_class": {"type": "string"},
                  "domain": {"type": "string"},
                  "scope_id": {"type": "string"},
                  "collector_kind": {"type": "string"},
                  "updated_after": {"type": "string", "format": "date-time"},
                  "updated_before": {"type": "string", "format": "date-time"},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 500},
                  "timeout_ms": {"type": "integer", "minimum": 1, "maximum": 30000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Bounded dead-letter page with schema_version, limit, count, truncated, and items"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "504": {"description": "Dead-letter query timed out"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/dead-letter": {
      "post": {
        "tags": ["admin"],
        "summary": "Dead-letter work items",
        "responses": {
          "200": {"description": "Dead-letter request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/skip": {
      "post": {
        "tags": ["admin"],
        "summary": "Skip repository work items",
        "responses": {
          "200": {"description": "Skip request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/replay": {
      "post": {
        "tags": ["admin"],
        "summary": "Replay failed work items",
        "description": "Safely replays terminal work items. Requires an explicit reason and idempotency_key, an admin (all-scopes) token, and refuses unsafe failure classes (input_invalid, unsafe_payload, and the manual-review dead-letter triage classes projection_bug and resource_exhausted) unless force is set. Duplicate delivery of the same idempotency_key returns the prior outcome instead of replaying again.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reason", "idempotency_key"],
                "properties": {
                  "work_item_ids": {"type": "array", "items": {"type": "string"}},
                  "scope_id": {"type": "string"},
                  "stage": {"type": "string"},
                  "failure_class": {"type": "string"},
                  "operator_note": {"type": "string"},
                  "reason": {"type": "string", "description": "Why the replay is safe."},
                  "idempotency_key": {"type": "string", "description": "Makes the replay safe under retries and concurrent delivery."},
                  "force": {"type": "boolean", "description": "Replay unsafe failure classes after addressing the cause."},
                  "limit": {"type": "integer"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Replay request results (duplicate=true when an idempotent prior outcome is returned)"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"description": "Replay requires an admin (all-scopes) token"},
          "409": {"description": "Idempotency key already in progress or reused with different parameters"},
          "422": {"description": "Refused: unsafe failure class without force"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/backfill": {
      "post": {
        "tags": ["admin"],
        "summary": "Request a backfill",
        "responses": {
          "200": {"description": "Backfill request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/replay-events/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query replay events",
        "responses": {
          "200": {"description": "Replay-event query results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
