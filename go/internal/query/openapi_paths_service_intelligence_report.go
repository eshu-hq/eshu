// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsServiceIntelligenceReport = `
    "/api/v0/services/{service_name}/intelligence-report": {
      "get": {
        "tags": ["entities"],
        "summary": "Service intelligence report",
        "description": "Composes the one-call service intelligence report: identity, code-to-runtime trace, deployment/config influence, supply-chain, and incident sections, each with preserved truth labels, evidence handles, limitations, and bounded next calls, plus deterministic suggested investigations. Returns schema service_intelligence_report.v1. Runs no LLM interpretation path.",
        "operationId": "getServiceIntelligenceReport",
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {
            "name": "service_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional canonical workload id used to disambiguate the service selector"
          },
          {
            "name": "repo",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector used to disambiguate the service"
          },
          {
            "name": "environment",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional environment context"
          }
        ],
        "responses": {
          "200": {
            "description": "Composed service intelligence report",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema": {"type": "string"},
                    "subject": {"type": "object"},
                    "supported": {"type": "boolean"},
                    "partial": {"type": "boolean"},
                    "truth_class": {"type": "string"},
                    "truth": {"type": "object"},
                    "sections": {"type": "array", "items": {"type": "object"}},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "suggested_investigations": {"type": "array", "items": {"type": "object"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
