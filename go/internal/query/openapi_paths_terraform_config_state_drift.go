// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsTerraformConfigStateDrift = `
    "/api/v0/terraform/config-state-drift/findings": {
      "post": {
        "tags": ["terraform"],
        "summary": "List Terraform config-vs-state drift findings",
        "description": "Lists active reducer-materialized Terraform config-vs-state drift findings for one bounded state-snapshot scope. Provider-neutral: config-vs-state drift is not cloud-specific, so this route is separate from the AWS and multi-cloud runtime-drift routes. Every finding carries an outcome: exact (a classified per-address drift kind) or ambiguous (backend-owner resolution found more than one candidate config repo; no per-address classification ran). Scoped tokens must supply an exact scope_id that resolves to a granted repository or ingestion scope; an out-of-grant scope_id receives an empty page.",
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
                  "outcome": {"type": "string", "enum": ["exact", "ambiguous"], "description": "Optional outcome filter."},
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
                    "scope_id": {"type": "string"},
                    "address": {"type": "string"},
                    "outcome": {"type": "string"},
                    "drift_kinds": {"type": "array", "items": {"type": "string"}},
                    "story": {"type": "string"},
                    "outcome_groups": {"type": "array", "items": {"type": "object"}},
                    "findings_count": {"type": "integer"},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"]},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
                    "graph_projection_note": {"type": "string"},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "drift_findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_id": {"type": "string"},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "source_system": {"type": "string"},
                          "canonical_id": {"type": "string"},
                          "candidate_id": {"type": "string"},
                          "candidate_kind": {"type": "string"},
                          "outcome": {"type": "string", "enum": ["exact", "ambiguous"]},
                          "address": {"type": "string"},
                          "drift_kind": {"type": "string"},
                          "backend_kind": {"type": "string"},
                          "locator_hash": {"type": "string"},
                          "confidence": {"type": "number"},
                          "ambiguous_owner_candidates": {"type": "array", "items": {"type": "object"}, "description": "Every competing config repo's identity for an admin/unscoped caller. A scoped caller only sees candidates inside its own grant; out-of-grant candidates are removed, not returned redacted."},
                          "ambiguous_owner_candidates_withheld_count": {"type": "integer", "description": "Count of ambiguous_owner_candidates entries removed because their repo_id was outside a scoped caller's grant. Omitted (zero) for an unscoped caller."},
                          "evidence": {"type": "array", "items": {"type": "object"}, "description": "Reducer evidence atoms explaining the finding (address, drift_kind, config, state, and a prior atom whenever a prior-generation row exists for the address). Each atom's own scope_id names the repository/state-snapshot scope that atom's data came from, which can differ from the finding's own scope_id -- notably the config atom's scope_id is the config repo's scope, not the state-snapshot scope this endpoint is bound to. A scoped caller only sees an atom's scope_id when it is inside its own grant; an out-of-grant scope_id is removed from the atom (the atom itself, and its other fields, are still returned)."}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "501": {"description": "Terraform config-vs-state drift findings require reducer-materialized drift facts"}
        }
      }
    },
`
