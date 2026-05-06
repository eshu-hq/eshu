package query

const openAPIPathsEvidence = `
    "/api/v0/evidence/relationships/{resolved_id}": {
      "get": {
        "tags": ["evidence"],
        "summary": "Get relationship evidence",
        "description": "Dereferences a compact relationship evidence pointer from repository context by resolved_id and returns the durable Postgres evidence row, preview details, and source/target metadata.",
        "operationId": "getRelationshipEvidence",
        "parameters": [
          {
            "name": "resolved_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "resolved_relationships.resolved_id returned by deployment_evidence artifacts or evidence_index"
          }
        ],
        "responses": {
          "200": {
            "description": "Relationship evidence drilldown",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "lookup_basis": {"type": "string", "enum": ["resolved_id"]},
                    "resolved_id": {"type": "string"},
                    "postgres_lookup_id": {"type": "string"},
                    "generation_id": {"type": "string"},
                    "generation": {"type": "object"},
                    "source": {"type": "object"},
                    "target": {"type": "object"},
                    "relationship_type": {"type": "string"},
                    "confidence": {"type": "number"},
                    "evidence_count": {"type": "integer"},
                    "evidence_kinds": {"type": "array", "items": {"type": "string"}},
                    "evidence_type": {"type": "string"},
                    "evidence_preview": {"type": "array", "items": {"type": "object"}},
                    "rationale": {"type": "string"},
                    "resolution_source": {"type": "string"},
                    "details": {"type": "object"}
                  },
                  "required": ["lookup_basis", "resolved_id", "generation_id", "source", "target", "relationship_type", "confidence", "evidence_count"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Postgres relationship read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
`
