// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth //nolint:filelength // ~800 lines of OpenAPI path fragments for auth-protected routes. Per internal/query/AGENTS.md, each path fragment is a single string literal that contributes to the assembled OpenAPI spec; splitting the string across files breaks the per-fragment review boundary.

// ~800 lines of OpenAPI path fragments for auth-protected routes. Per
// internal/query/AGENTS.md, each path fragment is a single string literal
// that contributes to the assembled OpenAPI spec; splitting the string
// across files breaks the per-fragment review boundary. This is the only
// fragment over the 500-line cap; do not copy the exemption to a new file
// without the same justification.

// Routes is the OpenAPI path fragment documenting the `/api/v0/auth/*`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/auth/providers": {
      "get": {
        "tags": ["auth"],
        "summary": "Derive the tenant's pre-auth sign-in posture (issue #5165)",
        "description": "Public pre-auth endpoint scoped to a single tenant. Returns the tenant's derived AuthPosture: the configured OIDC/SAML providers available for interactive browser login, whether the local username/password form is offered, and whether self-service personal API tokens are offered — one discovery call for the console login picker instead of separate provider and sign-in-policy fetches. This is the single reusable posture derivation (query.DeriveAuthPosture) issue #5163 (F-2)'s MCP OAuth-discovery route also consumes to decide enablement. Provider entries expose only the opaque provider_config_id (required by the redirect endpoints), a safe generic display label, and a generic icon hint derived from the protocol class (never an IdP hostname, issuer, or operator-specific name). No secrets or private IdP configuration are ever returned. When tenant_id is absent or empty the safe zero-configuration default is returned (empty providers array, local login offered, self-service tokens offered) — the endpoint never performs a global cross-tenant scan. When no providers are configured for the tenant an empty array is returned. The response carries Cache-Control: public, max-age=60 — an admin enabling or disabling a provider is reflected on the tenant's NEXT (uncached) load, no restart required.",
        "operationId": "listAuthProviders",
        "security": [],
        "parameters": [
          {
            "name": "tenant_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Tenant to derive the sign-in posture for. When absent or empty the response is always the safe zero-configuration default — no global cross-tenant scan is performed."
          }
        ],
        "responses": {
          "200": {
            "description": "The tenant's derived sign-in posture. Empty providers array when tenant_id is absent or no providers are configured.",
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
                  "required": ["providers", "local_login_offered", "self_service_tokens_offered"],
                  "properties": {
                    "providers": {
                      "type": "array",
                      "description": "Derived provider entries for the tenant's login picker, per DeriveAuthPosture.",
                      "items": {
                        "type": "object",
                        "description": "One derived provider entry. Owner: DeriveAuthPosture.",
                        "required": ["provider_config_id", "display_label", "provider_kind", "icon_hint"],
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
                            "enum": ["oidc", "saml", "github"],
                            "description": "Protocol class: oidc, saml, or github. Used by the console to select the correct redirect helper. Unlike oidc/saml (a protocol class only), github additionally identifies the specific connector (issue #5166) since a GitHub provider IS github.com or a GitHub Enterprise Server instance by construction."
                          },
                          "icon_hint": {
                            "type": "string",
                            "enum": ["oidc", "saml", "github"],
                            "description": "Generic icon selector for the login button, derived from provider_kind. For oidc/saml this carries no IdP brand information — only the protocol class; github is the one exception (see provider_kind's description)."
                          }
                        }
                      }
                    },
                    "local_login_offered": {
                      "type": "boolean",
                      "description": "Whether the console should render the local username/password form. A UX hint only: false exactly when the tenant's sign-in policy has require_sso=true. The server's break-glass admin path (POST /api/v0/auth/local/login) always stays reachable regardless of this value."
                    },
                    "self_service_tokens_offered": {
                      "type": "boolean",
                      "description": "Whether an authenticated caller may self-issue a personal API token (issue #5164). Always true today — self-service token creation has no sign-in-policy gate."
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/github/login": {
      "get": {
        "tags": ["auth"],
        "summary": "Start a dashboard GitHub login (issue #5166, F-5)",
        "description": "Starts the backend GitHub Authorization Code (plain OAuth2) flow for dashboard users. Unlike /api/v0/auth/oidc/login, this is NOT OpenID Connect: github.com publishes no discovery document, issues no ID token, and has no JWKS — identity is resolved entirely by calling the GitHub REST API with the exchanged access token (see internal/githublogin's doc.go). The server stores only a SHA-256 hash of the state parameter (no nonce: plain OAuth2 has none) and a hash of the redirect URI, then redirects the browser to the configured GitHub (or GitHub Enterprise Server) authorization endpoint.",
        "operationId": "startGitHubLogin",
        "parameters": [
          {"name": "provider_config_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional opaque provider config id. Defaults to the configured default provider."},
          {"name": "tenant_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional tenant id; must match the selected provider config."},
          {"name": "workspace_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional workspace id; must match the selected provider config."},
          {"name": "return_to", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional same-origin return path after callback. Absolute URLs and protocol-relative paths are ignored."}
        ],
        "responses": {
          "302": {
            "description": "Redirect to the configured GitHub authorization endpoint.",
            "headers": {
              "Location": {"description": "Provider authorization URL with state.", "schema": {"type": "string"}}
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/github/callback": {
      "get": {
        "tags": ["auth"],
        "summary": "Complete a dashboard GitHub login (issue #5166, F-5)",
        "description": "Completes the GitHub Authorization Code callback: validates the one-time state, exchanges the code for an access token, resolves the caller's verified primary email, active GitHub org memberships, and team memberships from the GitHub REST API, denies login (with an audited server-side reason; no session created) when the caller has no verified email or no active membership in the provider's configured allowed_orgs, then maps hashed team handles (\"org/team-slug\") to Eshu role grants through the SAME group-to-role resolver an OIDC login's group claims use, before issuing the same hash-only browser-session cookies used by explicit session creation.",
        "operationId": "completeGitHubLogin",
        "parameters": [
          {"name": "state", "in": "query", "required": true, "schema": {"type": "string"}},
          {"name": "code", "in": "query", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {
          "201": {
            "description": "GitHub login completed and browser session created. Returned when no return path was stored.",
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
            "description": "GitHub login completed and browser session cookies were set before redirecting to the stored same-origin return path.",
            "headers": {
              "Location": {"description": "Stored same-origin return path.", "schema": {"type": "string"}},
              "Set-Cookie": {"description": "Sets __Host-eshu_session and __Host-eshu_csrf.", "schema": {"type": "string"}}
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
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
        "x-scoped-token-support": true,
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
        "x-browser-session-only": true,
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
        "x-browser-session-only": true,
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
        "x-browser-session-only": true,
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
          "201": {
            "description": "First owner/admin created. The response echoes the created identity's ids.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bootstrap outcome. Owner: local.handleBootstrap.",
                  "required": ["status", "user_id", "tenant_id", "workspace_id"],
                  "properties": {
                    "status": {"type": "string", "enum": ["bootstrapped"], "description": "Request disposition. Owner: local.handleBootstrap."},
                    "user_id": {"type": "string", "description": "Created owner user id. Owner: local.handleBootstrap via record.UserID."},
                    "tenant_id": {"type": "string", "description": "Owner tenant id. Owner: local.handleBootstrap via record.TenantID."},
                    "workspace_id": {"type": "string", "description": "Owner workspace id. Owner: local.handleBootstrap via record.WorkspaceID."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"description": "Server-side ID generation failed; the only 500 path on this route, per the bootstrap handler."},
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
          "202": {"description": "Credential accepted but more proof is required before a session is issued: MFA proof (mfa_required) or a password rotation (must_change_password).", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {
            "description": "Wrong login_id, password, or MFA proof. The body is the error envelope, or the session response with status invalid (no session issued).",
            "content": {"application/json": {"schema": {"oneOf": [{"$ref": "#/components/schemas/ErrorResponse"}, {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}]}}}
          },
          "403": {
            "description": "Local sign-in is disabled for this caller: the tenant requires SSO, or the identity is disabled. The body is the error envelope, or the session response with status disabled (no session issued).",
            "content": {"application/json": {"schema": {"oneOf": [{"$ref": "#/components/schemas/ErrorResponse"}, {"$ref": "#/components/schemas/LocalIdentitySessionResponse"}]}}}
          },
          "423": {"description": "Local identity is temporarily locked."},
          "500": {"description": "Browser session creation failed after successful authentication, per the session issuance handler."},
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
          "201": {
            "description": "Invitation created. The invite_code is returned once and only the hash is persisted.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Created invitation. Owner: local.handleCreateInvitation.",
                  "required": ["invite_id", "invite_code", "expires_at"],
                  "properties": {
                    "invite_id": {"type": "string", "description": "Invitation id. Owner: local.handleCreateInvitation via record.InviteID."},
                    "invite_code": {"type": "string", "description": "Plaintext invite code, returned once only. Owner: local.handleCreateInvitation."},
                    "expires_at": {"type": "string", "format": "date-time", "description": "When the invitation expires. Owner: local.handleCreateInvitation via record.ExpiresAt."}
                  }
                }
              }
            }
          },
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
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "The tenant's invitations.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Tenant invitations page emitted by handleListInvitations.",
                  "properties": {
                    "invitations": {
                      "type": "array",
                      "description": "Invitation rows projected by handleListInvitations; never carries the invite code or identity hashes.",
                      "items": {
                        "type": "object",
                        "description": "One invitation row: id, role, status, and lifecycle timestamps.",
                        "properties": {
                          "invite_id": {"type": "string", "description": "Invitation id projected by handleListInvitations."},
                          "role_id": {"type": "string", "description": "Invited role id projected by handleListInvitations."},
                          "status": {"type": "string", "description": "Invitation status projected by handleListInvitations."},
                          "expires_at": {"type": "string", "format": "date-time", "description": "When the invitation expires, per handleListInvitations."},
                          "accepted_at": {"type": "string", "format": "date-time", "description": "When the invitation was accepted, per handleListInvitations; absent when never accepted."},
                          "revoked_at": {"type": "string", "format": "date-time", "description": "When the invitation was revoked, per handleListInvitations; absent when never revoked."},
                          "created_at": {"type": "string", "format": "date-time", "description": "When the invitation was created, per handleListInvitations."},
                          "updated_at": {"type": "string", "format": "date-time", "description": "When the invitation was updated, per handleListInvitations."},
                          "tenant_id": {"type": "string", "description": "Tenant id projected by handleListInvitations."},
                          "workspace_id": {"type": "string", "description": "Workspace id projected by handleListInvitations."}
                        },
                        "required": ["invite_id", "role_id", "status", "expires_at", "created_at", "updated_at", "tenant_id", "workspace_id"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when the invitation page reached the list cap, per handleListInvitations."}
                  },
                  "required": ["invitations", "truncated"]
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
          "201": {
            "description": "Invitation accepted and local identity created.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Invitation acceptance outcome. Owner: local.handleAcceptInvitation.",
                  "required": ["status", "user_id"],
                  "properties": {
                    "status": {"type": "string", "enum": ["accepted"], "description": "Request disposition. Owner: local.handleAcceptInvitation."},
                    "user_id": {"type": "string", "description": "Created local identity user id. Owner: local.handleAcceptInvitation via acceptance.UserID."}
                  }
                }
              }
            }
          },
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
          "403": {"description": "Local sign-in is disabled by tenant policy; sign in with SSO, or an admin may use break-glass local sign-in."},
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
          "201": {
            "description": "Break-glass window enabled. The break_glass_code is returned once and only the hash is persisted.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Enabled break-glass window. Owner: local.handleEnableBreakGlass.",
                  "required": ["recovery_id", "break_glass_code", "expires_at", "policy_revision_hash"],
                  "properties": {
                    "recovery_id": {"type": "string", "description": "Break-glass window id. Owner: local.handleEnableBreakGlass via window.RecoveryID."},
                    "break_glass_code": {"type": "string", "description": "Plaintext break-glass code, returned once only. Owner: local.handleEnableBreakGlass."},
                    "expires_at": {"type": "string", "format": "date-time", "description": "When the break-glass window expires. Owner: local.handleEnableBreakGlass via window.ExpiresAt."},
                    "policy_revision_hash": {"type": "string", "description": "Policy revision hash the window was enabled under. Owner: local.handleEnableBreakGlass via window.PolicyRevisionHash."}
                  }
                }
              }
            }
          },
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
          "400": {"$ref": "#/components/responses/BadRequest"},
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
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Caller profile.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Caller identity profile aggregated by handleProfile; never exposes session hashes, token hashes, or credential handles.",
                  "properties": {
                    "external_provider_config_id": {"type": "string", "description": "Identity provider config id from the caller's auth context, per handleProfile; absent for local identity."},
                    "active_tenant_id": {"type": "string", "description": "Active tenant id from the caller's auth context, per handleProfile."},
                    "active_workspace_id": {"type": "string", "description": "Active workspace id from the caller's auth context, per handleProfile."},
                    "role_ids": {"type": "array", "description": "Role ids from the caller's auth context, per handleProfile.", "items": {"type": "string"}},
                    "allowed_permission_features": {"type": "array", "description": "Permitted permission features from the caller's auth context, per handleProfile.", "items": {"type": "string"}},
                    "permission_catalog_enforced": {"type": "boolean", "description": "Whether the permission catalog is enforced for the caller, per handleProfile."},
                    "mfa": {
                      "type": "object",
                      "description": "Safe MFA status fields looked up by handleProfile; never a credential handle or hash.",
                      "properties": {
                        "has_active_mfa": {"type": "boolean", "description": "Whether the caller has an active MFA factor, per handleProfile."},
                        "factor_kind": {"type": "string", "description": "Active MFA factor kind, per handleProfile; empty when none."}
                      },
                      "required": ["has_active_mfa"]
                    },
                    "memberships": {
                      "type": "array",
                      "description": "Active tenant/workspace membership only; no full per-subject membership query exists, per handleProfile.",
                      "items": {
                        "type": "object",
                        "description": "The caller's active tenant/workspace membership.",
                        "properties": {
                          "tenant_id": {"type": "string", "description": "Active tenant id, per handleProfile."},
                          "workspace_id": {"type": "string", "description": "Active workspace id, per handleProfile."}
                        },
                        "required": ["tenant_id", "workspace_id"]
                      }
                    }
                  },
                  "required": ["permission_catalog_enforced", "mfa", "memberships"]
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
        "x-browser-session-only": true,
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
                  "description": "Caller browser sessions page emitted by the session list handler.",
                  "properties": {
                    "sessions": {
                      "type": "array",
                      "description": "Active session metadata rows (sessionJSON); never carries a session hash or secret.",
                      "items": {
                        "type": "object",
                        "description": "One session row: timestamps, tenant/workspace, current marker, and revocation time.",
                        "properties": {
                          "issued_at": {"type": "string", "format": "date-time", "description": "When the session was issued, per the session list handler."},
                          "last_seen_at": {"type": "string", "format": "date-time", "description": "When the session was last seen, per the session list handler."},
                          "idle_expires_at": {"type": "string", "format": "date-time", "description": "Idle expiry of the session, per the session list handler."},
                          "absolute_expires_at": {"type": "string", "format": "date-time", "description": "Absolute expiry of the session, per the session list handler."},
                          "tenant_id": {"type": "string", "description": "Session tenant id, per the session list handler."},
                          "workspace_id": {"type": "string", "description": "Session workspace id, per the session list handler."},
                          "current": {"type": "boolean", "description": "True for the row of the calling session, per the session list handler."},
                          "revoked_at": {"type": "string", "format": "date-time", "description": "When the session was revoked, per the session list handler; absent when active."}
                        },
                        "required": ["issued_at", "last_seen_at", "idle_expires_at", "absolute_expires_at", "current"]
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
