// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

// AdminReads documents the tenant-scoped admin identity read
// endpoints (console admin UX, #3462 criterion #4). Every route requires
// all-scope admin authentication, reads strictly within the caller's tenant, and
// returns metadata only — never a secret, hash, invite code, credential handle,
// or external group hash.
const AdminReads = `
    "/api/v0/auth/admin/role-assignments": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's membership-role assignments",
        "description": "All-scopes admin route that lists membership-role assignments within the caller's own tenant/workspace, optionally filtered by user_id: user id, role id, assignment source, status, and effective/expiry timestamps. Returns no hashed secret.",
        "operationId": "listAdminRoleAssignments",
        "x-scoped-token-support": true,
        "parameters": [{"name": "user_id", "in": "query", "required": false, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The tenant's membership-role assignments.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Membership-role assignments page emitted by handleListRoleAssignments.",
                  "properties": {
                    "role_assignments": {
                      "type": "array",
                      "description": "Membership-role assignment rows projected by handleListRoleAssignments.",
                      "items": {
                        "type": "object",
                        "description": "One assignment row: user, role, source, status, and effective/expiry timestamps.",
                        "properties": {
                          "user_id": {"type": "string", "description": "Assigned user id projected by handleListRoleAssignments."},
                          "role_id": {"type": "string", "description": "Assigned role id projected by handleListRoleAssignments."},
                          "assignment_source": {"type": "string", "description": "How the assignment was created, per handleListRoleAssignments."},
                          "status": {"type": "string", "description": "Assignment status projected by handleListRoleAssignments."},
                          "effective_at": {"type": "string", "format": "date-time", "description": "When the assignment takes effect, per handleListRoleAssignments."},
                          "expires_at": {"type": "string", "format": "date-time", "description": "When the assignment expires, per handleListRoleAssignments; absent when the row carries none."},
                          "tenant_id": {"type": "string", "description": "Tenant id projected by handleListRoleAssignments."},
                          "workspace_id": {"type": "string", "description": "Workspace id projected by handleListRoleAssignments."}
                        },
                        "required": ["user_id", "role_id", "assignment_source", "status", "effective_at", "tenant_id", "workspace_id"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when the assignment page reached the list cap, per handleListRoleAssignments."}
                  },
                  "required": ["role_assignments", "truncated"]
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "post": {
        "tags": ["auth"],
        "summary": "Grant a membership-role assignment",
        "description": "All-scopes admin route that idempotently activates a membership-role assignment for a user within the caller's own tenant/workspace. The role must exist and be active in the tenant. A repeated grant converges on one row. Optional workspace_id must match the caller's workspace. Emits a governance audit event.",
        "operationId": "grantAdminRoleAssignment",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["user_id", "role_id"],
                "properties": {
                  "user_id": {"type": "string"},
                  "role_id": {"type": "string"},
                  "workspace_id": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "The granted assignment's state.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Grant outcome emitted by handleGrantRoleAssignment: the granted assignment's state.",
                  "properties": {
                    "user_id": {"type": "string", "description": "Granted user id echoed by handleGrantRoleAssignment."},
                    "role_id": {"type": "string", "description": "Granted role id echoed by handleGrantRoleAssignment."},
                    "status": {"type": "string", "description": "Effective assignment status reported by handleGrantRoleAssignment."},
                    "changed": {"type": "boolean", "description": "False when the grant reactivated an already-revoked row instead of inserting a fresh one, per handleGrantRoleAssignment."}
                  },
                  "required": ["user_id", "role_id", "status", "changed"]
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
    "/api/v0/auth/admin/roles": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's roles and grants",
        "description": "All-scopes admin route that lists the caller's own tenant's roles and the capability grants each role confers: role id, status, built-in flag, and per-grant action/feature/data class/scope class/status. Returns no role key hash, policy revision hash, or hashed scope selector.",
        "operationId": "listAdminRoles",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "The tenant's roles and grants.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Tenant roles and capability grants page emitted by handleListRoles.",
                  "properties": {
                    "roles": {
                      "type": "array",
                      "description": "Role rows projected by handleListRoles.",
                      "items": {
                        "type": "object",
                        "description": "One role row: id, status, built-in flag, and capability grants.",
                        "properties": {
                          "role_id": {"type": "string", "description": "Role id projected by handleListRoles."},
                          "status": {"type": "string", "description": "Role status projected by handleListRoles."},
                          "built_in": {"type": "boolean", "description": "Whether the role is built-in, per handleListRoles."},
                          "grants": {
                            "type": "array",
                            "description": "Capability grants the role confers, per handleListRoles.",
                            "items": {
                              "type": "object",
                              "description": "One capability grant: action, feature, data/scope class, and status.",
                              "properties": {
                                "grant_id": {"type": "string", "description": "Grant id projected by handleListRoles."},
                                "action": {"type": "string", "description": "Granted action projected by handleListRoles."},
                                "feature": {"type": "string", "description": "Granted feature projected by handleListRoles."},
                                "data_class": {"type": "string", "description": "Granted data class projected by handleListRoles."},
                                "scope_class": {"type": "string", "description": "Granted scope class projected by handleListRoles."},
                                "status": {"type": "string", "description": "Grant status projected by handleListRoles."}
                              },
                              "required": ["grant_id", "action", "feature", "data_class", "scope_class", "status"]
                            }
                          }
                        },
                        "required": ["role_id", "status", "built_in", "grants"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when the roles page or a role's grant set reached its cap, per handleListRoles."}
                  },
                  "required": ["roles", "truncated"]
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/idp-providers": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's configured identity providers",
        "description": "All-scopes admin route that lists the caller's own tenant's configured identity providers: provider config id, provider kind, and status only. Never returns issuer, metadata URL, entity id, or client id hashes, and never returns credential handles.",
        "operationId": "listAdminIdPProviders",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "The tenant's configured identity providers.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Configured identity providers page emitted by handleListIdPProviders.",
                  "properties": {
                    "providers": {
                      "type": "array",
                      "description": "Provider rows projected by handleListIdPProviders.",
                      "items": {
                        "type": "object",
                        "description": "One provider row: config id, kind, and status.",
                        "properties": {
                          "provider_config_id": {"type": "string", "description": "Provider config id projected by handleListIdPProviders."},
                          "provider_kind": {"type": "string", "description": "Provider kind projected by handleListIdPProviders."},
                          "status": {"type": "string", "description": "Provider status projected by handleListIdPProviders."}
                        },
                        "required": ["provider_config_id", "provider_kind", "status"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when the provider page reached the list cap, per handleListIdPProviders."}
                  },
                  "required": ["providers", "truncated"]
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/idp-group-mappings": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's IdP group-to-role mappings",
        "description": "All-scopes admin route that lists up to 500 of the caller's own tenant/workspace external group-to-role mappings per page, ordered by opaque mapping_ref. Follow next_after_ref as after_ref until the page is empty or truncated is false. Never returns the external group hash (the hashed group-name secret).",
        "operationId": "listAdminIdPGroupMappings",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "after_ref", "in": "query", "schema": {"type": "string", "pattern": "^[0-9a-f]{64}$"}, "description": "Optional opaque mapping_ref from the previous page's next_after_ref, read without normalization; omit it for the first page. A present value that is not a lowercase 64-hex ref (including an empty, padded, or repeated one) and an undecodable query string return 400."}
        ],
        "responses": {
          "200": {
            "description": "The tenant's IdP group-to-role mappings.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "IdP group-to-role mappings page emitted by handleListIdPGroupMappings.",
                  "properties": {
                    "group_mappings": {
                      "type": "array",
                      "description": "Mapping rows projected by handleListIdPGroupMappings, ordered by opaque mapping_ref.",
                      "items": {
                        "type": "object",
                        "description": "One mapping row addressed by its opaque mapping_ref.",
                        "properties": {
                          "mapping_ref": {"type": "string", "description": "Opaque mapping ref projected by handleListIdPGroupMappings."},
                          "provider_config_id": {"type": "string", "description": "Provider config id projected by handleListIdPGroupMappings."},
                          "role_id": {"type": "string", "description": "Role id projected by handleListIdPGroupMappings."},
                          "status": {"type": "string", "description": "Mapping status projected by handleListIdPGroupMappings."},
                          "effective_at": {"type": "string", "format": "date-time", "description": "When the mapping takes effect, per handleListIdPGroupMappings."},
                          "expires_at": {"type": "string", "format": "date-time", "description": "When the mapping expires, per handleListIdPGroupMappings; absent when the row carries none."},
                          "tenant_id": {"type": "string", "description": "Tenant id projected by handleListIdPGroupMappings."},
                          "workspace_id": {"type": "string", "description": "Workspace id projected by handleListIdPGroupMappings."}
                        },
                        "required": ["mapping_ref", "provider_config_id", "role_id", "status", "effective_at", "tenant_id", "workspace_id"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when this page reaches the 500-row cap. A subsequent page may be empty."},
                    "next_after_ref": {"type": "string", "description": "Last mapping_ref on a full page; empty when fewer than 500 rows were returned."}
                  },
                  "required": ["group_mappings", "truncated", "next_after_ref"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "post": {
        "tags": ["auth"],
        "summary": "Create an IdP group to role mapping",
        "description": "All-scopes admin route that idempotently activates an external group to role mapping within the caller's own tenant/workspace. The provider config and role must exist and be active in the tenant. The raw external_group name is hashed server-side with the same hash the OIDC login path uses to read mappings and is never stored or returned; only the opaque 64-hex SHA-256 mapping_ref over the composite key is returned. Optional workspace_id must match the caller's workspace. Emits a governance audit event.",
        "operationId": "createAdminIdPGroupMapping",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["provider_config_id", "external_group", "role_id"],
                "properties": {
                  "provider_config_id": {"type": "string"},
                  "external_group": {"type": "string", "description": "Raw external group name; hashed server-side and never stored or returned in clear."},
                  "role_id": {"type": "string"},
                  "workspace_id": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "The created or activated mapping, addressed by its opaque mapping_ref.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Created mapping outcome emitted by handleCreateIdPGroupMapping.",
                  "properties": {
                    "mapping_ref": {"type": "string", "description": "Opaque mapping ref of the created mapping, per handleCreateIdPGroupMapping."},
                    "provider_config_id": {"type": "string", "description": "Provider config id echoed by handleCreateIdPGroupMapping."},
                    "role_id": {"type": "string", "description": "Role id echoed by handleCreateIdPGroupMapping."},
                    "status": {"type": "string", "description": "Effective mapping status reported by handleCreateIdPGroupMapping."},
                    "created": {"type": "boolean", "description": "False when the create reactivated an existing mapping, per handleCreateIdPGroupMapping."}
                  },
                  "required": ["mapping_ref", "provider_config_id", "role_id", "status", "created"]
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
    "/api/v0/auth/admin/api-tokens": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's generated API tokens",
        "description": "All-scopes admin route that lists every user's generated API tokens within the caller's own tenant/workspace: token id, class, owning user or service principal, status, display_label, and issued/expires/revoked timestamps. Never returns the token hash or display label hash. Distinct from the self-scoped GET /api/v0/auth/local/api-tokens.",
        "operationId": "listAdminAPITokens",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "The tenant's generated API tokens.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Tenant API tokens page emitted by handleListAPITokens.",
                  "properties": {
                    "tokens": {
                      "type": "array",
                      "description": "Token rows projected by handleListAPITokens.",
                      "items": {
                        "type": "object",
                        "description": "One token row: id, class, owner, status, label, and lifecycle timestamps.",
                        "properties": {
                          "token_id": {"type": "string", "description": "Token id projected by handleListAPITokens."},
                          "token_class": {"type": "string", "description": "Token class projected by handleListAPITokens."},
                          "user_id": {"type": "string", "description": "Owning user id projected by handleListAPITokens; absent for service-principal tokens."},
                          "service_principal_id": {"type": "string", "description": "Owning service principal id projected by handleListAPITokens; absent for user tokens."},
                          "status": {"type": "string", "description": "Token status projected by handleListAPITokens."},
                          "display_label": {"type": "string", "description": "Present only when the token was created with a label."},
                          "issued_at": {"type": "string", "format": "date-time", "description": "When the token was issued, per handleListAPITokens."},
                          "expires_at": {"type": "string", "format": "date-time", "description": "When the token expires, per handleListAPITokens; absent when the row carries none."},
                          "revoked_at": {"type": "string", "format": "date-time", "description": "When the token was revoked, per handleListAPITokens; absent when the row carries none."},
                          "tenant_id": {"type": "string", "description": "Tenant id projected by handleListAPITokens."},
                          "workspace_id": {"type": "string", "description": "Workspace id projected by handleListAPITokens."}
                        },
                        "required": ["token_id", "token_class", "status", "issued_at", "tenant_id", "workspace_id"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when the token page reached the list cap, per handleListAPITokens."}
                  },
                  "required": ["tokens", "truncated"]
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/audit/events": {
      "get": {
        "tags": ["auth"],
        "summary": "List governance audit events",
        "description": "All-scopes admin route that lists governance audit events for the caller's tenant context, filtered by event_type, decision, reason_code, and occurred_after/occurred_before, bounded by limit. Returns only audit-safe fields: event type, actor class, scope class, decision, reason code, occurred-at, and optional service principal id and correlation id. Never returns actor, scope, or policy revision hashes. A 503 with Retry-After means a transient condition that is safe to retry: the governance audit read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the admin audit reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability audit.events.list.",
        "operationId": "listAdminAuditEvents",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "event_type", "in": "query", "required": false, "schema": {"type": "string"}},
          {"name": "decision", "in": "query", "required": false, "schema": {"type": "string"}},
          {"name": "reason_code", "in": "query", "required": false, "schema": {"type": "string"}},
          {"name": "occurred_after", "in": "query", "required": false, "schema": {"type": "string", "format": "date-time"}},
          {"name": "occurred_before", "in": "query", "required": false, "schema": {"type": "string", "format": "date-time"}},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "maximum": 500}}
        ],
        "responses": {
          "200": {
            "description": "Governance audit events.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Governance audit events page emitted by handleListAuditEvents.",
                  "properties": {
                    "events": {
                      "type": "array",
                      "description": "Audit-safe event rows projected by auditEventJSON (hashes omitted).",
                      "items": {
                        "type": "object",
                        "description": "One audit event: type, actor/scope class, decision, reason, and occurred-at.",
                        "properties": {
                          "event_type": {"type": "string", "description": "Event type projected by auditEventJSON."},
                          "actor_class": {"type": "string", "description": "Actor class projected by auditEventJSON."},
                          "scope_class": {"type": "string", "description": "Scope class projected by auditEventJSON."},
                          "decision": {"type": "string", "description": "Decision projected by auditEventJSON."},
                          "reason_code": {"type": "string", "description": "Reason code projected by auditEventJSON."},
                          "occurred_at": {"type": "string", "format": "date-time", "description": "When the event occurred, per auditEventJSON."},
                          "service_principal_id": {"type": "string", "description": "Service principal id projected by auditEventJSON; absent when the event carries none."},
                          "correlation_id": {"type": "string", "description": "Correlation id projected by auditEventJSON; absent when the event carries none."}
                        },
                        "required": ["event_type", "actor_class", "scope_class", "decision", "reason_code", "occurred_at"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when the event page reached the effective limit, per handleListAuditEvents."}
                  },
                  "required": ["events", "truncated"]
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
    "/api/v0/auth/admin/audit/summary": {
      "get": {
        "tags": ["auth"],
        "summary": "Summarize governance audit events",
        "description": "All-scopes admin route that returns aggregate-only governance audit counts: total, allowed, denied, unavailable, last-occurred-at, and low-cardinality counts by event type, decision, reason, actor class, and scope class. Returns no individual event identifiers. A 503 with Retry-After means a transient condition that is safe to retry: the governance audit read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the admin audit reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability audit.summary.read.",
        "operationId": "summarizeAdminAuditEvents",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Aggregate governance audit counts.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Aggregate governance audit counts emitted by auditSummaryJSON.",
                  "properties": {
                    "total": {"type": "integer", "description": "Total audited events counted by auditSummaryJSON."},
                    "allowed": {"type": "integer", "description": "Allowed decisions counted by auditSummaryJSON."},
                    "denied": {"type": "integer", "description": "Denied decisions counted by auditSummaryJSON."},
                    "unavailable": {"type": "integer", "description": "Unavailable decisions counted by auditSummaryJSON."},
                    "last_occurred_at": {"type": "string", "format": "date-time", "description": "Latest event instant reported by auditSummaryJSON."},
                    "event_type_counts": {"$ref": "#/components/schemas/AdminAuditCountList", "description": "Low-cardinality counts by event type, per auditSummaryJSON."},
                    "decision_counts": {"$ref": "#/components/schemas/AdminAuditCountList", "description": "Low-cardinality counts by decision, per auditSummaryJSON."},
                    "reason_counts": {"$ref": "#/components/schemas/AdminAuditCountList", "description": "Low-cardinality counts by reason, per auditSummaryJSON."},
                    "actor_class_counts": {"$ref": "#/components/schemas/AdminAuditCountList", "description": "Low-cardinality counts by actor class, per auditSummaryJSON."},
                    "scope_class_counts": {"$ref": "#/components/schemas/AdminAuditCountList", "description": "Low-cardinality counts by scope class, per auditSummaryJSON."}
                  },
                  "required": ["total", "allowed", "denied", "unavailable", "last_occurred_at", "event_type_counts", "decision_counts", "reason_counts", "actor_class_counts", "scope_class_counts"]
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
