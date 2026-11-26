// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// WorkItem is the OpenAPI path fragment documenting the
// `/api/v0/work-items/evidence` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const WorkItem = `
    "/api/v0/work-items/evidence": {
      "get": {
        "tags": ["work-items"],
        "summary": "List work-item evidence",
        "description": "Lists bounded Jira/work-item source evidence from active facts. The route reports provider facts, missing evidence, stale/permission-hidden states, unsupported link types, and rejected unsafe payloads. Jira evidence remains source-only and never verifies pull request, commit, deployment, runtime artifact, image, version, service, or incident truth by itself.",
        "operationId": "listWorkItemEvidence",
        "x-scoped-token-support": true,
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
                          "evidence_state": {"type": "string", "enum": ["exact_provider_fact", "unsupported_link_type", "missing_evidence", "stale_evidence", "permission_hidden", "rejected_unsafe_payload", "metadata_warning"]},
                          "work_item_key": {"type": "string"},
                          "provider_work_item_id": {"type": "string"},
                          "project_id": {"type": "string", "description": "Provider-native project id; present on project-scoped rows."},
                          "project_key": {"type": "string", "description": "Provider-native project key; present on project-scoped rows."},
                          "issue_type_id": {"type": "string", "description": "Provider-native issue type id; present on issue rows."},
                          "issue_type_name": {"type": "string", "description": "Provider-native issue type name; present on issue rows."},
                          "status_id": {"type": "string", "description": "Provider-native status id; present on rows carrying a status."},
                          "status_name": {"type": "string", "description": "Provider-native status name; present on rows carrying a status."},
                          "created_at": {"type": "string", "description": "Provider-reported creation time of the work item; present when the provider supplies it."},
                          "updated_at": {"type": "string", "description": "Provider-reported last update time of the work item; present when the provider supplies it."},
                          "resolved_at": {"type": "string", "description": "Provider-reported resolution time; present only on resolved rows."},
                          "provider_changelog_id": {"type": "string"},
                          "field": {"type": "string", "description": "Changelog field that changed; present on changelog rows."},
                          "from": {"type": "string", "description": "Changelog value before the change; redacted values read empty with value_redacted true."},
                          "to": {"type": "string", "description": "Changelog value after the change; redacted values read empty with value_redacted true."},
                          "value_redacted": {"type": "boolean", "description": "True when from/to carried a redacted value."},
                          "provider_remote_link_id": {"type": "string"},
                          "global_id": {"type": "string", "description": "Provider-global id of the linked entity; present on rows that resolve one."},
                          "application_name": {"type": "string", "description": "Application name behind a remote link; present on link rows that resolve one."},
                          "application_type": {"type": "string", "description": "Application type behind a remote link; present on link rows that resolve one."},
                          "relationship": {"type": "string", "description": "How the linked entity relates to the work item; present on link rows."},
                          "correlation_anchor_class": {"type": "string"},
                          "linked_repository_id": {"type": "string", "description": "Canonical repository id resolved from a confidently typed GitHub PR or GitLab MR link before redaction; carries no raw URL or secret. Scoped tokens are authorized on this id. Omitted for links that did not canonicalize."},
                          "url_fingerprint": {"type": "string"},
                          "url_present": {"type": "boolean"},
                          "url_redacted": {"type": "boolean"},
                          "title_present": {"type": "boolean"},
                          "summary_present": {"type": "boolean"},
                          "provider_support_state": {"type": "string"},
                          "redaction_policy_version": {"type": "string"},
                          "metadata_type": {"type": "string", "description": "Metadata class a work_item.metadata_warning applies to (for example \"workflow\"). Present only on metadata_warning rows."},
                          "warning_reason": {"type": "string", "description": "Bounded reason a work_item.metadata_warning was raised (for example \"permission_hidden\", \"archived\", or \"unsupported\"). Present only on metadata_warning rows; the row's evidence_state is metadata_warning regardless of this reason."},
                          "provider_id_fingerprint": {"type": "string", "description": "One-way sha256 fingerprint of the redacted provider id a metadata_warning was raised against; carries no raw provider id. Omitted when the warning had no provider id."}
                        },
                        "required": ["fact_id", "fact_kind", "evidence_state"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows in this page."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound."},
                    "truncated": {"type": "boolean", "description": "True when more rows matched than this page carries; continue with next_cursor."},
                    "missing_evidence": {"type": "boolean", "description": "True when the page carries no rows."},
                    "states": {"type": "array", "items": {"type": "string"}, "description": "Distinct evidence_state values present in this page."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Forward cursor; present only when truncated is true.",
                      "properties": {
                        "after_fact_id": {"type": "string", "description": "Fact id to pass as after_fact_id for the next page."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
