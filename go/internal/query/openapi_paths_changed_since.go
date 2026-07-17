// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsFreshnessChangedSince = `
    "/api/v0/freshness/changed-since": {
      "get": {
        "tags": ["freshness"],
        "summary": "Summarize what changed since a prior generation or instant",
        "description": "Returns a bounded changed-since delta for one repository-kind scope. Exactly one mutually exclusive scope selector is required: scope_id or repository. It diffs the prior generation's fact set against the current active generation's fact set, keyed by stable_fact_key, into per-evidence-category counts (files, content entities, facts) for added, updated, unchanged, retired, and superseded keys plus bounded, deterministic sample handles per classification. Supply since_generation_id to diff from an exact prior generation, or since_observed_at (RFC3339) to diff from the generation observed at or before that instant. An unknown scope/repository returns scope_not_found; a since reference that matches no generation returns not_found. A scope with no current active generation returns an explicit unavailable diff instead of zero deltas. When retention cleanup proves the prior generation was pruned, the response remains unavailable and sets unavailable_reason to retention_expired. Counts are exact; only the per-classification samples are capped by sample_limit with a per-classification truncated flag.",
        "operationId": "summarizeChangedSince",
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Exact ingestion scope id. Mutually exclusive with repository; exactly one selector is required."},
          {"name": "repository", "in": "query", "schema": {"type": "string"}, "description": "Canonical repository id matched to repository-kind scopes by source_key. Mutually exclusive with scope_id; exactly one selector is required."},
          {"name": "since_generation_id", "in": "query", "schema": {"type": "string"}, "description": "Prior generation id to diff from (required unless since_observed_at is set)."},
          {"name": "since_observed_at", "in": "query", "schema": {"type": "string", "format": "date-time"}, "description": "RFC3339 instant; the diff baseline is the generation observed at or before this time (required unless since_generation_id is set)."},
          {"name": "sample_limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 25}, "description": "Maximum sample handles returned per classification per category."}
        ],
        "responses": {
          "200": {
            "description": "Changed-since delta summary",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string"},
                    "scope_kind": {"type": "string"},
                    "repository": {"type": "string"},
                    "since_generation_id": {"type": "string"},
                    "since_observed_at": {"type": "string"},
                    "current_active_generation_id": {"type": "string"},
                    "current_observed_at": {"type": "string"},
                    "sample_limit": {"type": "integer"},
                    "unavailable": {"type": "boolean"},
                    "unavailable_reason": {"type": "string", "enum": ["retention_expired"], "description": "Closed unavailable reason. Present when the requested prior generation was known for the scope but pruned by generation retention."},
                    "categories": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "category": {"type": "string", "enum": ["files", "content_entities", "facts"]},
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
                  "required": ["scope_id", "scope_kind", "since_generation_id", "current_active_generation_id", "sample_limit", "categories", "unavailable"]
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
