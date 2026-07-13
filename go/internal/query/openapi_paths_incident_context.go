// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsIncidentContext = `
    "/api/v0/incidents/{incident_id}/context": {
      "get": {
        "tags": ["incidents"],
        "summary": "Get incident context",
        "description": "Returns a bounded incident context packet from active incident source facts. The response includes provider incident state, timeline events, intended/applied/live PagerDuty routing evidence, fallback change candidates, explicit evidence-path slots, missing evidence for unproven hops, deployable/image/runtime artifact evidence only when explicit service-catalog and reducer-owned runtime facts prove those links, build/commit evidence only when reducer-owned CI/CD run correlations match the selected image digest or reference, and PR/work-item enrichment only when provider PR evidence or Jira work-item evidence proves those hops.",
        "operationId": "getIncidentContext",
        "x-scoped-token-support": true,
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
                      "description": "Ordered incident to service/routing/deployable/runtime/image/build/commit/PR/work-item slots with exact, derived, fallback, drifted, ambiguous, permission_hidden, unresolved, stale, rejected, or missing truth labels.",
                      "items": {"type": "object"}
                    },
                    "missing_evidence": {"type": "array", "items": {"type": "object"}},
                    "ambiguous_evidence": {"type": "array", "items": {"type": "object"}},
                    "truncated": {"type": "boolean"},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls."}
                  },
                  "required": ["query", "incident", "timeline", "related_changes", "evidence_path", "missing_evidence", "ambiguous_evidence", "truncated", "answer_metadata"]
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
