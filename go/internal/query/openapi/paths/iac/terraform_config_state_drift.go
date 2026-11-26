// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// TerraformConfigStateDrift is the OpenAPI path fragment documenting the
// `/api/v0/terraform/config-state-drift/findings` route. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const TerraformConfigStateDrift = `
    "/api/v0/terraform/config-state-drift/findings": {
      "post": {
        "tags": ["terraform"],
        "summary": "List Terraform config-vs-state drift findings",
        "description": "Lists active reducer-materialized Terraform config-vs-state drift findings for one bounded state-snapshot scope. Provider-neutral: config-vs-state drift is not cloud-specific, so this route is separate from the AWS and multi-cloud runtime-drift routes. Every finding carries an outcome: exact (a classified per-address drift kind whose config-side address resolved unambiguously), derived (a classified per-address drift kind whose address may not match the real Terraform state address because it, or its paired config-side/state-side counterpart, depended on an unresolved module-prefix fallback -- a Terraform-Registry-shorthand misclassification or a module chain deeper than the resolver's depth bound; both members of a spurious added_in_config/added_in_state pair for the same resource carry derived when the pairing is unambiguous, not only the config-side half -- the finding's evidence array names the specific reason), ambiguous (backend-owner resolution found more than one candidate config repo; no per-address classification ran), or unresolved (backend-owner resolution found zero candidate config repos AT EVALUATION TIME -- no Eshu-tracked repo declared this backend as of the generation evaluated; no per-address classification ran). A zero-row resolver result does not prove no owner exists, only that none was resolvable when this generation was evaluated: a repo that has not yet synced into Eshu, or genuinely does not track its backend as a config, both produce the same result today, and a later evaluation of the same generation (a fresh sync, a bootstrap-index re-run) can resolve to exact or ambiguous instead once an owning repo is present. A scope that has not resolved at evaluation time is reported as one unresolved finding rather than an empty page, so it can be told apart from a scope that resolved cleanly and simply has no drift. Scoped tokens must supply an exact scope_id that resolves to a granted repository or ingestion scope; an out-of-grant scope_id receives an empty page.",
        "operationId": "listTerraformConfigStateDriftFindings",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string", "description": "Exact Terraform state-snapshot scope, for example state_snapshot:s3:hash-1."},
                  "address": {"type": "string", "description": "Optional exact Terraform resource address to inspect."},
                  "outcome": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved"], "description": "Optional outcome filter."},
                  "drift_kinds": {
                    "type": "array",
                    "description": "Optional drift kinds: added_in_state, added_in_config, attribute_drift, removed_from_state, or removed_from_config.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
                },
                "required": ["scope_id"]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Terraform config-vs-state drift findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string", "description": "Echo of the caller's exact state-snapshot scope filter, emitted by the drift handler from FindingFilter.ScopeID."},
                    "address": {"type": "string", "description": "Echo of the caller's Terraform resource address filter, emitted by the drift handler from FindingFilter.Address."},
                    "outcome": {"type": "string", "description": "Echo of the caller's outcome filter, emitted by the drift handler from FindingFilter.Outcome."},
                    "drift_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's drift-kind filter, emitted by the drift handler from FindingFilter.DriftKinds."},
                    "story": {"type": "string", "description": "One-line page summary rendered by drift.terraformConfigStateDriftStory from the caller's filter and the page counts."},
                    "outcome_groups": {
                      "type": "array",
                      "description": "Finding counts grouped by outcome, emitted by drift.terraformConfigStateDriftOutcomeGroups.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "outcome": {"type": "string", "description": "Outcome bucket emitted by drift.terraformConfigStateDriftOutcomeGroups (exact, derived, ambiguous, or unresolved)."},
                          "count": {"type": "integer", "description": "Number of page findings in this outcome bucket, emitted by drift.terraformConfigStateDriftOutcomeGroups."}
                        },
                        "required": ["outcome", "count"]
                      }
                    },
                    "findings_count": {"type": "integer", "description": "Number of findings in this page, emitted by the drift handler as len(findings)."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings matching the filter, emitted by the drift handler from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the drift handler from FindingFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the drift handler from FindingFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this readback, emitted by the drift handler (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Read-model status label for this readback, emitted by the drift handler (always materialized_terraform_config_state_drift)."},
                    "graph_projection_note": {"type": "string", "description": "Read-model projection note emitted by the drift handler: this surface reads the reducer read model and the graph projection stays deferred."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this readback, emitted by the drift handler."},
                    "drift_findings": {
                      "type": "array",
                      "description": "Page of config-vs-state drift findings, emitted by the drift handler from the read store as []drift.FindingRow.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_id": {"type": "string", "description": "Reducer fact identity emitted by drift.FindingRow.FactID."},
                          "scope_id": {"type": "string", "description": "State-snapshot scope the finding was evaluated under, emitted by drift.FindingRow.ScopeID."},
                          "generation_id": {"type": "string", "description": "Reducer generation that produced the finding, emitted by drift.FindingRow.GenerationID."},
                          "source_system": {"type": "string", "description": "Source system that admitted the finding, emitted by drift.FindingRow.SourceSystem."},
                          "canonical_id": {"type": "string", "description": "Canonical finding identity emitted by drift.FindingRow.CanonicalID."},
                          "candidate_id": {"type": "string", "description": "Backend-owner candidate identity emitted by drift.FindingRow.CandidateID."},
                          "candidate_kind": {"type": "string", "description": "Backend-owner candidate kind emitted by drift.FindingRow.CandidateKind."},
                          "outcome": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved"], "description": "Backend-resolution outcome emitted by drift.FindingRow.Outcome."},
                          "address": {"type": "string", "description": "Terraform resource address under review, emitted by drift.FindingRow.Address."},
                          "drift_kind": {"type": "string", "description": "Classified per-address drift kind emitted by drift.FindingRow.DriftKind."},
                          "backend_kind": {"type": "string", "description": "State backend kind emitted by drift.FindingRow.BackendKind."},
                          "locator_hash": {"type": "string", "description": "Backend locator hash emitted by drift.FindingRow.LocatorHash."},
                          "confidence": {"type": "number", "description": "Finding confidence emitted by drift.FindingRow.Confidence."},
                          "ambiguous_owner_candidates": {"type": "array", "items": {"type": "object", "additionalProperties": true}, "description": "Every competing config repo's identity for an admin/unscoped caller, emitted by drift.FindingRow.AmbiguousOwnerCandidates as reducer-admitted open maps. Keys are the reducer-admitted candidate identity fields and always include repo_id, which names the competing config repo; a scoped caller only sees candidates inside its own grant (out-of-grant candidates are removed, not returned redacted, and counted in ambiguous_owner_candidates_withheld_count)."},
                          "ambiguous_owner_candidates_withheld_count": {"type": "integer", "description": "Count of ambiguous_owner_candidates entries removed because their repo_id was outside a scoped caller's grant, emitted by drift.FindingRow.AmbiguousOwnerCandidatesWithheldCount. Omitted (zero) for an unscoped caller."},
                          "evidence": {"type": "array", "items": {"type": "object", "additionalProperties": true}, "description": "Reducer evidence atoms explaining the finding (address, drift_kind, config, state, and a prior atom whenever a prior-generation row exists for the address), emitted by drift.FindingRow.Evidence as reducer-admitted open maps. Keys are the reducer-admitted atom fields and always include scope_id, which names the repository/state-snapshot scope that atom's data came from -- notably the config atom's scope_id is the config repo's scope, not the state-snapshot scope this endpoint is bound to. A scoped caller only sees an atom's scope_id when it is inside its own grant; an out-of-grant scope_id is removed from the atom (the atom itself, and its other fields, are still returned)."}
                        },
                        "required": ["fact_id", "scope_id", "generation_id", "source_system", "canonical_id", "candidate_id", "candidate_kind", "outcome", "backend_kind", "locator_hash", "confidence"]
                      }
                    }
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"description": "Terraform config-vs-state drift findings require reducer-materialized drift facts"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
