// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsGovernanceStatus = `
    "/api/v0/status/governance": {
      "get": {
        "tags": ["status"],
        "summary": "Get hosted governance status",
        "description": "Returns redacted hosted governance policy mode, state, readiness, and aggregate reason-code readbacks without raw policy bodies, subjects, tenants, source identifiers, credentials, provider endpoints, prompts, or provider responses.",
        "operationId": "getHostedGovernanceStatus",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Hosted governance status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "mode": {"type": "string", "enum": ["local_no_policy", "hosted_single_tenant", "hosted_multi_tenant", "unrecognized"], "description": "Configured governance posture. \"unrecognized\" is a readback state, not a settable value: ESHU_GOVERNANCE_MODE held something that is not one of supported_modes, and the deployment is fail-closed for all-scope callers on grant-bound routes."},
                    "state": {"type": "string", "enum": ["disabled", "partial", "enforcing", "stale", "invalid"]},
                    "source_kind": {"type": "string", "enum": ["environment", "kubernetes_secret", "config_map", "postgres_revision", "unknown"]},
                    "policy_revision_hash": {"type": "string"},
                    "readiness": {"type": "object"},
                    "identity": {"type": "object"},
                    "tenancy": {"type": "object"},
                    "egress": {"type": "object"},
                    "semantic": {"type": "object"},
                    "extensions": {"type": "object"},
                    "redaction": {"type": "object"},
                    "retention": {"type": "object"},
                    "audit": {
                      "type": "object",
                      "properties": {
                        "state": {"type": "string"},
                        "event_count": {"type": "integer", "minimum": 0},
                        "denied_decision_count": {"type": "integer", "minimum": 0},
                        "unavailable_decision_count": {"type": "integer", "minimum": 0},
                        "event_type_count": {"type": "integer", "minimum": 0},
                        "actor_class_count": {"type": "integer", "minimum": 0},
                        "scope_class_count": {"type": "integer", "minimum": 0},
                        "reason_count": {"type": "integer", "minimum": 0},
                        "acl_state_count": {"type": "integer", "minimum": 0}
                      }
                    },
                    "aggregates": {"type": "object"},
                    "reasons": {"type": "array", "items": {"type": "string"}},
                    "all_scope_route_policy": {
                      "type": "object",
                      "description": "Whether a tenant-bound all-scope credential is admitted on routes whose handlers filter reads by the caller's repository or scope grant. Derived from the same mode mapping the auth middleware applies.",
                      "properties": {
                        "grant_bound_routes": {"type": "string", "enum": ["admitted", "refused"]}
                      }
                    },
                    "supported_modes": {"type": "array", "items": {"type": "string"}},
                    "supported_states": {"type": "array", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
