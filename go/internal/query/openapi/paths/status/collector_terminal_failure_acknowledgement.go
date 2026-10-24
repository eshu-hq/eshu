// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// CollectorTerminalFailureAcknowledgement is the OpenAPI path fragment
// documenting POST /api/v0/admin/collector-failures/acknowledge and POST
// /api/v0/admin/collector-failures/acknowledgements/query (#8830). It lives
// apart from Admin because admin.go is at the 500-line cap (the
// repository_retirement.go precedent). openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handler in
// admin/acknowledgement and with
// docs/public/reference/http-api/collector-terminal-failure.md.
const CollectorTerminalFailureAcknowledgement = `
    "/api/v0/admin/collector-failures/acknowledge": {
      "post": {
        "tags": ["admin"],
        "summary": "Acknowledge a collector terminal failure",
        "description": "Marks the newest unresolved terminal failure of one collector instance and scope as known and expected (#8830). The failure must be inside the instance's window (3 x scan_interval, 30 minute floor) with no later completed work item for the scope; otherwise the call answers 422 ack_refused_no_terminal_failure and consumes nothing. The acknowledgement covers that failure and later failures on the same scope with the same raw failure class; a completed work item after the acknowledged failure (a recovery) or a later failure with a different raw class is reported again with no further write. An acknowledged instance reads health partial and promotion_state partial with the blocker (terminal fetch failure acknowledged: <class> (<operation>)), never healthy or implemented; the acknowledgement does not expire. Requires an admin (all-scopes) token; a scoped token receives 403. reason and idempotency_key are mandatory; a repeat of a completed key with the same instance and scope answers 200 with status duplicate and the stored outcome, and the same key with another target answers 409. Every decision is recorded as a governance audit event carrying the bounded reason code and hashed actor, never the reason text. The reason is stored and returned only by the acknowledgements query route; no status surface carries it. A 503 means acknowledgement is not configured on this API instance. No 5xx body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["collector_kind", "instance_id", "scope_id", "reason", "idempotency_key"],
                "properties": {
                  "collector_kind": {"type": "string", "minLength": 1, "description": "The collector kind of the instance, for example security_alert."},
                  "instance_id": {"type": "string", "minLength": 1, "description": "The collector instance, as collector readiness reports it."},
                  "scope_id": {"type": "string", "minLength": 1, "description": "The failed scope. Find it with POST /api/v0/admin/work-items/query (statuses failed_terminal, scope_id)."},
                  "reason": {"type": "string", "minLength": 1, "maxLength": 512, "description": "Why the failure is known and expected. Stored as typed; do not put secrets in it."},
                  "idempotency_key": {"type": "string", "minLength": 1, "description": "Idempotency key; a retry with the same key and target replays the stored outcome."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "The acknowledgement, recorded now (status acknowledged) or replayed from an earlier completed call with the same key (status duplicate).",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "enum": ["acknowledged", "duplicate"]},
                    "idempotency_key": {"type": "string"},
                    "acknowledgement": {
                      "type": "object",
                      "properties": {
                        "collector_kind": {"type": "string"},
                        "instance_id": {"type": "string"},
                        "scope_id": {"type": "string"},
                        "work_item_id": {"type": "string"},
                        "failure_class": {"type": "string"},
                        "failure_operation": {"type": "string"},
                        "failed_at": {"type": "string", "format": "date-time"},
                        "acknowledged_at": {"type": "string", "format": "date-time"}
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "422": {"description": "ack_refused_no_terminal_failure: nothing to acknowledge on that instance and scope inside the window. Nothing was written and the idempotency key was not consumed."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/collector-failures/acknowledgements/query": {
      "post": {
        "tags": ["admin"],
        "summary": "List collector terminal failure acknowledgements",
        "description": "Admin-only read of stored acknowledgements (#8830), newest first, bounded by limit (default 100, at most 500). It is the only read that returns the operator's reason; collector status, collector readiness, the MCP tools, and the metrics never do. Requires an admin (all-scopes) token.",
        "requestBody": {
          "required": false,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "collector_kind": {"type": "string"},
                  "instance_id": {"type": "string"},
                  "scope_id": {"type": "string"},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 500}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Stored acknowledgements.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "count": {"type": "integer"},
                    "acknowledgements": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "collector_kind": {"type": "string"},
                          "instance_id": {"type": "string"},
                          "scope_id": {"type": "string"},
                          "failure_class": {"type": "string"},
                          "failed_at": {"type": "string", "format": "date-time"},
                          "work_item_id": {"type": "string"},
                          "reason": {"type": "string"},
                          "actor_class": {"type": "string"},
                          "correlation_id": {"type": "string"},
                          "acknowledged_at": {"type": "string", "format": "date-time"}
                        }
                      }
                    }
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
