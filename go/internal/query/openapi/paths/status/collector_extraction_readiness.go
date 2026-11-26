// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// collectorExtractionReadinessSchema is the inline schema for one
// collector family's advisory extraction readiness, reused by the list and
// drilldown responses.
const collectorExtractionReadinessSchema = `{
                      "type": "object",
                      "description": "Advisory extraction readiness for one collector family. Owner: query/collector_extraction_readiness.go serving extraction.Readiness.",
                      "properties": {
                        "family": {"type": "string", "description": "Canonical collector family kind. Owner: extraction.Readiness Family."},
                        "display_name": {"type": "string", "description": "Human label for the family. Owner: extraction.Readiness DisplayName."},
                        "classification": {"type": "string", "enum": ["keep_in_tree", "extraction_candidate", "blocked", "external_ready"], "description": "Advisory extraction verdict. Owner: extraction.Readiness Classification."},
                        "criteria": {
                          "type": "array",
                          "description": "Per-criterion checklist in canonical order. Owner: extraction.Readiness Criteria.",
                          "items": {
                            "type": "object",
                            "description": "Evaluated state of one extraction criterion. Owner: extraction.CriterionResult.",
                            "properties": {
                              "criterion": {"type": "string", "description": "Policy row this result describes. Owner: extraction.CriterionResult Criterion."},
                              "state": {"type": "string", "enum": ["met", "unmet", "not_applicable"], "description": "Evidence state of the criterion. Owner: extraction.CriterionResult State."},
                              "detail": {"type": "string", "description": "Stable operator-facing reason for the state; absent when empty. Owner: extraction.CriterionResult Detail."}
                            }
                          }
                        },
                        "blockers": {
                          "type": "array",
                          "description": "Criteria whose state is unmet; empty unless the classification is blocked, absent when none. Owner: extraction.Readiness Blockers.",
                          "items": {
                            "type": "object",
                            "description": "One unmet extraction criterion. Owner: extraction.CriterionResult.",
                            "properties": {
                              "criterion": {"type": "string", "description": "Policy row this result describes. Owner: extraction.CriterionResult Criterion."},
                              "state": {"type": "string", "enum": ["met", "unmet", "not_applicable"], "description": "Evidence state of the criterion. Owner: extraction.CriterionResult State."},
                              "detail": {"type": "string", "description": "Stable operator-facing reason for the state; absent when empty. Owner: extraction.CriterionResult Detail."}
                            }
                          }
                        },
                        "rationale": {"type": "string", "description": "Why the family received its classification. Owner: extraction.Readiness Rationale."}
                      }
                    }`

// CollectorExtractionReadiness is the OpenAPI path fragment documenting the
// `/api/v0/collector-extraction-readiness`,
// `/api/v0/collector-extraction-readiness/{family}` routes. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const CollectorExtractionReadiness = `
    "/api/v0/collector-extraction-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "List collector extraction readiness",
        "description": "Returns the advisory collector extraction readiness catalog: for each collector family the extraction policy tracks, its classification (keep_in_tree, extraction_candidate, blocked, or external_ready), per-criterion checklist, and blockers. The data is static policy classification computed from documented repository evidence; it reads no runtime, graph, or registry state and never moves code.",
        "operationId": "listCollectorExtractionReadiness",
        "x-scoped-token-support": true,
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
                  "description": "Advisory collector extraction readiness catalog. Owner: query/collector_extraction_readiness.go CollectorExtractionReadinessListResponse.",
                  "required": ["schema_version", "status", "families", "count", "total_count", "limit", "truncated"],
                  "properties": {
                    "schema_version": {"type": "string", "description": "Catalog schema version. Owner: query/collector_extraction_readiness.go collectorExtractionReadinessSchemaVersion."},
                    "status": {"type": "string", "enum": ["available"], "description": "Catalog availability. Owner: query/collector_extraction_readiness.go list."},
                    "families": {
                      "type": "array",
                      "description": "One readiness row per tracked collector family. Owner: query/collector_extraction_readiness.go list.",
                      "items": ` + collectorExtractionReadinessSchema + `
                    },
                    "count": {"type": "integer", "description": "Families returned on this page. Owner: query/collector_extraction_readiness.go list."},
                    "total_count": {"type": "integer", "description": "Families in the catalog before truncation. Owner: query/collector_extraction_readiness.go list."},
                    "limit": {"type": "integer", "description": "Applied per-page family cap. Owner: query/collector_extraction_readiness.go limit."},
                    "truncated": {"type": "boolean", "description": "True when the catalog holds more families than the limit. Owner: query/collector_extraction_readiness.go list."}
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
        "x-scoped-token-support": true,
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
                  "description": "Advisory extraction readiness for one collector family. Owner: query/collector_extraction_readiness.go CollectorExtractionReadinessFamilyResponse.",
                  "required": ["schema_version", "status", "family"],
                  "properties": {
                    "schema_version": {"type": "string", "description": "Catalog schema version. Owner: query/collector_extraction_readiness.go collectorExtractionReadinessSchemaVersion."},
                    "status": {"type": "string", "enum": ["available"], "description": "Drilldown availability. Owner: query/collector_extraction_readiness.go getFamily."},
                    "family": ` + collectorExtractionReadinessSchema + `
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
