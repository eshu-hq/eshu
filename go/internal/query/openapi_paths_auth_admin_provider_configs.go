// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsAuthAdminProviderConfigs documents the DB-backed identity
// provider-config CRUD endpoints (#4966, epic #4962). Every route requires
// all-scope admin authentication, is tenant-scoped, and emits a governance
// audit event on mutation. No route ever accepts a request body field that
// echoes back, or a response field that carries, a client secret or SAML
// signing key in plaintext — has_secret, secret_fingerprint, and key_id are
// the only secret-adjacent response fields. GET and POST/DELETE share one
// path-key object per Eshu OpenAPI convention (a paths object cannot carry
// the same key twice) — see openapi_paths_auth_admin_mutations.go's doc
// comment for the same pattern.
const openAPIPathsAuthAdminProviderConfigs = `
    "/api/v0/auth/admin/provider-configs": {
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's DB-backed and env-registered identity provider configs",
        "description": "All-scopes admin route. Merges DB-backed provider configs with env/file-registered providers (env-file authoritative): a DB row whose provider_config_id matches an env-registered provider is returned with shadowed_by_environment=true and its sealed secret is never consulted for login. Never returns a secret — only has_secret, secret_fingerprint, and key_id.",
        "operationId": "listAdminProviderConfigs",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "The tenant's provider configs.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "provider_configs": {
                      "type": "array",
                      "items": {"$ref": "#/components/schemas/AdminProviderConfig"}
                    },
                    "truncated": {"type": "boolean"}
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
        "summary": "Create a DB-backed identity provider config",
        "description": "All-scopes admin route that creates a new draft provider config with one active revision carrying the sealed secret. client_secret (oidc) or sp_private_key/sp_certificate (saml) are write-only: never echoed back, never logged. The provider cannot be enabled until it passes a test-connection call. Emits a governance audit event.",
        "operationId": "createAdminProviderConfig",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteRequest"}
            }
          }
        },
        "responses": {
          "200": {
            "description": "The created provider config's id and revision.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteResult"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "409": {"description": "A provider config already exists for this tenant, kind, and identity key."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/provider-configs/{provider_config_id}": {
      "get": {
        "tags": ["auth"],
        "summary": "Get one identity provider config",
        "description": "All-scopes admin route. Never returns a secret — only has_secret, secret_fingerprint, and key_id.",
        "operationId": "getAdminProviderConfig",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The provider config.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminProviderConfig"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "post": {
        "tags": ["auth"],
        "summary": "Update an identity provider config, creating a new active revision",
        "description": "All-scopes admin route. Every change creates a new audited revision superseding the current one. The full secret must be resupplied on every update — write-only secrets are never carried forward automatically (the AAD binds each sealed envelope to its own revision id specifically to prevent that). Emits a governance audit event.",
        "operationId": "updateAdminProviderConfig",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteRequest"}
            }
          }
        },
        "responses": {
          "200": {
            "description": "The updated provider config's new revision.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteResult"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/provider-configs/{provider_config_id}/revisions": {
      "get": {
        "tags": ["auth"],
        "summary": "List a provider config's revision history",
        "description": "All-scopes admin route. Never returns a secret — only has_secret per revision.",
        "operationId": "listAdminProviderConfigRevisions",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The provider config's revision history, newest first.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "revisions": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "revision_id": {"type": "string"},
                          "status": {"type": "string"},
                          "has_secret": {"type": "boolean"},
                          "created_at": {"type": "string", "format": "date-time"},
                          "activated_at": {"type": "string", "format": "date-time"},
                          "superseded_at": {"type": "string", "format": "date-time"}
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
    "/api/v0/auth/admin/provider-configs/{provider_config_id}/revert": {
      "post": {
        "tags": ["auth"],
        "summary": "Revert a provider config to a prior revision",
        "description": "All-scopes admin route. Activates a prior revision, restoring its sealed secret automatically (no secret is transmitted or re-entered). Idempotent: reverting to the already-active revision is a safe no-op. Emits a governance audit event.",
        "operationId": "revertAdminProviderConfig",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["revision_id"],
                "properties": {"revision_id": {"type": "string"}}
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "The reactivated revision.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteResult"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/provider-configs/{provider_config_id}/enable": {
      "post": {
        "tags": ["auth"],
        "summary": "Enable a provider config",
        "description": "All-scopes admin route. Re-runs a test-connection for the current active revision synchronously and only transitions the provider to active if it passes; a draft provider without a passing test cannot be enabled. For a login-capable provider kind (oidc, saml, github), also rejects if the stored configuration is missing a field required to resolve the provider for login (redirect_url for oidc/github; service_provider_entity_id, service_provider_acs_url, or inline metadata_xml for saml) even though those fields are optional at create/test-connection time. Emits a governance audit event.",
        "operationId": "enableAdminProviderConfig",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The provider config's new status.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteResult"}
              }
            }
          },
          "400": {"description": "The provider cannot be enabled: either the connection test did not pass, or the stored configuration is missing a field required to resolve the provider for login."},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/provider-configs/{provider_config_id}/disable": {
      "post": {
        "tags": ["auth"],
        "summary": "Disable a provider config",
        "description": "All-scopes admin route that transitions an active provider config back to draft. Idempotent. Emits a governance audit event.",
        "operationId": "disableAdminProviderConfig",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The provider config's new status.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminProviderConfigWriteResult"}
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection": {
      "post": {
        "tags": ["auth"],
        "summary": "Test a provider config's connection",
        "description": "All-scopes admin route that validates OIDC discovery/JWKS reachability (or SAML IdP metadata) and that the stored secret decrypts to well-formed material. Does not perform a live OAuth2 authorization-code round trip or a live SAML SSO exchange — see the oidclogin/samlauth TestConnection doc comments for the exact, bounded scope. Emits a governance audit event.",
        "operationId": "testAdminProviderConfigConnection",
        "x-scoped-token-support": true,
        "parameters": [{"name": "provider_config_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The test-connection result.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "provider_config_id": {"type": "string"},
                    "ok": {"type": "boolean"},
                    "detail": {"type": "string"}
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
