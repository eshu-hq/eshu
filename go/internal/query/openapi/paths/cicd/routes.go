// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

// Routes is the OpenAPI path fragment documenting the
// `/api/v0/ci-cd/run-correlations` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/ci-cd/run-correlations": {
      "get": {
        "tags": ["ci-cd"],
        "summary": "List CI/CD run correlations",
        "description": "Lists reducer-owned CI/CD run, artifact, and environment correlations. CI success and shell-only hints are not deployment truth; exact rows require explicit artifact identity evidence. Repository-scoped responses include evidence_summary so static GitHub Actions workflow artifacts, live run rows, and run-to-artifact/image bridges stay separate even when live run correlation rows are missing.",
        "operationId": "listCICDRunCorrelations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL) to anchor lookup. Unknown selectors answer 404 and ambiguous selectors answer 409 instead of an empty page."},
          {"name": "commit_sha", "in": "query", "schema": {"type": "string"}, "description": "Commit SHA to answer what happened after a commit."},
          {"name": "provider", "in": "query", "schema": {"type": "string"}, "description": "CI/CD provider such as github_actions or gitlab_ci; required when provider_run_id is the only anchor."},
          {"name": "provider_run_id", "in": "query", "schema": {"type": "string"}, "description": "Provider-native run, build, or pipeline ID. Pair with provider when no other bounded anchor is present."},
          {"name": "run_id", "in": "query", "schema": {"type": "string"}, "description": "Alias for provider_run_id."},
          {"name": "artifact_digest", "in": "query", "schema": {"type": "string"}, "description": "Artifact or image digest anchor."},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}, "description": "Image reference anchor for tag-or-reference based correlation rows."},
          {"name": "environment", "in": "query", "schema": {"type": "string"}, "description": "Provider environment observation anchor."},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "rejected"]}},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "CI/CD run correlation rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "CI/CD run correlation rows page emitted by the run-correlations list handler.",
                  "properties": {
                    "correlations": {
                      "type": "array",
                      "description": "Correlation rows projected from fact payloads by decodeRunCorrelationRow.",
                      "items": {
                        "type": "object",
                        "description": "One correlation row: the fixed 17-key projection of a CI/CD run correlation fact payload.",
                        "properties": {
                          "correlation_id": {"type": "string", "description": "Correlation id (fact id) projected by decodeRunCorrelationRow."},
                          "provider": {"type": "string", "description": "CI/CD provider projected by decodeRunCorrelationRow."},
                          "run_id": {"type": "string", "description": "Provider-native run id projected by decodeRunCorrelationRow."},
                          "run_attempt": {"type": "string", "description": "Run attempt projected by decodeRunCorrelationRow."},
                          "repository_id": {"type": "string", "description": "Repository id projected by decodeRunCorrelationRow."},
                          "commit_sha": {"type": "string", "description": "Commit SHA projected by decodeRunCorrelationRow."},
                          "environment": {"type": "string", "description": "Environment projected by decodeRunCorrelationRow."},
                          "environment_evidence": {"type": "string", "enum": ["deploy_event", "declared"], "description": "How the environment was established. deploy_event means a ci.deployment_event observed at this run's commit supplied it; declared means it came from the CI-declared workflow job gate alone. Absent when the correlation has no environment."},
                          "artifact_digest": {"type": "string", "description": "Artifact digest anchor projected by decodeRunCorrelationRow."},
                          "image_ref": {"type": "string", "description": "Image reference anchor projected by decodeRunCorrelationRow."},
                          "outcome": {"type": "string", "description": "Correlation outcome projected by decodeRunCorrelationRow."},
                          "reason": {"type": "string", "description": "Outcome reason projected by decodeRunCorrelationRow."},
                          "provenance_only": {"type": "boolean", "description": "Whether the row carries provenance only, per decodeRunCorrelationRow."},
                          "canonical_writes": {"type": "integer", "description": "Canonical write count projected by decodeRunCorrelationRow."},
                          "canonical_target": {"type": "string", "description": "Canonical target projected by decodeRunCorrelationRow."},
                          "correlation_kind": {"type": "string", "description": "Correlation kind projected by decodeRunCorrelationRow."},
                          "evidence_fact_ids": {"type": "array", "description": "Evidence fact ids projected by decodeRunCorrelationRow.", "items": {"type": "string"}}
                        },
                        "required": ["correlation_id", "outcome", "provenance_only", "canonical_writes"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page counted by the run-correlations list handler."},
                    "limit": {"type": "integer", "description": "Page bound echoed by the run-correlations list handler."},
                    "truncated": {"type": "boolean", "description": "True when more correlations exist past this page, per the run-correlations list handler."},
                    "evidence_summary": {
                      "type": "object",
                      "description": "Static/live/bridge evidence summary for repository-scoped responses, per the run-correlations list handler.",
                      "properties": {
                        "static_workflow_artifacts": {
                          "type": "object",
                          "description": "Static GitHub Actions workflow artifact evidence, per the evidence summary builder.",
                          "properties": {
                            "state": {"type": "string", "enum": ["present", "absent", "unknown", "not_checked", "unavailable"], "description": "unknown means the bounded candidate file scan cannot establish workflow absence."},
                            "count": {"type": "integer", "description": "Workflow files observed in the candidate page; not a proven repository total when candidate coverage is uncertain."},
                            "candidate_pool_status": {"type": "string", "enum": ["unknown_at_limit"], "description": "The path-ordered file read returned a sentinel row past its 5,000-file limit, so files beyond the first 5,000 were not scanned. Omitted when the scan is complete, including a repository with exactly 5,000 files."},
                            "paths": {"type": "array", "description": "Displayed workflow paths (shortened to 20), per the evidence summary builder.", "items": {"type": "string"}},
                            "truncated": {"type": "boolean", "description": "The displayed workflow paths were shortened to 20. Separate from candidate_pool_status."},
                            "image_ref_count": {"type": "integer", "description": "Image references counted by the evidence summary builder."},
                            "unresolved_count": {"type": "integer", "description": "Unresolved image references counted by the evidence summary builder."},
                            "ambiguous_count": {"type": "integer", "description": "Ambiguous image references counted by the evidence summary builder."},
                            "evidence_class": {"type": "string", "enum": ["workflow_image_ref", "workflow_image_unresolved", "workflow_image_ambiguous"], "description": "Static artifact evidence class, per the evidence summary builder."},
                            "reason": {"type": "string", "description": "Static artifact reason, per the evidence summary builder."}
                          },
                          "required": ["state", "count"]
                        },
                        "live_run_correlations": {
                          "type": "object",
                          "description": "Live run correlation evidence, per the evidence summary builder.",
                          "properties": {
                            "state": {"type": "string", "enum": ["present", "missing", "unavailable"], "description": "Live correlation presence, per the evidence summary builder."},
                            "count": {"type": "integer", "description": "Live correlations counted by the evidence summary builder."},
                            "truncated": {"type": "boolean", "description": "Whether live correlations were cut, per the evidence summary builder."},
                            "reason": {"type": "string", "description": "Live correlation reason, per the evidence summary builder."}
                          },
                          "required": ["state", "count"]
                        },
                        "run_artifact_evidence": {
                          "type": "object",
                          "description": "Run-to-artifact/image bridge evidence, per the evidence summary builder.",
                          "properties": {
                            "state": {"type": "string", "enum": ["present", "missing", "ambiguous"], "description": "Bridge presence, per the evidence summary builder."},
                            "count": {"type": "integer", "description": "Bridge rows counted by the evidence summary builder."},
                            "artifact_digest_count": {"type": "integer", "description": "Artifact digests counted by the evidence summary builder."},
                            "image_ref_count": {"type": "integer", "description": "Image references counted by the evidence summary builder."},
                            "ambiguous_count": {"type": "integer", "description": "Ambiguous bridge rows counted by the evidence summary builder."},
                            "reason": {"type": "string", "description": "Bridge reason, per the evidence summary builder."}
                          },
                          "required": ["state", "count", "artifact_digest_count", "image_ref_count", "ambiguous_count"]
                        },
                        "missing_evidence": {
                          "type": "array",
                          "items": {"type": "string"},
                          "description": "Stable public-safe missing evidence classes for the source repository to provider run to image artifact bridge."
                        },
                        "reason": {"type": "string", "description": "Evidence summary reason, per the run-correlations list handler."}
                      },
                      "required": ["static_workflow_artifacts", "live_run_correlations", "run_artifact_evidence"]
                    },
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor for the next page, per the run-correlations list handler.",
                      "properties": {
                        "after_correlation_id": {"type": "string", "description": "Correlation id to continue after, per the run-correlations list handler."}
                      },
                      "required": ["after_correlation_id"]
                    },
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope", "description": "Collector readiness envelope attached by the run-correlations list handler."}
                  },
                  "required": ["correlations", "count", "limit", "truncated", "evidence_summary"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
