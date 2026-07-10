// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:filelength // 585 lines of OpenAPI path fragments for auth-protected routes. Per internal/query/AGENTS.md, each openapi_paths_*.go file is a single string literal that contributes to the assembled OpenAPI spec; splitting the string across files breaks the per-fragment review boundary.

// 580 lines of OpenAPI path fragments for auth-protected routes. Per
// internal/query/AGENTS.md, each openapi_paths_*.go file is a single
// string literal that contributes to the assembled OpenAPI spec; splitting
// the string across files breaks the per-fragment review boundary.

const openAPIPathsAuth = `
    "/api/v0/auth/providers": {
      "get": {
        "tags": ["auth"],
        "summary": "List configured SSO providers for login (tenant-scoped)",
        "description": "Public pre-auth endpoint scoped to a single tenant. Returns the configured OIDC and SAML providers available for interactive browser login for the specified tenant so the console can render SSO buttons. The response exposes only the opaque provider_config_id (required by the redirect endpoints) and a safe generic display label derived from the protocol class (never an IdP hostname, issuer, or operator-specific name). No secrets or private IdP configuration are ever returned. When tenant_id is absent or empty an empty array is returned — the endpoint never performs a global cross-tenant scan. When no providers are configured for the tenant an empty array is returned. The response carries Cache-Control: public, max-age=60.",
        "operationId": "listAuthProviders",
        "security": [],
        "parameters": [
          {
            "name": "tenant_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Tenant to list SSO providers for. When absent or empty the response is always an empty providers array — no global cross-tenant scan is performed."
          }
        ],
        "responses": {
          "200": {
            "description": "The configured SSO providers for the tenant. Empty array when tenant_id is absent or no providers are configured.",
            "headers": {
              "Cache-Control": {
                "description": "Always set to 'public, max-age=60'.",
                "schema": {"type": "string"}
              }
            },
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "providers": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "required": ["provider_config_id", "display_label", "provider_kind"],
                        "properties": {
                          "provider_config_id": {
                            "type": "string",
                            "description": "Opaque operator-assigned provider config identifier. Required by the OIDC and SAML redirect endpoints."
                          },
                          "display_label": {
                            "type": "string",
                            "description": "Safe generic label for the login button. Always a generic protocol-class label (e.g. 'Single sign-on (OIDC)'). Never echoes a domain, org name, or IdP identifier."
                          },
                          "provider_kind": {
                            "type": "string",
                            "enum": ["oidc", "saml"],
                            "description": "Protocol class: oidc or saml. Used by the console to select the correct redirect helper."
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/auth/oidc/login": {
      "get": {
        "tags": ["auth"],
        "summary": "Start a dashboard OIDC login",
        "description": "Starts the backend OpenID Connect Authorization Code flow for dashboard users. The server stores only SHA-256 hashes for state, nonce, and redirect URI proof, then redirects the browser to the configured provider. Provider group claims are later mapped to Eshu roles and grants; raw OIDC tokens and raw group names are not persisted.",
        "operationId": "startOIDCLogin",
        "parameters": [
          {"name": "provider_config_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional opaque provider config id. Defaults to the configured default provider."},
          {"name": "tenant_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional tenant id; must match the selected provider config."},
          {"name": "workspace_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional workspace id; must match the selected provider config."},
          {"name": "return_to", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional same-origin return path after callback. Absolute URLs and protocol-relative paths are ignored."}
        ],
        "responses": {
          "302": {
            "description": "Redirect to the configured OIDC provider authorization endpoint.",
            "headers": {
              "Location": {"description": "Provider authorization URL with state and nonce.", "schema": {"type": "string"}}
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/oidc/callback": {
      "get": {
        "tags": ["auth"],
        "summary": "Complete a dashboard OIDC login",
        "description": "Completes the backend Authorization Code callback, verifies issuer metadata/JWKS through the configured provider, validates state, nonce, redirect URI proof, and subject claims, then maps hashed external groups to Eshu role grants before issuing the same hash-only browser-session cookies used by explicit session creation. Unmapped groups or revoked grant mappings deny login and create no session.",
        "operationId": "completeOIDCLogin",
        "parameters": [
          {"name": "state", "in": "query", "required": true, "schema": {"type": "string"}},
          {"name": "code", "in": "query", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {
          "201": {
            "description": "OIDC login completed and browser session created. Returned when no return path was stored.",
            "headers": {
              "Set-Cookie": {
                "description": "Sets __Host-eshu_session and __Host-eshu_csrf with the same browser-session security attributes.",
                "schema": {"type": "string"}
              }
            },
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/BrowserSessionResponse"}
              }
            }
          },
          "303": {
            "description": "OIDC login completed and browser session cookies were set before redirecting to the stored same-origin return path.",
            "headers": {
              "Location": {"description": "Stored same-origin return path.", "schema": {"type": "string"}},
              "Set-Cookie": {"description": "Sets __Host-eshu_session and __Host-eshu_csrf.", "schema": {"type": "string"}}
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/browser-session": {
      "post": {
        "tags": ["auth"],
        "summary": "Create a dashboard browser session",
        "description": "Exchanges an already-authenticated explicit API credential for a server-managed dashboard browser session. The raw session secret is only sent in the __Host-eshu_session cookie, which is HttpOnly, Secure, SameSite=Strict, Path=/. The server persists only SHA-256 hashes for the session and CSRF secrets. The response returns the CSRF secret and also sets __Host-eshu_csrf as a readable Secure SameSite=Strict cookie so browser clients can send X-Eshu-CSRF on unsafe cookie-authenticated requests. Existing browser sessions cannot mint new browser sessions from this route; CLI, MCP, and automation clients should keep using explicit bearer tokens.",
        "operationId": "createBrowserSession",
        "responses": {
          "201": {
            "description": "Browser session created; Set-Cookie includes __Host-eshu_session and __Host-eshu_csrf.",
            "headers": {
              "Set-Cookie": {
                "description": "__Host-eshu_session is HttpOnly/Secure/SameSite=Strict; __Host-eshu_csrf is readable by the browser client and must be echoed in X-Eshu-CSRF for unsafe requests.",
                "schema": {"type": "string"}
              }
            },
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/BrowserSessionResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "get": {
        "tags": ["auth"],
        "summary": "Read the current dashboard browser session",
        "description": "Returns the auth context attached to the current browser session cookie. This route requires cookie authentication and does not reveal the raw session secret or CSRF secret.",
        "operationId": "getBrowserSession",
        "responses": {
          "200": {
            "description": "Current browser session context.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/BrowserSessionResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"}
        }
      },
      "delete": {
        "tags": ["auth"],
        "summary": "Revoke the current dashboard browser session",
        "description": "Revokes the current browser session by its server-side hash and clears both browser-session cookies. Because it is an unsafe cookie-authenticated request, callers must include X-Eshu-CSRF with the CSRF secret bound to the session.",
        "operationId": "deleteBrowserSession",
        "responses": {
          "204": {
            "description": "Browser session revoked and cookies cleared.",
            "headers": {
              "Set-Cookie": {
                "description": "Clears __Host-eshu_session and __Host-eshu_csrf.",
                "schema": {"type": "string"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/browser-session/context": {
      "patch": {
        "tags": ["auth"],
        "summary": "Switch the current dashboard session tenant/workspace",
        "description": "Moves the current active all-scopes browser session to another active tenant/workspace boundary. Scoped sessions cannot switch workspaces until the identity/grant UX can model cross-workspace grants explicitly. The session remains revocable by its original session hash. Because it is an unsafe cookie-authenticated request, callers must include X-Eshu-CSRF with the CSRF secret bound to the session.",
        "operationId": "switchBrowserSessionContext",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["tenant_id", "workspace_id"],
                "properties": {
                  "tenant_id": {"type": "string"},
                  "workspace_id": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Updated browser session context.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/BrowserSessionResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/bootstrap": {
      "post": {
        "tags": ["auth"],
        "summary": "Bootstrap the first local identity owner",
        "description": "Operator-controlled one-time setup for deployments that do not use an external IdP. This route is not public: it requires the shared operator bearer token, stores only hashed login/recovery identifiers plus a bcrypt password hash, creates the first owner role assignment, and requires MFA recovery-code material for the admin account.",
        "operationId": "bootstrapLocalIdentity",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityBootstrapRequest"}}}
        },
        "responses": {
          "201": {"description": "First owner/admin created."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/login": {
      "post": {
        "tags": ["auth"],
        "summary": "Authenticate a local identity",
        "description": "Public local-login route. The handler hashes the submitted login id and optional recovery code before storage lookup, verifies the password against the stored bcrypt hash, enforces MFA for admin accounts, applies lockout state, and creates the same secure browser-session cookies used by the dashboard when authentication succeeds.",
        "operationId": "loginLocalIdentity",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityLoginRequest"}}}
        },
        "responses": {
          "200": {"description": "Authenticated and browser session created.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}}}},
          "202": {"description": "Password accepted but MFA proof is required.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}}}},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "423": {"description": "Local identity is temporarily locked."},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/invitations": {
      "post": {
        "tags": ["auth"],
        "summary": "Create a local identity invitation",
        "description": "Creates a hash-only invitation for assignment-required signup. This route requires all-scopes admin authentication; broad open self-signup is intentionally not supported.",
        "operationId": "createLocalIdentityInvitation",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityInvitationRequest"}}}
        },
        "responses": {
          "201": {"description": "Invitation created. The invite_code is returned once and only the hash is persisted."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "get": {
        "tags": ["auth"],
        "summary": "List the tenant's invitations",
        "description": "All-scopes admin route that lists invitations within the caller's own tenant/workspace: invite id, role id, status, and lifecycle timestamps. Never returns the invite code, invitee handle, or inviter identity (all stored only as hashes).",
        "operationId": "listAdminInvitations",
        "responses": {
          "200": {
            "description": "The tenant's invitations.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "invitations": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "invite_id": {"type": "string"},
                          "role_id": {"type": "string"},
                          "status": {"type": "string"},
                          "expires_at": {"type": "string", "format": "date-time"},
                          "accepted_at": {"type": "string", "format": "date-time"},
                          "revoked_at": {"type": "string", "format": "date-time"},
                          "created_at": {"type": "string", "format": "date-time"},
                          "updated_at": {"type": "string", "format": "date-time"},
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
    "/api/v0/auth/local/invitations/accept": {
      "post": {
        "tags": ["auth"],
        "summary": "Accept a local identity invitation",
        "description": "Public invitation acceptance route. A valid active invite code is required; the route stores only invite/login/recovery hashes plus a bcrypt password hash.",
        "operationId": "acceptLocalIdentityInvitation",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityInvitationAcceptanceRequest"}}}
        },
        "responses": {
          "201": {"description": "Invitation accepted and local identity created."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/users/{user_id}/password": {
      "post": {
        "tags": ["auth"],
        "summary": "Reset a local identity password",
        "description": "All-scopes admin route that revokes active local credentials, stores a new bcrypt password hash, and clears lockout state.",
        "operationId": "resetLocalIdentityPassword",
        "parameters": [{"name": "user_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"type": "object", "required": ["password"], "properties": {"password": {"type": "string", "format": "password"}}}}}
        },
        "responses": {
          "204": {"description": "Password reset."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/password/rotate": {
      "post": {
        "tags": ["auth"],
        "summary": "Self-service local identity password rotation",
        "description": "Public pre-session route (issue #4976). Re-proves the caller's current_password (and recovery_code, when the account has an active MFA factor) instead of relying on an existing session, then stores a new bcrypt password hash and clears must_change_password. This is the only way a must_change_password=true credential -- the ESHU_ADMIN_USERNAME/PASSWORD[_FILE]-seeded bootstrap admin -- can ever obtain a session; any local user may also use it to voluntarily rotate their own password. Returns the same LocalIdentitySessionResponse shape as login: mfa_required (202) when the account has an active MFA factor and no recovery_code was submitted, locked (423) after too many failed attempts, or invalid (401) for a wrong current_password.",
        "operationId": "rotateLocalIdentityPassword",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityPasswordRotationRequest"}}}
        },
        "responses": {
          "200": {
            "description": "Password rotated; browser session issued.",
            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}}}
          },
          "202": {
            "description": "Credential proof accepted but more proof (MFA recovery code) or another rotation is still required.",
            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"description": "Wrong current_password or unknown login_id."},
          "423": {"description": "Local login is temporarily locked after repeated failed proof attempts."},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/users/{user_id}/mfa-reset": {
      "post": {
        "tags": ["auth"],
        "summary": "Reset local identity MFA recovery material",
        "description": "All-scopes admin route that revokes active MFA factors and recovery codes, then stores replacement recovery-code hashes.",
        "operationId": "resetLocalIdentityMFA",
        "parameters": [{"name": "user_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"type": "object", "required": ["recovery_codes"], "properties": {"mfa_factor_kind": {"type": "string"}, "mfa_credential_handle": {"type": "string"}, "recovery_codes": {"type": "array", "items": {"type": "string"}}}}}}
        },
        "responses": {
          "204": {"description": "MFA material reset."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/users/{user_id}/disable": {
      "post": {
        "tags": ["auth"],
        "summary": "Disable a local identity",
        "description": "All-scopes admin route that disables a user and revokes active local credentials, MFA factors, and browser sessions.",
        "operationId": "disableLocalIdentity",
        "parameters": [{"name": "user_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "204": {"description": "Local identity disabled."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/break-glass": {
      "post": {
        "tags": ["auth"],
        "summary": "Enable time-boxed local identity break-glass recovery",
        "description": "Operator-controlled break-glass enablement. This route is not public, requires the shared operator bearer token, is disabled by default when no active window exists, stores only a break-glass code hash, and emits governance audit events for allowed or denied enablement.",
        "operationId": "enableLocalIdentityBreakGlass",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityBreakGlassRequest"}}}
        },
        "responses": {
          "201": {"description": "Break-glass window enabled. The break_glass_code is returned once and only the hash is persisted."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/break-glass/session": {
      "post": {
        "tags": ["auth"],
        "summary": "Create a browser session from break-glass recovery",
        "description": "Public recovery route that succeeds only when an operator-enabled break-glass window is active, unexpired, and unconsumed. The submitted break-glass code is hashed before lookup and successful recovery emits a governance audit event.",
        "operationId": "createLocalIdentityBreakGlassSession",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"type": "object", "required": ["break_glass_code"], "properties": {"break_glass_code": {"type": "string", "format": "password"}}}}}
        },
        "responses": {
          "200": {"description": "Break-glass browser session created.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}}}},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/saml/providers/{provider_id}/metadata": {
      "get": {
        "tags": ["auth"],
        "summary": "Read SAML service-provider metadata",
        "description": "public route that returns Eshu service-provider metadata for a configured SAML provider. The metadata advertises the provider-specific EntityID and POST ACS endpoint only; raw assertions, raw NameID values, group claims, private metadata URLs, provider secrets, and browser-session state are never returned.",
        "operationId": "getSAMLServiceProviderMetadata",
        "parameters": [
          {
            "name": "provider_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Opaque SAML provider configuration identifier."
          }
        ],
        "responses": {
          "200": {
            "description": "SAML service-provider metadata XML.",
            "content": {
              "application/samlmetadata+xml": {
                "schema": {"type": "string"}
              }
            }
          },
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/saml/providers/{provider_id}/login": {
      "get": {
        "tags": ["auth"],
        "summary": "Start SAML login",
        "description": "public SP-initiated SAML login route. Eshu creates an AuthnRequest, generates a RelayState secret, stores only the RelayState SHA-256 hash with the request ID and short expiry, and redirects the browser to the IdP SSO endpoint. The raw RelayState is used only in the browser redirect and is required by the ACS route before any browser session can be created.",
        "operationId": "startSAMLLogin",
        "parameters": [
          {
            "name": "provider_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Opaque SAML provider configuration identifier."
          },
          {"name": "return_to", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional same-origin return path after the ACS callback. Absolute URLs and protocol-relative paths are ignored."}
        ],
        "responses": {
          "302": {
            "description": "Redirect to the IdP SSO endpoint.",
            "headers": {
              "Location": {
                "description": "IdP SSO URL carrying SAMLRequest and RelayState.",
                "schema": {"type": "string"}
              }
            }
          },
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/saml/providers/{provider_id}/acs": {
      "post": {
        "tags": ["auth"],
        "summary": "Complete SAML login at the assertion consumer service",
        "description": "public ACS route that accepts IdP POSTed RelayState and SAMLResponse form fields. Eshu consumes the RelayState hash, validates the signed assertion through the configured SAML provider, reserves a hash-only replay key before session creation, maps NameID and group claims through Eshu-owned memberships and grants, and then creates the normal __Host-eshu_session and __Host-eshu_csrf browser cookies. The server persists only SHA-256 hashes for session, CSRF, RelayState, subject, group-claim, and replay material; raw SAML assertions and raw claim values are not stored or returned.",
        "operationId": "postSAMLAssertionConsumerService",
        "parameters": [
          {
            "name": "provider_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Opaque SAML provider configuration identifier."
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/x-www-form-urlencoded": {
              "schema": {
                "type": "object",
                "required": ["RelayState", "SAMLResponse"],
                "properties": {
                  "RelayState": {"type": "string"},
                  "SAMLResponse": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "SAML login accepted; Set-Cookie includes __Host-eshu_session and __Host-eshu_csrf. Returned when no return_to path was stored.",
            "headers": {
              "Set-Cookie": {
                "description": "__Host-eshu_session is HttpOnly/Secure/SameSite=Strict; __Host-eshu_csrf is readable by the browser client and must be echoed in X-Eshu-CSRF for unsafe requests.",
                "schema": {"type": "string"}
              }
            },
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/BrowserSessionResponse"}
              }
            }
          },
          "303": {
            "description": "SAML login accepted; browser redirected to the same-origin return_to path stored at login initiation. Set-Cookie is set before the redirect.",
            "headers": {
              "Location": {
                "description": "Same-origin path from the original return_to query parameter.",
                "schema": {"type": "string"}
              },
              "Set-Cookie": {
                "description": "__Host-eshu_session and __Host-eshu_csrf browser-session cookies.",
                "schema": {"type": "string"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/profile": {
      "get": {
        "tags": ["auth"],
        "summary": "Read the caller's identity profile",
        "description": "Returns the authenticated caller's own profile: identity provider config id (absent for local identity), active tenant/workspace, role ids, permitted permission features, MFA status, and memberships. Cookie- or bearer-authenticated; never returns secrets, credential handles, or other subjects' data.",
        "operationId": "getAuthProfile",
        "responses": {
          "200": {
            "description": "Caller profile.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "external_provider_config_id": {"type": "string"},
                    "active_tenant_id": {"type": "string"},
                    "active_workspace_id": {"type": "string"},
                    "role_ids": {"type": "array", "items": {"type": "string"}},
                    "allowed_permission_features": {"type": "array", "items": {"type": "string"}},
                    "permission_catalog_enforced": {"type": "boolean"},
                    "mfa": {
                      "type": "object",
                      "properties": {
                        "has_active_mfa": {"type": "boolean"},
                        "factor_kind": {"type": "string"}
                      }
                    },
                    "memberships": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
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
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/sessions": {
      "get": {
        "tags": ["auth"],
        "summary": "List the caller's own browser sessions",
        "description": "Returns metadata for the authenticated caller's own active browser sessions: issued/last-seen/expiry timestamps, tenant/workspace, and which row is the current session. Never returns the session hash or secret, and never returns other subjects' sessions.",
        "operationId": "listAuthSessions",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "description": "Maximum number of sessions to return (default 20, max 100). Invalid or missing values use the default.",
            "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
          },
          {
            "name": "offset",
            "in": "query",
            "description": "Number of sessions to skip before the first result (default 0).",
            "schema": {"type": "integer", "minimum": 0, "default": 0}
          }
        ],
        "responses": {
          "200": {
            "description": "The caller's browser sessions.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "sessions": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "issued_at": {"type": "string", "format": "date-time"},
                          "last_seen_at": {"type": "string", "format": "date-time"},
                          "idle_expires_at": {"type": "string", "format": "date-time"},
                          "absolute_expires_at": {"type": "string", "format": "date-time"},
                          "tenant_id": {"type": "string"},
                          "workspace_id": {"type": "string"},
                          "current": {"type": "boolean"},
                          "revoked_at": {"type": "string", "format": "date-time"}
                        }
                      }
                    },
                    "prev": {
                      "type": "string",
                      "description": "Relative URL to the previous page of results. Present only when offset > 0."
                    },
                    "next": {
                      "type": "string",
                      "description": "Relative URL to the next page of results. Present only when more results exist."
                    }
                  }
                }
              }
            }
          },
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
