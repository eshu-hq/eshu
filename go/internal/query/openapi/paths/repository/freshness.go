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
        "description": "Answers two questions for one repository: did eshu pick up its latest commit, and is the evidence for that commit fully built. verdict is one of current, building, behind, unobserved, not_selected, unknown, retiring, or retired. verdict=retiring (#7766) means an open non-complete retirement tombstone covers the repository and verdict=retired means its tombstone is complete; both outrank every other verdict as rule 0 and retirement carries the bounded tombstone evidence. verdict=not_selected (#7625) means no live selector that observes this repository's scope selects it, every live observation is confirmed exclusion evidence (the same archived_excluded, rule_excluded, or not_listed state over at least two evaluations spanning five minutes), and no generation of the scope was observed after the newest exclusion started, so no newer commit will arrive; it outranks every verdict except unknown. A selector's observations stay live for its configured liveness window after its newest evaluation. It is evidence only: nothing is hidden or deleted. selection always reports that evidence; when its state is anything other than not_selected (including unknown, when no selector row is live) the verdict is computed exactly as before. verdict=current speaks to BUILD COMPLETENESS for the resolved generation, not necessarily a commit receipt: observed_commit may be an empty string while verdict is still honestly current. An empty observed_commit is legitimate for non-git scopes, for pre-delta-baseline git generations that predate the source_commit_sha column, and for snapshot-trigger git generations (trigger_kind=snapshot: a cassette-replayed or otherwise non-live-git-sync source with no commit to report, as opposed to a push/delta-triggered sync) -- represented explicitly rather than fabricated. The optional expected_commit query parameter is compared as an opaque string (no format validation); when it does not match observed_commit the verdict is behind regardless of whether a generation is actively progressing. shared_enrichment reports cross-repo materialization backlog referencing this repository's generation as a separate axis from stages, so a different repository's shared backlog is never attributed here. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
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
                    "repository": {"$ref": "#/components/schemas/RepositoryRef", "description": "Repository identity emitted by the repository freshness handler from the base row (component shape)."},
                    "scope_id": {"type": "string", "description": "Canonical ingestion scope of the repository, emitted by the repository freshness handler from the receipt snapshot."},
                    "verdict": {"type": "string", "enum": ["current", "building", "behind", "unobserved", "not_selected", "unknown", "retiring", "retired"], "description": "retiring marks an open non-complete retirement tombstone and retired a complete one (#7766); both outrank every other verdict as rule 0."},
                    "observed_commit": {"type": "string", "description": "May be empty for non-git scopes, pre-delta-baseline generations, or snapshot-trigger git generations (trigger_kind=snapshot). An empty value with verdict=current means build completeness for this generation, not a commit receipt."},
                    "observed_at": {"type": "string", "nullable": true, "description": "Latest observation time of the resolved generation, emitted by the repository freshness handler (null when never observed)."},
                    "generation": {
                      "type": "object",
                      "nullable": true,
                      "description": "Resolved generation, emitted by the repository freshness handler from repositoryFreshnessGenerationToMap.",
                      "properties": {
                        "id": {"type": "string", "description": "Generation identity emitted by repository.repositoryFreshnessGenerationToMap."},
                        "status": {"type": "string", "description": "Generation status emitted by repository.repositoryFreshnessGenerationToMap."},
                        "trigger_kind": {"type": "string", "description": "Trigger kind emitted by repository.repositoryFreshnessGenerationToMap (push, delta, or snapshot)."},
                        "is_delta": {"type": "boolean", "description": "Whether the generation is a delta, emitted by repository.repositoryFreshnessGenerationToMap."},
                        "activated_at": {"type": "string", "nullable": true, "description": "Activation time emitted by repository.repositoryFreshnessGenerationToMap (null when never activated)."}
                      },
                      "required": ["id", "status", "trigger_kind", "is_delta"]
                    },
                    "stages": {
                      "type": "object",
                      "description": "Per-stage build completion for the resolved generation, emitted by the repository freshness handler from repositoryFreshnessStagesToMap.",
                      "properties": {
                        "collected": {"type": "boolean", "description": "Collection completed, emitted by repository.repositoryFreshnessStagesToMap."},
                        "reduced": {"type": "boolean", "description": "Reduction completed, emitted by repository.repositoryFreshnessStagesToMap."},
                        "projected": {"type": "boolean", "description": "Projection completed, emitted by repository.repositoryFreshnessStagesToMap."},
                        "materialized": {"type": "boolean", "description": "Materialization completed, emitted by repository.repositoryFreshnessStagesToMap."}
                      },
                      "required": ["collected", "reduced", "projected", "materialized"]
                    },
                    "outstanding_by_stage": {
                      "type": "array",
                      "description": "Outstanding work grouped by stage, emitted by the repository freshness handler from repositoryFreshnessOutstandingToSlice.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "stage": {"type": "string", "description": "Pipeline stage emitted by repository.repositoryFreshnessOutstandingToSlice."},
                          "status": {"type": "string", "description": "Work status emitted by repository.repositoryFreshnessOutstandingToSlice."},
                          "count": {"type": "integer", "description": "Outstanding units emitted by repository.repositoryFreshnessOutstandingToSlice."}
                        },
                        "required": ["stage", "status", "count"]
                      }
                    },
                    "shared_enrichment": {
                      "type": "object",
                      "description": "Cross-repo materialization backlog referencing this generation, emitted by the repository freshness handler from repositoryFreshnessSharedEnrichmentToMap.",
                      "properties": {
                        "pending": {"type": "boolean", "description": "Whether shared enrichment is pending, emitted by repository.repositoryFreshnessSharedEnrichmentToMap."},
                        "pending_domains": {
                          "type": "array",
                          "description": "Pending enrichment domains, emitted by repository.repositoryFreshnessSharedEnrichmentToMap.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "domain": {"type": "string", "description": "Enrichment domain emitted by repository.repositoryFreshnessSharedEnrichmentToMap."},
                              "count": {"type": "integer", "description": "Pending units in this domain emitted by repository.repositoryFreshnessSharedEnrichmentToMap."}
                            },
                            "required": ["domain", "count"]
                          }
                        }
                      },
                      "required": ["pending", "pending_domains"]
                    },
                    "unobserved_push": {
                      "type": "object",
                      "nullable": true,
                      "description": "Push received but not yet observed in a generation, emitted by the repository freshness handler from repositoryFreshnessUnobservedPushToMap (null when every push is observed).",
                      "properties": {
                        "target_sha": {"type": "string", "description": "Pushed commit SHA emitted by repository.repositoryFreshnessUnobservedPushToMap."},
                        "ref": {"type": "string", "description": "Pushed ref emitted by repository.repositoryFreshnessUnobservedPushToMap."},
                        "received_at": {"type": "string", "nullable": true, "description": "Push receipt time emitted by repository.repositoryFreshnessUnobservedPushToMap (null when unknown)."}
                      },
                      "required": ["target_sha", "ref"]
                    },
                    "retirement": {
                      "type": "object",
                      "nullable": true,
                      "description": "Bounded retirement tombstone evidence (#7766): the tombstone state and row for retiring and retired verdicts; null otherwise.",
                      "properties": {
                        "state": {"type": "string", "description": "Tombstone state (pending, running, blocked, repairing_graph, complete, failed)."},
                        "retirement_id": {"type": "string", "description": "Tombstone key."}
                      }
                    },
                    "selection": {
                      "type": "object",
                      "description": "Collector selection evidence from the scope's live selector observations (#7625). Always present; state is unknown, with live_selector_count 0 and null fields, when no selector row is live. When reason is not_listed, classification and successor_slug carry the upstream probe verdict (#8454).",
                      "properties": {
                        "state": {"type": "string", "enum": ["selected", "not_selected", "pending_confirmation", "excluded_still_ingested", "unknown"], "description": "selected when any live selector selects the scope; not_selected when no live selector selects it, every live observation is confirmed exclusion evidence, and no generation was observed after state_since; excluded_still_ingested when the exclusion is confirmed but a generation was observed after state_since; pending_confirmation when no live selector selects the scope but an observation is not yet confirmed; unknown when no selector row is live."},
                        "reason": {"type": "string", "nullable": true, "enum": ["not_listed", "archived_excluded", "rule_excluded", null], "description": "Null when selected or unknown; otherwise the state of the most recently evaluated live observation."},
                        "state_since": {"type": "string", "format": "date-time", "nullable": true, "description": "When selected, the earliest time a live selector began selecting the scope; otherwise the latest time a live observation entered its current state. Null when unknown."},
                        "last_listed_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Newest time a deciding selector's listing named the repository; null if never."},
                        "evaluated_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Newest evaluation time among the deciding observations."},
                        "live_selector_count": {"type": "integer", "description": "Number of selectors with a live observation of the scope."},
                        "classification": {"type": "string", "nullable": true, "enum": ["transferred", "renamed", "still_present", "not_found_or_inaccessible", null], "description": "Upstream probe verdict for a Confirmed not_listed scope (#8454); null otherwise."},
                        "successor_slug": {"type": "string", "nullable": true, "description": "Probe full_name for a renamed or transferred verdict (#8454); null otherwise."},
                        "successor_selected": {"type": "boolean", "description": "Whether the successor slug resolves to a selected scope, decided at read time (#8454)."}
                      }
                    },
                    "as_of": {"type": "string", "description": "Read time of this verdict, emitted by the repository freshness handler."},
                    "scoped": {"type": "boolean", "description": "Whether the caller is scope-restricted, emitted by the repository freshness handler from the access filter."}
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
