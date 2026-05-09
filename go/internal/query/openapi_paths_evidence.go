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
    "/api/v0/documentation/findings": {
      "get": {
        "tags": ["documentation"],
        "summary": "List documentation truth findings",
        "description": "Lists read-only documentation truth findings from durable documentation finding facts for external updater actuators.",
        "operationId": "listDocumentationFindings",
        "parameters": [
          {"name": "finding_type", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "truth_level", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}},
          {"name": "updated_since", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 50, "maximum": 200}},
          {"name": "cursor", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Documentation findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "findings": {"type": "array", "items": {"type": "object"}},
                    "next_cursor": {"type": "string"}
                  },
                  "required": ["findings", "next_cursor"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation evidence packet capability or Postgres documentation read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/documentation/findings/{finding_id}/evidence-packet": {
      "get": {
        "tags": ["documentation"],
        "summary": "Get documentation evidence packet",
        "description": "Returns the bounded evidence packet for one documentation truth finding. External updater services should snapshot this response before planning a diff.",
        "operationId": "getDocumentationEvidencePacket",
        "parameters": [
          {
            "name": "finding_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Documentation finding ID returned by the findings list"
          }
        ],
        "responses": {
          "200": {
            "description": "Documentation evidence packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "packet_id": {"type": "string"},
                    "packet_version": {"type": "string"},
                    "generated_at": {"type": "string", "format": "date-time"},
                    "finding": {"type": "object"},
                    "document": {"type": "object"},
                    "section": {"type": "object"},
                    "bounded_excerpt": {"type": "object"},
                    "linked_entities": {"type": "array", "items": {"type": "object"}},
                    "current_truth": {"type": "object"},
                    "evidence_refs": {"type": "array", "items": {"type": "object"}},
                    "truth": {"type": "object"},
                    "permissions": {"type": "object"},
                    "states": {"type": "object"}
                  },
                  "required": ["packet_id", "packet_version", "generated_at", "finding", "document", "section", "bounded_excerpt", "linked_entities", "current_truth", "evidence_refs", "truth", "permissions", "states"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation evidence packet capability or Postgres documentation read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/documentation/evidence-packets/{packet_id}/freshness": {
      "get": {
        "tags": ["documentation"],
        "summary": "Check documentation evidence packet freshness",
        "description": "Checks whether a previously snapshotted documentation evidence packet is still current before an external updater publishes a diff.",
        "operationId": "checkDocumentationEvidencePacketFreshness",
        "parameters": [
          {
            "name": "packet_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Evidence packet ID returned by getDocumentationEvidencePacket"
          }
        ],
        "responses": {
          "200": {
            "description": "Documentation evidence packet freshness",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "packet_id": {"type": "string"},
                    "packet_version": {"type": "string"},
                    "freshness_state": {"type": "string"},
                    "latest_packet_version": {"type": "string"}
                  },
                  "required": ["packet_id", "packet_version", "freshness_state", "latest_packet_version"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation evidence packet capability or Postgres documentation read model is unavailable",
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
