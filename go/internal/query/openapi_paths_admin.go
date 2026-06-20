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
        "description": "Safely replays terminal work items. Requires an explicit reason and idempotency_key, an admin (all-scopes) token, and refuses unsafe failure classes (input_invalid, unsafe_payload) unless force is set. Duplicate delivery of the same idempotency_key returns the prior outcome instead of replaying again.",
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
