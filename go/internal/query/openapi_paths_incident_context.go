package query

const openAPIPathsIncidentContext = `
    "/api/v0/incidents/{incident_id}/context": {
      "get": {
        "tags": ["incidents"],
        "summary": "Get incident context",
        "description": "Returns a bounded incident context packet from active incident source facts. The response includes provider incident state, timeline events, fallback change candidates, explicit evidence-path slots, missing evidence for unproven hops, and deployable/image/runtime artifact evidence only when explicit service-catalog and reducer-owned runtime facts prove those links.",
        "operationId": "getIncidentContext",
        "parameters": [
          {"name": "incident_id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string", "default": "pagerduty"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Optional provider scope disambiguator."},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Optional provider service id used to bound fallback change candidates."},
          {"name": "since", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "until", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 25}}
        ],
        "responses": {
          "200": {
            "description": "Incident context packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "query": {"type": "object"},
                    "incident": {"type": "object"},
                    "timeline": {"type": "array", "items": {"type": "object"}},
                    "related_changes": {
                      "type": "array",
                      "description": "Fallback change candidates matched by service and time window unless a later reducer promotes stronger evidence.",
                      "items": {"type": "object"}
                    },
                    "evidence_path": {
                      "type": "array",
                      "description": "Ordered incident to service/deployable/runtime/image/build/commit/PR/work-item slots with exact, derived, fallback, ambiguous, or missing truth labels.",
                      "items": {"type": "object"}
                    },
                    "missing_evidence": {"type": "array", "items": {"type": "object"}},
                    "ambiguous_evidence": {"type": "array", "items": {"type": "object"}},
                    "truncated": {"type": "boolean"}
                  },
                  "required": ["query", "incident", "timeline", "related_changes", "evidence_path", "missing_evidence", "ambiguous_evidence", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"description": "Incident id matched multiple active provider scopes; retry with scope_id."},
          "503": {"description": "Postgres incident source fact read model unavailable"}
        }
      }
    },
`
