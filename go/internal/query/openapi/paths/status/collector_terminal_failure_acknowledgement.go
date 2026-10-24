// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// CollectorTerminalFailureAcknowledgement is the OpenAPI path fragment
// documenting POST /api/v0/admin/collector-terminal-failure-acknowledgements
// (#8830). It lives apart from Admin because admin.go is near the 500-line
// cap (the repository_retirement.go precedent). openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handler in
// admin/acknowledgement and with
// docs/public/reference/http-api/collector-terminal-failure.md.
const CollectorTerminalFailureAcknowledgement = `
    "/api/v0/admin/collector-terminal-failure-acknowledgements": {
      "post": {
        "tags": ["admin"],
        "summary": "Acknowledge a collector terminal failure",
        "description": "Marks the newest unresolved terminal failure of one collector instance and scope as known and expected (#8830). Collector readiness then reports that instance as not failed, with last_failure_acknowledged set and the failure fields kept, instead of promotion_state failed. The acknowledgement covers that failure and the failures after it on the same scope with the same class, until a completed work item lands for the scope (a recovery) or a failure of another class appears; either reports the failure again with no further write. The caller names failure_class from collector readiness; a failure that changed class since is refused with 409 so an operator never acknowledges a failure they have not read. Repeating an acknowledgement a covering one already holds answers 200 with duplicate true and changes nothing, keeping the first reason and actor. Requires an admin (all-scopes) token; a scoped token receives 403. Every decision is recorded as a governance audit event carrying the bounded reason code and hashed actor, never the reason text. The reason is stored and shown on collector status to callers that may see instance identity, so it must not contain secrets. A 503 means acknowledgement is not configured on this API instance. No 5xx body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["instance_id", "scope_id", "failure_class", "reason"],
                "properties": {
                  "instance_id": {"type": "string", "minLength": 1, "description": "The collector instance, as reported by collector readiness."},
                  "scope_id": {"type": "string", "minLength": 1, "description": "The failed scope, from last_failure_scope_id on collector status or readiness."},
                  "failure_class": {"type": "string", "minLength": 1, "description": "The bounded last_failure_class read from collector readiness."},
                  "reason": {"type": "string", "minLength": 1, "maxLength": 512, "description": "Why the failure is known and expected."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "The acknowledgement, recorded now or already held.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "enum": ["acknowledged", "unchanged"]},
                    "duplicate": {"type": "boolean"},
                    "acknowledgement": {
                      "type": "object",
                      "properties": {
                        "collector_kind": {"type": "string"},
                        "instance_id": {"type": "string"},
                        "scope_id": {"type": "string"},
                        "failure_class": {"type": "string"},
                        "failure_at": {"type": "string", "format": "date-time"},
                        "acknowledged_at": {"type": "string", "format": "date-time"},
                        "reason": {"type": "string"}
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
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
