package query

const openAPIPathsIaC = `
    "/api/v0/iac/dead": {
      "post": {
        "tags": ["iac"],
        "summary": "Find dead IaC candidates",
        "description": "Finds bounded, content-derived dead-IaC candidates for explicit repository scopes. Dynamic references are returned as ambiguous until reducer-materialized usage rows make the result exact.",
        "operationId": "findDeadIaC",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Single repository ID to analyze."},
                  "repo_ids": {
                    "type": "array",
                    "description": "Explicit bounded repository scope to analyze.",
                    "items": {"type": "string"}
                  },
                  "families": {
                    "type": "array",
                    "description": "Optional IaC families to include: terraform, helm, kustomize, ansible, compose.",
                    "items": {"type": "string"}
                  },
                  "include_ambiguous": {"type": "boolean", "description": "Include dynamic-reference candidates.", "default": false},
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Dead-IaC candidate findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_ids": {"type": "array", "items": {"type": "string"}},
                    "findings_count": {"type": "integer"},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"]},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "family": {"type": "string"},
                          "repo_id": {"type": "string"},
                          "artifact": {"type": "string"},
                          "reachability": {"type": "string", "enum": ["unused", "ambiguous"]},
                          "finding": {"type": "string"},
                          "confidence": {"type": "number"},
                          "evidence": {"type": "array", "items": {"type": "string"}},
                          "limitations": {"type": "array", "items": {"type": "string"}}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/ServiceUnavailable"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
