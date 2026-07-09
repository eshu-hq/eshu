// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsAuthSignInPolicy documents the tenant sign-in policy routes
// (#4968, epic #4962): a public pre-auth GET the login page uses to decide
// whether to hide the local password form, and admin GET/PATCH routes for
// the full policy. The PATCH route enforces the require_sso guardrail — it
// cannot be enabled without at least one provider config with a passing
// connection test AND at least one admin having signed in via SSO — and
// rejects with 400 when either is unproven. GET and PATCH share one
// path-key object per Eshu OpenAPI convention (a paths object cannot carry
// the same key twice) — see openapi_paths_auth_admin_mutations.go's doc
// comment for the same pattern.
const openAPIPathsAuthSignInPolicy = `
    "/api/v0/auth/sign-in-policy": {
      "get": {
        "tags": ["auth"],
        "summary": "Get the public require_sso flag for the login page",
        "description": "PUBLIC (pre-auth) route. Returns only require_sso, scoped by the required tenant_id query parameter; an absent tenant_id or a read failure both default to require_sso=false rather than a cross-tenant scan or an error. This is a UX hint only — the server enforces the require_sso gate in POST /api/v0/auth/local/login regardless of what this route returns.",
        "operationId": "getPublicSignInPolicy",
        "parameters": [{"name": "tenant_id", "in": "query", "required": false, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The public sign-in policy hint.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {"require_sso": {"type": "boolean"}}
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/auth/admin/sign-in-policy": {
      "get": {
        "tags": ["auth"],
        "summary": "Get the tenant's full sign-in policy",
        "description": "All-scopes admin route. Includes SSO-admin-proof metadata (sso_admin_verified_at, sso_admin_verified_provider_config_id) used by the console to explain the require_sso guardrail state.",
        "operationId": "getAdminSignInPolicy",
        "responses": {
          "200": {
            "description": "The tenant's sign-in policy.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminSignInPolicy"}
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "patch": {
        "tags": ["auth"],
        "summary": "Update the tenant's sign-in policy",
        "description": "All-scopes admin route. Every field is optional; an absent field leaves the current value unchanged. Setting require_sso=true is guarded: it is rejected with 400 unless the tenant has at least one provider config with a passing connection test AND at least one admin has completed at least one SSO sign-in. Break-glass local admin sign-in (POST /api/v0/auth/local/login, evaluated the same way regardless of the console's /login?local=1 UI hint) always stays reachable regardless of require_sso, so this guardrail cannot lock a tenant out of its own dashboard. Emits a governance audit event for every allowed and denied attempt, including a guardrail rejection.",
        "operationId": "updateAdminSignInPolicy",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/AdminSignInPolicyUpdateRequest"}
            }
          }
        },
        "responses": {
          "200": {
            "description": "The updated sign-in policy.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/AdminSignInPolicy"}
              }
            }
          },
          "400": {"description": "Invalid request, or the require_sso guardrail rejected the change (no provider config has a passing connection test, or no admin has signed in via SSO yet)."},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
