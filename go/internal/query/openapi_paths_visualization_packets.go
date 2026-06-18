package query

const openAPIPathsVisualizationPackets = `
    "/api/v0/visualizations/derive": {
      "post": {
        "tags": ["visualizations"],
        "summary": "Derive a visualization packet",
        "description": "Builds a bounded visualization packet from a source response the caller already received from an authorized answer route. The derivation is side-effect-free, performs no graph or content query, preserves the supplied source_truth in the packet, and returns a derived visualization.packet_derivation truth envelope. Supports service_story, evidence_citation, and incident_context views. Unsupported source shapes return an explicit unsupported packet with limitations and recommended next calls.",
        "operationId": "deriveVisualizationPacket",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "view": {
                    "type": "string",
                    "enum": ["service_story", "evidence_citation", "incident_context"],
                    "description": "Visualization view to derive from the source response."
                  },
                  "source_response": {
                    "type": "object",
                    "description": "Authorized source response payload from the matching answer route."
                  },
                  "source_truth": {
                    "type": "object",
                    "description": "Optional TruthEnvelope from the source response; copied into the derived visualization packet."
                  }
                },
                "required": ["view", "source_response"]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Derived visualization packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "visualization_packet": {
                      "type": "object",
                      "properties": {
                        "view": {"type": "string"},
                        "title": {"type": "string"},
                        "supported": {"type": "boolean"},
                        "nodes": {"type": "array", "items": {"type": "object"}},
                        "edges": {"type": "array", "items": {"type": "object"}},
                        "truth": {"type": "object"},
                        "limits": {"type": "object"},
                        "truncation": {"type": "object"},
                        "limitations": {"type": "array", "items": {"type": "string"}},
                        "recommended_next_calls": {"type": "array", "items": {"type": "object"}}
                      },
                      "required": ["view", "supported", "nodes", "edges", "limits", "truncation"]
                    }
                  },
                  "required": ["visualization_packet"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
`
