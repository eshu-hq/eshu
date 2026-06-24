// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPICollectorExtractionReadinessSchema is the inline schema for one
// collector family's advisory extraction readiness, reused by the list and
// drilldown responses.
const openAPICollectorExtractionReadinessSchema = `{
                      "type": "object",
                      "properties": {
                        "family": {"type": "string"},
                        "display_name": {"type": "string"},
                        "classification": {"type": "string", "enum": ["keep_in_tree", "extraction_candidate", "blocked", "external_ready"]},
                        "criteria": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "criterion": {"type": "string"},
                              "state": {"type": "string", "enum": ["met", "unmet", "not_applicable"]},
                              "detail": {"type": "string"}
                            }
                          }
                        },
                        "blockers": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "criterion": {"type": "string"},
                              "state": {"type": "string", "enum": ["met", "unmet", "not_applicable"]},
                              "detail": {"type": "string"}
                            }
                          }
                        },
                        "rationale": {"type": "string"}
                      }
                    }`

const openAPIPathsCollectorExtractionReadiness = `
    "/api/v0/collector-extraction-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "List collector extraction readiness",
        "description": "Returns the advisory collector extraction readiness catalog: for each collector family the extraction policy tracks, its classification (keep_in_tree, extraction_candidate, blocked, or external_ready), per-criterion checklist, and blockers. The data is static policy classification computed from documented repository evidence; it reads no runtime, graph, or registry state and never moves code.",
        "operationId": "listCollectorExtractionReadiness",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100},
            "description": "Maximum number of collector family rows to return."
          }
        ],
        "responses": {
          "200": {
            "description": "Collector extraction readiness catalog",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "status": {"type": "string", "enum": ["available"]},
                    "families": {
                      "type": "array",
                      "items": ` + openAPICollectorExtractionReadinessSchema + `
                    },
                    "count": {"type": "integer"},
                    "total_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
    "/api/v0/collector-extraction-readiness/{family}": {
      "get": {
        "tags": ["status"],
        "summary": "Get collector extraction readiness for one family",
        "description": "Returns the advisory extraction readiness drilldown for one collector family, including the per-criterion checklist and rationale. Advisory only; does not move code.",
        "operationId": "getCollectorExtractionReadiness",
        "parameters": [
          {
            "name": "family",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Canonical collector family kind, such as git, pagerduty, or jira."
          }
        ],
        "responses": {
          "200": {
            "description": "Collector extraction readiness drilldown",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "status": {"type": "string", "enum": ["available"]},
                    "family": ` + openAPICollectorExtractionReadinessSchema + `
                  }
                }
              }
            }
          },
          "404": {"$ref": "#/components/responses/NotFound"}
        }
      }
    },
`
