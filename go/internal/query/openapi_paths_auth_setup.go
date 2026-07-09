// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsAuthSetup documents the first-run setup wizard routes
// (epic #4962, issue #4965). Kept in its own file per
// go/internal/query/AGENTS.md's per-fragment review boundary, separate from
// openAPIPathsAuth (already at the 500-line file cap's documented exception).
const openAPIPathsAuthSetup = `
    "/api/v0/auth/setup-state": {
      "get": {
        "tags": ["auth"],
        "summary": "Report whether the first-run setup wizard is still reachable",
        "description": "Public pre-auth endpoint. needs_setup is true while a generated bootstrap admin credential (ESHU_AUTH_BOOTSTRAP_MODE=generated) remains unconsumed for this deployment; the console routes to the setup wizard instead of the login page when true. bootstrap_mode echoes ESHU_AUTH_BOOTSTRAP_MODE (generated, sso-only, disabled) for console messaging only — it does not itself gate the wizard.",
        "operationId": "getAuthSetupState",
        "security": [],
        "responses": {
          "200": {
            "description": "Current setup state.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "required": ["needs_setup", "bootstrap_mode"],
                  "properties": {
                    "needs_setup": {"type": "boolean"},
                    "bootstrap_mode": {"type": "string", "enum": ["generated", "sso-only", "disabled"]}
                  }
                }
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/setup/claim": {
      "post": {
        "tags": ["auth"],
        "summary": "Claim the instance with the generated one-time bootstrap credential (wizard step 1)",
        "description": "Public pre-auth endpoint scoped by possession of the generated one-time admin credential (same trust model as Jenkins initialAdminPassword — an exposed port cannot be seized by a drive-by visitor). Verifies username/password against the sealed bootstrap credential envelope without mutating any state. Returns 401 for a wrong or expired credential, pointing at the CLI recovery commands. Returns 410 once any identity exists — the wizard cannot be re-entered.",
        "operationId": "claimSetup",
        "security": [],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["username", "password"],
                "properties": {
                  "username": {"type": "string"},
                  "password": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Credential verified.",
            "content": {"application/json": {"schema": {"type": "object", "properties": {"status": {"type": "string", "enum": ["claimed"]}}}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "410": {"description": "Setup is no longer available; an identity already exists."},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/setup/admin": {
      "post": {
        "tags": ["auth"],
        "summary": "Create the first administrator by replacing the bootstrap password (wizard step 2)",
        "description": "Public pre-auth endpoint. Reproves the bootstrap credential (username/password) and rotates the bootstrap admin's password to new_password. The tenant and workspace are the fixed system-assigned bootstrap slot — no operator-invented IDs. The login username is not renameable through the wizard. Returns 410 once any identity exists.",
        "operationId": "createSetupAdmin",
        "security": [],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["username", "password", "new_password"],
                "properties": {
                  "username": {"type": "string"},
                  "password": {"type": "string"},
                  "new_password": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Administrator password set.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "enum": ["admin_created"]},
                    "tenant_id": {"type": "string"},
                    "workspace_id": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "410": {"description": "Setup is no longer available; an identity already exists."},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/setup/mfa": {
      "post": {
        "tags": ["auth"],
        "summary": "Enroll MFA recovery codes and complete first-run setup (wizard step 3)",
        "description": "Public pre-auth endpoint. Reproves the bootstrap credential, generates and persists a fresh set of MFA recovery codes, permanently consumes the bootstrap credential (sealing every setup route with 410 forever after), and issues a browser session so the operator lands logged in. The recovery codes are returned exactly once in this response and are never logged or persisted in clear text; only their hashes reach storage. The console must not navigate away from this step until the operator confirms the codes were saved.",
        "operationId": "completeSetupMFA",
        "security": [],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["username", "password"],
                "properties": {
                  "username": {"type": "string"},
                  "password": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Setup completed; browser session cookies set.",
            "headers": {
              "Set-Cookie": {
                "description": "Sets __Host-eshu_session and __Host-eshu_csrf with the same browser-session security attributes as local login.",
                "schema": {"type": "string"}
              }
            },
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "enum": ["completed"]},
                    "recovery_codes": {"type": "array", "items": {"type": "string"}, "description": "One-time plaintext recovery codes, returned exactly once."},
                    "auth": {"$ref": "#/components/schemas/BrowserSessionAuth"},
                    "csrf_token": {"type": "string"},
                    "idle_expires_at": {"type": "string", "format": "date-time"},
                    "absolute_expires_at": {"type": "string", "format": "date-time"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "410": {"description": "Setup is no longer available; an identity already exists."},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },`
