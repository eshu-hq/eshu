package query

const openAPIPathsAuthTokens = `
    "/api/v0/auth/local/api-tokens": {
      "post": {
        "tags": ["auth"],
        "summary": "Create a generated API token",
        "description": "All-scopes admin route that creates a generated personal or service-principal API token. Shared-operator callers whose auth context has no tenant/workspace must provide tenant_id and workspace_id in the request body. The raw bearer value is returned once in api_token; storage persists only token_hash metadata, active subject ownership, status, expiry, and last-used timestamps. Personal tokens inherit the target user's active grants at request time, and service-principal tokens inherit the active service-principal role grants.",
        "operationId": "createLocalIdentityAPIToken",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityAPITokenCreateRequest"}}}
        },
        "responses": {
          "201": {"description": "Generated API token created. The api_token value is returned once only.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityAPITokenResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      },
      "get": {
        "tags": ["auth"],
        "summary": "List the caller's generated API tokens",
        "description": "Returns metadata for the authenticated caller's own personal and service-principal generated API tokens: token id, class, and issued/expires/revoked timestamps. Never returns the token hash or raw bearer value, and never returns other subjects' tokens.",
        "operationId": "listLocalIdentityAPITokens",
        "responses": {
          "200": {
            "description": "The caller's generated API tokens.",
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
                          "issued_at": {"type": "string", "format": "date-time"},
                          "expires_at": {"type": "string", "format": "date-time"},
                          "revoked_at": {"type": "string", "format": "date-time"}
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
    "/api/v0/auth/local/api-tokens/{token_id}/revoke": {
      "post": {
        "tags": ["auth"],
        "summary": "Revoke a generated API token",
        "description": "All-scopes admin route that immediately marks one active generated API token revoked within the caller's tenant/workspace. Shared-operator callers whose auth context has no tenant/workspace must provide tenant_id and workspace_id in the request body. The request never accepts or returns bearer token values and emits token_lifecycle audit events.",
        "operationId": "revokeLocalIdentityAPIToken",
        "parameters": [{"name": "token_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": false,
          "content": {"application/json": {"schema": {"type": "object", "properties": {"tenant_id": {"type": "string"}, "workspace_id": {"type": "string"}, "reason_code": {"type": "string", "default": "api_token_revoked"}}}}}
        },
        "responses": {
          "204": {"description": "Generated API token revoked."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/api-tokens/{token_id}/rotate": {
      "post": {
        "tags": ["auth"],
        "summary": "Rotate a generated API token",
        "description": "All-scopes admin route that atomically inserts a replacement token hash and revokes the old generated API token in the same tenant/workspace. Shared-operator callers whose auth context has no tenant/workspace must provide tenant_id and workspace_id in the request body. The replacement api_token is returned once and never persisted raw.",
        "operationId": "rotateLocalIdentityAPIToken",
        "parameters": [{"name": "token_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"type": "object", "properties": {"tenant_id": {"type": "string"}, "workspace_id": {"type": "string"}, "expires_at": {"type": "string", "format": "date-time"}}}}}
        },
        "responses": {
          "201": {"description": "Replacement generated API token created. The api_token value is returned once only.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/LocalIdentityAPITokenResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
