package query

const openAPIPathsInvestigationWorkflows = `
    "/api/v0/investigation-workflows": {
      "get": {
        "tags": ["query"],
        "summary": "List guided investigation workflows",
        "description": "Returns the deterministic guided investigation workflow catalog. This is workflow-plan truth from static catalog data, not a live graph query.",
        "operationId": "listInvestigationWorkflows",
        "responses": {
          "200": {
            "description": "Guided investigation workflow catalog",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "workflows": {"type": "array", "items": {"type": "object"}},
                    "versions": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/investigation-workflows/resolve": {
      "post": {
        "tags": ["query"],
        "summary": "Resolve a guided investigation workflow",
        "description": "Resolves one catalog workflow, declared inputs, and observed missing-evidence state into bounded recommended next calls. It does not execute the calls.",
        "operationId": "resolveInvestigationWorkflow",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["workflow_id"],
                "properties": {
                  "workflow_id": {"type": "string"},
                  "inputs": {"type": "object", "additionalProperties": {"type": "string"}},
                  "missing_evidence": {"type": "array", "items": {"type": "string"}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resolved recommended next calls",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "resolved": {"type": "object"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"}
        }
      }
    },
`
