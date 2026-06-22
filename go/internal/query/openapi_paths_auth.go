package query

const openAPIPathsAuth = `
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
`
