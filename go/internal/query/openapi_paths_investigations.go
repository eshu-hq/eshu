package query

const openAPIPathsInvestigations = `
    "/api/v0/investigations/services/{service_name}": {
      "get": {
        "tags": ["entities"],
        "summary": "Investigate service",
        "description": "Returns a bounded service investigation packet with repositories considered, evidence coverage, findings, and recommended follow-up calls.",
        "operationId": "investigateService",
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {
            "name": "environment",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional environment context"
          },
          {
            "name": "intent",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional investigation intent such as runbook, onboarding, or incident"
          },
          {
            "name": "question",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional user question to preserve in the investigation packet"
          }
        ],
        "responses": {
          "200": {
            "description": "Service investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string"},
                    "environment": {"type": "string"},
                    "intent": {"type": "string"},
                    "question": {"type": "string"},
                    "repositories_considered": {"type": "array", "items": {"type": "object"}},
                    "repositories_with_evidence": {"type": "array", "items": {"type": "object"}},
                    "evidence_families_found": {"type": "array", "items": {"type": "string"}},
                    "coverage_summary": {"type": "object"},
                    "investigation_findings": {"type": "array", "items": {"type": "object"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "service_story_path": {"type": "string"},
                    "service_context_path": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
