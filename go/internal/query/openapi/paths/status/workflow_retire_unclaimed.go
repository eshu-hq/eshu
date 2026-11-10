// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// WorkflowRetireUnclaimed is the OpenAPI path fragment documenting
// POST /api/v0/admin/workflow-retire-unclaimed. It lives apart from Admin
// because admin.go sits at the 500-line cap (the repository_retirement.go
// precedent). openapi.Spec concatenates it into the published document
// right after RepositoryRetirement; keep it in lockstep with the handler.
const WorkflowRetireUnclaimed = `
    "/api/v0/admin/workflow-retire-unclaimed": {
      "post": {
        "tags": ["admin"],
        "summary": "Retire unclaimed workflow work items",
        "description": "Moves at most 100 never-claimed pending workflow work items (status pending, attempt_count 0, no current claim) of one deconfigured collector instance to failed_terminal with the operator-retired failure class. The response count includes only rows transitioned by this call; a repeat call retires the residue or nothing. An unknown instance fails closed with 404; a still-configured instance fails closed with 409 and a deny audit event, naming the instance. A 503 means the admin store is not configured on this API instance.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["collector_kind", "collector_instance_id"],
                "properties": {
                  "collector_kind": {"type": "string"},
                  "collector_instance_id": {"type": "string"},
                  "operator_note": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Retire request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
