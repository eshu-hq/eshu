// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// CollectorTerminalFailureAcknowledgement is the OpenAPI path fragment
// documenting POST /api/v0/admin/collector-failures/acknowledge and POST
// /api/v0/admin/collector-failures/acknowledgements/query, and POST
// /api/v0/admin/collector-failures/query (#8830). It lives
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
        "description": "Marks the newest unresolved terminal failure of one collector instance and scope as known and expected (#8830). The failure must be inside the instance's window (3 x scan_interval, 30 minute floor) with no later completed work item for the scope; otherwise the call answers 422 ack_refused_no_terminal_failure and writes nothing. A refusal before the idempotency claim leaves the key unconsumed; the same refusal from the re-resolve inside the acknowledgement transaction (a concurrent resolution) consumes the key, and a retry with the same key answers 409 until a fresh idempotency_key is used; the same holds for a 404 from that re-resolve (the instance was disabled or deactivated after the first check), whose body keeps the standard error and detail and adds key_consumed true and a key_detail saying a retry needs a new idempotency_key. The acknowledgement covers that failure and later failures on the same scope with the same raw failure class; a completed work item after the acknowledged failure (a recovery) or a later failure with a different raw class is reported again with no further write. An acknowledged instance reads health at least partial (a stale vulnerability source or a degraded failure on the same instance keeps the higher health) and promotion_state partial with the blocker (terminal fetch failure acknowledged: <class> (<operation>)), never healthy or implemented; the acknowledgement does not expire. Requires an admin (all-scopes) token; a scoped token receives 403. reason and idempotency_key are mandatory; a repeat of a completed key with the same instance and scope answers 200 with status duplicate and the stored outcome, and the same key with another target answers 409. Every decision is recorded as a governance audit event carrying the bounded reason code and hashed actor, never the reason text. The reason is stored and returned only by the acknowledgements query route; no status surface carries it. A 503 means acknowledgement is not configured on this API instance. No 5xx body carries backend error text.",
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
                  "scope_id": {"type": "string", "minLength": 1, "description": "The failed scope. Find it with POST /api/v0/admin/collector-failures/query, which lists the failed scopes of one instance."},
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
          "404": {
            "description": "The instance is unknown, disabled, or deactivated, or the collector_kind does not match. Before the idempotency claim the key is not consumed. A 404 from the re-resolve inside the acknowledgement transaction (the instance was disabled or deactivated after the first check) consumes the key: the body then carries key_consumed true and key_detail, and a retry needs a new idempotency_key.",
            "content": {
              "application/json": {
                "schema": {
                  "allOf": [
                    {"$ref": "#/components/schemas/ErrorResponse"},
                    {
                      "type": "object",
                      "properties": {
                        "key_consumed": {"type": "boolean", "description": "Present and true only when the refusal came after the idempotency claim."},
                        "key_detail": {"type": "string", "description": "Present only with key_consumed: nothing was acknowledged and a retry needs a new idempotency_key."}
                      }
                    }
                  ]
                }
              }
            }
          },
          "409": {"$ref": "#/components/responses/Conflict"},
          "422": {"description": "ack_refused_no_terminal_failure: nothing to acknowledge on that instance and scope inside the window. Nothing was written. Before the idempotency claim the key was not consumed; from the re-resolve inside the acknowledgement transaction the key is consumed and a retry needs a new idempotency_key (the detail says which)."},
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
    "/api/v0/admin/collector-failures/query": {
      "post": {
        "tags": ["admin"],
        "summary": "List the failed scopes of one collector instance",
        "description": "Admin-only discovery read for acknowledgement (#8830): the unresolved terminal failures of ONE collector instance, per scope, with the scope ids the acknowledge route accepts. No status surface carries a scope id, because it names an org, repository, or account and the status payloads reach callers outside the admin gate. It lists exactly what collector status counts: the same instance window (3 x scan_interval, 30 minute floor), the same rule that a later completed work item resolves a failure, and the same acknowledgement cover. A scope appears once per state: the newest unacknowledged failure and the newest acknowledged one, so a scope whose older failure is acknowledged and whose newer failure has another class lists both. A scope that recovered or aged out is not listed. limit counts scopes (default 100, at most 500) and cursor is the next_cursor of the previous page; items are ordered by scope_id, unacknowledged before acknowledged. Only bounded tokens are returned: never a failure message, a provider status code, or an acknowledgement reason (read the reason with the acknowledgements query route). An unknown, kind-mismatched, disabled, or deactivated instance lists nothing. Requires an admin (all-scopes) token; a scoped token or a browser session without all scopes receives 403. It is a read: no audit event and no idempotency key. No 5xx body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["collector_kind", "instance_id"],
                "properties": {
                  "collector_kind": {"type": "string", "minLength": 1},
                  "instance_id": {"type": "string", "minLength": 1},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 500, "description": "Scopes per page; default 100."},
                  "cursor": {"type": "string", "description": "The next_cursor of the previous page."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "One page of failed scopes.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "count": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "string", "description": "Present only when truncated."},
                    "items": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "scope_id": {"type": "string"},
                          "failure_class": {"type": "string"},
                          "failure_operation": {"type": "string"},
                          "failed_at": {"type": "string", "format": "date-time"},
                          "work_item_id": {"type": "string"},
                          "acknowledged": {"type": "boolean"},
                          "acknowledged_at": {"type": "string", "format": "date-time", "description": "Present only when acknowledged."}
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
