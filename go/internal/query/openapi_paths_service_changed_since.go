// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsFreshnessServiceChangedSince = `
    "/api/v0/freshness/services/changed-since": {
      "get": {
        "tags": ["freshness"],
        "summary": "Summarize what changed for a service since a prior service generation",
        "description": "Returns a bounded service-scope changed-since delta for one service (#1943). It diffs a prior service materialization generation's evidence snapshot set against the current active generation's set, keyed by a generation-independent service_evidence_key, into per-evidence-family counts for added, updated, unchanged, retired, and superseded keys plus bounded, deterministic sample handles per classification. It reports the ownership (#1943), deployment (#1985), runtime (#1986), dependencies (#1987), docs (#1988), incidents (#1989), and vulnerabilities (#1990) evidence families. Supply since_generation_id to diff from an exact prior service generation. An unknown service_id returns service_not_found; a since reference that matches no service generation returns not_found. A service with no current active generation returns an explicit unavailable diff instead of zero deltas. Counts are exact; only the per-classification samples are capped by sample_limit with a per-classification truncated flag.",
        "operationId": "summarizeServiceChangedSince",
        "parameters": [
          {"name": "service_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Exact service id whose evidence lineage to diff."},
          {"name": "since_generation_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Prior service materialization generation id to diff from."},
          {"name": "sample_limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 25}, "description": "Maximum sample handles returned per classification per family."}
        ],
        "responses": {
          "200": {
            "description": "Service-scope changed-since delta summary",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_id": {"type": "string"},
                    "since_generation_id": {"type": "string"},
                    "since_observed_at": {"type": "string"},
                    "current_active_generation_id": {"type": "string"},
                    "current_observed_at": {"type": "string"},
                    "sample_limit": {"type": "integer"},
                    "unavailable": {"type": "boolean"},
                    "categories": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "category": {"type": "string", "enum": ["ownership", "deployment", "runtime", "dependencies", "docs", "incidents", "vulnerabilities"]},
                          "counts": {
                            "type": "object",
                            "properties": {
                              "added": {"type": "integer"},
                              "updated": {"type": "integer"},
                              "unchanged": {"type": "integer"},
                              "retired": {"type": "integer"},
                              "superseded": {"type": "integer"}
                            },
                            "required": ["added", "updated", "unchanged", "retired", "superseded"]
                          },
                          "samples": {
                            "type": "object",
                            "additionalProperties": {
                              "type": "array",
                              "items": {
                                "type": "object",
                                "properties": {
                                  "stable_fact_key": {"type": "string"},
                                  "fact_kind": {"type": "string"}
                                },
                                "required": ["stable_fact_key", "fact_kind"]
                              }
                            }
                          },
                          "truncated": {
                            "type": "object",
                            "additionalProperties": {"type": "boolean"}
                          },
                          "unavailable": {"type": "boolean"}
                        },
                        "required": ["category", "counts", "unavailable"]
                      }
                    }
                  },
                  "required": ["service_id", "since_generation_id", "current_active_generation_id", "sample_limit", "categories", "unavailable"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
