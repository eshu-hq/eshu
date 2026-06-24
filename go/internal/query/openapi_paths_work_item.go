// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsWorkItemEvidence = `
    "/api/v0/work-items/evidence": {
      "get": {
        "tags": ["work-items"],
        "summary": "List work-item evidence",
        "description": "Lists bounded Jira/work-item source evidence from active facts. The route reports provider facts, missing evidence, stale/permission-hidden states, unsupported link types, and rejected unsafe payloads. Jira evidence remains source-only and never verifies pull request, commit, deployment, runtime artifact, image, version, service, or incident truth by itself.",
        "operationId": "listWorkItemEvidence",
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Optional Jira collector scope id."},
          {"name": "project_key", "in": "query", "schema": {"type": "string"}, "description": "Optional Jira project key."},
          {"name": "work_item_key", "in": "query", "schema": {"type": "string"}, "description": "Optional Jira issue/work-item key."},
          {"name": "provider_work_item_id", "in": "query", "schema": {"type": "string"}, "description": "Optional provider-native Jira issue id."},
          {"name": "external_url", "in": "query", "schema": {"type": "string"}, "description": "Optional external URL to fingerprint server-side after removing sensitive query keys; raw URL is not returned."},
          {"name": "url_fingerprint", "in": "query", "schema": {"type": "string"}, "description": "Optional precomputed sanitized URL fingerprint."},
          {"name": "observed_after", "in": "query", "schema": {"type": "string", "format": "date-time"}, "description": "Optional observation lower bound."},
          {"name": "after_fact_id", "in": "query", "schema": {"type": "string"}, "description": "Fact id from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Work-item source evidence rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "evidence": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_id": {"type": "string"},
                          "fact_kind": {"type": "string"},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "provider": {"type": "string"},
                          "source_confidence": {"type": "string"},
                          "observed_at": {"type": "string"},
                          "evidence_state": {"type": "string", "enum": ["exact_provider_fact", "unsupported_link_type", "missing_evidence", "stale_evidence", "permission_hidden", "rejected_unsafe_payload"]},
                          "work_item_key": {"type": "string"},
                          "provider_work_item_id": {"type": "string"},
                          "project_id": {"type": "string"},
                          "project_key": {"type": "string"},
                          "provider_changelog_id": {"type": "string"},
                          "provider_remote_link_id": {"type": "string"},
                          "correlation_anchor_class": {"type": "string"},
                          "linked_repository_id": {"type": "string", "description": "Canonical repository id resolved from a confidently typed GitHub PR or GitLab MR link before redaction; carries no raw URL or secret. Scoped tokens are authorized on this id. Omitted for links that did not canonicalize."},
                          "url_fingerprint": {"type": "string"},
                          "url_present": {"type": "boolean"},
                          "url_redacted": {"type": "boolean"},
                          "title_present": {"type": "boolean"},
                          "summary_present": {"type": "boolean"},
                          "provider_support_state": {"type": "string"},
                          "redaction_policy_version": {"type": "string"}
                        },
                        "required": ["fact_id", "fact_kind", "evidence_state"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "missing_evidence": {"type": "boolean"},
                    "states": {"type": "array", "items": {"type": "string"}},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_fact_id": {"type": "string"}
                      },
                      "required": ["after_fact_id"]
                    }
                  },
                  "required": ["evidence", "count", "limit", "truncated", "missing_evidence", "states"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
