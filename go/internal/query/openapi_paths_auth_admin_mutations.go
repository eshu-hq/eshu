package query

// openAPIPathsAuthAdminMutations documents the tenant-scoped admin identity
// mutation endpoints introduced in #3703 PR-2 that occupy their own path keys.
// The grant role-assignment (POST) and create group-mapping (POST) operations
// share a path key with their read-list GET, so they are merged into the read
// fragment (openapi_paths_auth_admin_reads.go) rather than declared here: an
// OpenAPI paths object cannot carry the same path key twice. This fragment holds
// only the routes whose path key is unique to the mutation surface.
//
// Every route requires all-scope admin authentication, writes strictly within
// the caller's own tenant/workspace (derived from the session, never a request
// body), is idempotent under retry, and emits a governance audit event. No route
// accepts or returns a secret, invite code, credential handle, or raw external
// group name.
const openAPIPathsAuthAdminMutations = `
    "/api/v0/auth/local/invitations/{invite_id}/revoke": {
      "post": {
        "tags": ["auth"],
        "summary": "Revoke an invitation",
        "description": "All-scopes admin route that soft-revokes one invitation within the caller's own tenant/workspace. Idempotent: an already revoked, accepted, or expired invitation is a safe no-op returning its current status. Never echoes the invite code. Emits a governance audit event.",
        "operationId": "revokeAdminInvitation",
        "parameters": [{"name": "invite_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The invitation's terminal state after the idempotent revoke.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "invite_id": {"type": "string"},
                    "status": {"type": "string"},
                    "revoked": {"type": "boolean"}
                  }
                }
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
    "/api/v0/auth/admin/role-assignments/revoke": {
      "post": {
        "tags": ["auth"],
        "summary": "Revoke a membership-role assignment",
        "description": "All-scopes admin route that tombstones a membership-role assignment for a user within the caller's own tenant/workspace. Idempotent: an already revoked assignment is a safe no-op. Optional workspace_id must match the caller's workspace. Emits a governance audit event.",
        "operationId": "revokeAdminRoleAssignment",
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
            "description": "The revoked assignment's state.",
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
    "/api/v0/auth/admin/idp-group-mappings/{mapping_ref}": {
      "delete": {
        "tags": ["auth"],
        "summary": "Delete an IdP group to role mapping",
        "description": "All-scopes admin route that tombstones one external group to role mapping resolved by its opaque mapping_ref within the caller's own tenant/workspace. Idempotent: an already-deleted or absent mapping is a safe no-op. The raw external group name is never needed or returned. Emits a governance audit event.",
        "operationId": "deleteAdminIdPGroupMapping",
        "parameters": [{"name": "mapping_ref", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "description": "The idempotent delete outcome.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "mapping_ref": {"type": "string"},
                    "deleted": {"type": "boolean"}
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
