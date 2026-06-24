// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsDocumentationFindingAggregate = `
    "/api/v0/documentation/findings/count": {
      "get": {
        "summary": "Count durable documentation findings without paging the list endpoint",
        "operationId": "countDocumentationFindings",
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "finding_type", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "truth_level", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Documentation finding totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_findings": {"type": "integer"},
                    "by_status": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_truth_level": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_freshness_state": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/documentation/findings/inventory": {
      "get": {
        "summary": "Group durable documentation findings by one dimension without paging the list endpoint",
        "operationId": "getDocumentationFindingInventory",
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["status", "truth_level", "freshness_state", "finding_type", "source_id"], "default": "status"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "finding_type", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "truth_level", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}},
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
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string"},
                          "value": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "group_by": {"type": "string"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
