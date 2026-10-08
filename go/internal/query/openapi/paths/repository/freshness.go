// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// Freshness documents the repository freshness route
// (#5143). It is split from Routes to keep repository
// OpenAPI files small.
const Freshness = `
    "/api/v0/repositories/{repo_id}/freshness": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get per-repository commit receipt and build-completeness verdict",
        "description": "Answers two questions for one repository: did eshu pick up its latest commit, and is the evidence for that commit fully built. verdict is one of current, building, behind, unobserved, not_selected, or unknown. verdict=current speaks to BUILD COMPLETENESS for the resolved generation, not necessarily a commit receipt: observed_commit may be an empty string while verdict is still honestly current. An empty observed_commit is legitimate for non-git scopes, for pre-delta-baseline git generations that predate the source_commit_sha column, and for snapshot-trigger git generations (trigger_kind=snapshot: a cassette-replayed or otherwise non-live-git-sync source with no commit to report, as opposed to a push/delta-triggered sync) -- represented explicitly rather than fabricated. The optional expected_commit query parameter is compared as an opaque string (no format validation); when it does not match observed_commit the verdict is behind regardless of whether a generation is actively progressing. shared_enrichment reports cross-repo materialization backlog referencing this repository's generation as a separate axis from stages, so a different repository's shared backlog is never attributed here. selection reports whether the repository is still in some live selector's listing (#7625): state is one of selected, not_selected, pending_confirmation, excluded_still_ingested, or unknown. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryFreshness",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {
            "name": "expected_commit",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional commit SHA the caller expects to be observed. When supplied and it does not match observed_commit, the verdict is behind."
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Repository freshness verdict",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef"},
                    "scope_id": {"type": "string"},
                    "verdict": {"type": "string", "enum": ["current", "building", "behind", "unobserved", "not_selected", "unknown"]},
                    "observed_commit": {"type": "string", "description": "May be empty for non-git scopes, pre-delta-baseline generations, or snapshot-trigger git generations (trigger_kind=snapshot). An empty value with verdict=current means build completeness for this generation, not a commit receipt."},
                    "observed_at": {"type": "string", "nullable": true},
                    "generation": {
                      "type": "object",
                      "nullable": true,
                      "properties": {
                        "id": {"type": "string"},
                        "status": {"type": "string"},
                        "trigger_kind": {"type": "string"},
                        "is_delta": {"type": "boolean"},
                        "activated_at": {"type": "string", "nullable": true}
                      }
                    },
                    "stages": {
                      "type": "object",
                      "properties": {
                        "collected": {"type": "boolean"},
                        "reduced": {"type": "boolean"},
                        "projected": {"type": "boolean"},
                        "materialized": {"type": "boolean"}
                      }
                    },
                    "outstanding_by_stage": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "stage": {"type": "string"},
                          "status": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "shared_enrichment": {
                      "type": "object",
                      "properties": {
                        "pending": {"type": "boolean"},
                        "pending_domains": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "domain": {"type": "string"},
                              "count": {"type": "integer"}
                            }
                          }
                        }
                      }
                    },
                    "unobserved_push": {
                      "type": "object",
                      "nullable": true,
                      "properties": {
                        "target_sha": {"type": "string"},
                        "ref": {"type": "string"},
                        "received_at": {"type": "string", "nullable": true}
                      }
                    },
                    "selection": {
                      "type": "object",
                      "description": "Whether the repository is still in some live selector's listing (#7625). state=not_selected with reason=confirmed_exclusion means every live selector excludes the repository, every exclusion is confirmed, and no generation was observed after the exclusions began.",
                      "properties": {
                        "state": {"type": "string", "enum": ["selected", "not_selected", "pending_confirmation", "excluded_still_ingested", "unknown"]},
                        "reason": {"type": "string", "enum": ["no_live_observations", "live_selected_row", "unconfirmed_exclusion", "generation_observed_after_state_since", "confirmed_exclusion"]},
                        "state_since": {"type": "string", "nullable": true, "description": "Latest exclusion start among the live rows; null when state=unknown."},
                        "last_listed_at": {"type": "string", "nullable": true, "description": "Latest listing sighting among the live rows; null when no evaluation ever listed the scope."},
                        "evaluated_at": {"type": "string", "nullable": true, "description": "Latest evaluation stamp among the live rows; null when state=unknown."},
                        "live_selector_count": {"type": "integer", "description": "Number of live observation rows behind this block."}
                      }
                    },
                    "as_of": {"type": "string"},
                    "scoped": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
