// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsAuthAdminReads documents the tenant-scoped admin identity read
// endpoints (console admin UX, #3462 criterion #4). Every route requires
// all-scope admin authentication, reads strictly within the caller's tenant, and
// returns metadata only — never a secret, hash, invite code, credential handle,
// or external group hash.
const openAPIPathsAuthAdminReads = `
    "/api/v0/auth/admin/role-assignments": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's membership-role assignments",
        "description": "All-scopes admin route that lists membership-role assignments within the caller's own tenant/workspace, optionally filtered by user_id: user id, role id, assignment source, status, and effective/expiry timestamps. Returns no hashed secret.",
        "operationId": "listAdminRoleAssignments",
        "parameters": [{"name": "user_id", "in": "query", "required": false, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The tenant's membership-role assignments.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "role_assignments": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "user_id": {"type": "string"},
                          "role_id": {"type": "string"},
                          "assignment_source": {"type": "string"},
                          "status": {"type": "string"},
                          "effective_at": {"type": "string", "format": "date-time"},
                          "expires_at": {"type": "string", "format": "date-time"},
                          "tenant_id": {"type": "string"},
                          "workspace_id": {"type": "string"}
                        }
                      }
                    }
                  }
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
                  "properties": {
                    "user_id": {"type": "string"},
                    "role_id": {"type": "string"},
                    "status": {"type": "string"},
                    "changed": {"type": "boolean"}
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
    "/api/v0/auth/admin/roles": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's roles and grants",
        "description": "All-scopes admin route that lists the caller's own tenant's roles and the capability grants each role confers: role id, status, built-in flag, and per-grant action/feature/data class/scope class/status. Returns no role key hash, policy revision hash, or hashed scope selector.",
        "operationId": "listAdminRoles",
        "responses": {
          "200": {
            "description": "The tenant's roles and grants.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "roles": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "role_id": {"type": "string"},
                          "status": {"type": "string"},
                          "built_in": {"type": "boolean"},
                          "grants": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "grant_id": {"type": "string"},
                                "action": {"type": "string"},
                                "feature": {"type": "string"},
                                "data_class": {"type": "string"},
                                "scope_class": {"type": "string"},
                                "status": {"type": "string"}
                              }
                            }
                          }
                        }
                      }
                    }
                  }
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
        "responses": {
          "200": {
            "description": "The tenant's configured identity providers.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "providers": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "provider_config_id": {"type": "string"},
                          "provider_kind": {"type": "string"},
                          "status": {"type": "string"}
                        }
                      }
                    }
                  }
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
        "description": "All-scopes admin route that lists the caller's own tenant/workspace external group-to-role mappings: an opaque mapping reference, provider config id, role id, status, and effective/expiry timestamps. Never returns the external group hash (the hashed group-name secret).",
        "operationId": "listAdminIdPGroupMappings",
        "responses": {
          "200": {
            "description": "The tenant's IdP group-to-role mappings.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "group_mappings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "mapping_ref": {"type": "string"},
                          "provider_config_id": {"type": "string"},
                          "role_id": {"type": "string"},
                          "status": {"type": "string"},
                          "effective_at": {"type": "string", "format": "date-time"},
                          "expires_at": {"type": "string", "format": "date-time"},
                          "tenant_id": {"type": "string"},
                          "workspace_id": {"type": "string"}
                        }
                      }
                    }
                  }
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
        "summary": "Create an IdP group to role mapping",
        "description": "All-scopes admin route that idempotently activates an external group to role mapping within the caller's own tenant/workspace. The provider config and role must exist and be active in the tenant. The raw external_group name is hashed server-side with the same hash the OIDC login path uses to read mappings and is never stored or returned; only the opaque mapping_ref (an md5 over the composite key) is returned. Optional workspace_id must match the caller's workspace. Emits a governance audit event.",
        "operationId": "createAdminIdPGroupMapping",
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
                  "properties": {
                    "mapping_ref": {"type": "string"},
                    "provider_config_id": {"type": "string"},
                    "role_id": {"type": "string"},
                    "status": {"type": "string"},
                    "created": {"type": "boolean"}
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
    "/api/v0/auth/admin/api-tokens": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's generated API tokens",
        "description": "All-scopes admin route that lists every user's generated API tokens within the caller's own tenant/workspace: token id, class, owning user or service principal, status, and issued/expires/revoked timestamps. Never returns the token hash or display label hash. Distinct from the self-scoped GET /api/v0/auth/local/api-tokens.",
        "operationId": "listAdminAPITokens",
        "responses": {
          "200": {
            "description": "The tenant's generated API tokens.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "tokens": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "token_id": {"type": "string"},
                          "token_class": {"type": "string"},
                          "user_id": {"type": "string"},
                          "service_principal_id": {"type": "string"},
                          "status": {"type": "string"},
                          "issued_at": {"type": "string", "format": "date-time"},
                          "expires_at": {"type": "string", "format": "date-time"},
                          "revoked_at": {"type": "string", "format": "date-time"},
                          "tenant_id": {"type": "string"},
                          "workspace_id": {"type": "string"}
                        }
                      }
                    }
                  }
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
        "description": "All-scopes admin route that lists governance audit events for the caller's tenant context, filtered by event_type, decision, reason_code, and occurred_after/occurred_before, bounded by limit. Returns only audit-safe fields: event type, actor class, scope class, decision, reason code, occurred-at, and optional service principal id and correlation id. Never returns actor, scope, or policy revision hashes.",
        "operationId": "listAdminAuditEvents",
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
                  "properties": {
                    "events": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "event_type": {"type": "string"},
                          "actor_class": {"type": "string"},
                          "scope_class": {"type": "string"},
                          "decision": {"type": "string"},
                          "reason_code": {"type": "string"},
                          "occurred_at": {"type": "string", "format": "date-time"},
                          "service_principal_id": {"type": "string"},
                          "correlation_id": {"type": "string"}
                        }
                      }
                    }
                  }
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
    "/api/v0/auth/admin/audit/summary": {
      "get": {
        "tags": ["auth"],
        "summary": "Summarize governance audit events",
        "description": "All-scopes admin route that returns aggregate-only governance audit counts: total, allowed, denied, unavailable, last-occurred-at, and low-cardinality counts by event type, decision, reason, actor class, and scope class. Returns no individual event identifiers.",
        "operationId": "summarizeAdminAuditEvents",
        "responses": {
          "200": {
            "description": "Aggregate governance audit counts.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total": {"type": "integer"},
                    "allowed": {"type": "integer"},
                    "denied": {"type": "integer"},
                    "unavailable": {"type": "integer"},
                    "last_occurred_at": {"type": "string", "format": "date-time"},
                    "event_type_counts": {"$ref": "#/components/schemas/AdminAuditCountList"},
                    "decision_counts": {"$ref": "#/components/schemas/AdminAuditCountList"},
                    "reason_counts": {"$ref": "#/components/schemas/AdminAuditCountList"},
                    "actor_class_counts": {"$ref": "#/components/schemas/AdminAuditCountList"},
                    "scope_class_counts": {"$ref": "#/components/schemas/AdminAuditCountList"}
                  }
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
