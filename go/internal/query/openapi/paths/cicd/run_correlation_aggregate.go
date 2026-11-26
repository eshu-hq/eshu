// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

// RunCorrelationAggregate is the OpenAPI path fragment documenting the
// `/api/v0/ci-cd/run-correlations/count`,
// `/api/v0/ci-cd/run-correlations/inventory` routes. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const RunCorrelationAggregate = `
    "/api/v0/ci-cd/run-correlations/count": {
      "get": {
        "summary": "Count CI/CD run correlations without paging the list endpoint",
        "operationId": "countCICDRunCorrelations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
          {"name": "commit_sha", "in": "query", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "artifact_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "rejected"]}}
        ],
        "responses": {
          "200": {
            "description": "Reducer-owned CI/CD run correlation totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_correlations": {"type": "integer", "description": "Reducer-owned CI/CD run correlations matching the filters."},
                    "by_outcome": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Correlation counts by outcome (exact, derived, ambiguous, unresolved, rejected). Bucket names are outcome values; render as bars, never a fixed enum."},
                    "by_environment": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Correlation counts by environment. Rows with an empty environment fold to unknown. Bucket names are environment values; render as bars, never a fixed enum."},
                    "by_provider": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Correlation counts by provider. Rows with an empty provider fold to unknown. Bucket names are provider values; render as bars, never a fixed enum."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear). Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/ci-cd/run-correlations/inventory": {
      "get": {
        "summary": "Group CI/CD run correlations by one dimension without paging the list endpoint",
        "operationId": "getCICDRunCorrelationInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["outcome", "environment", "repository_id", "provider"], "default": "outcome"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty inventory.", "schema": {"type": "string"}},
          {"name": "commit_sha", "in": "query", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "artifact_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "rejected"]}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
          "200": {
            "description": "Grouped count buckets ordered by count desc",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "description": "Grouped count buckets ordered by count desc, per the inventory handler.",
                      "items": {
                        "type": "object",
                        "description": "One grouped count bucket. Owner: cicd run-correlations inventory handler.",
                        "properties": {
                          "dimension": {"type": "string", "description": "Bucket dimension value; the label comes from the group_by request value."},
                          "value": {"type": "string", "description": "Group value."},
                          "count": {"type": "integer", "description": "Correlations in this bucket."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Buckets in this page."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset."},
                    "group_by": {"type": "string", "description": "Echo of the requested grouping dimension."},
                    "truncated": {"type": "boolean", "description": "True when more buckets matched than this page carries; continue with next_offset."},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear). Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
