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
`
