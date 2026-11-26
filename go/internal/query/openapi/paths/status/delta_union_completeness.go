// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// DeltaUnionCompleteness is the OpenAPI fragment for the delta-union graph
// completeness endpoint. openapi.Spec concatenates it after Admin; keep it in
// lockstep with its handler and the HTTP API reference.
const DeltaUnionCompleteness = `
    "/api/v0/admin/delta-union/completeness": {
      "get": {
        "tags": ["admin"],
        "summary": "Check delta-union graph completeness",
        "description": "Read-only prod completeness check (#7797): per repository, the graph's file set against the ACTIVE set (base-full overlaid with the active delta). Repeat the scope query parameter to name scopes; at least one scope is required (an empty filter fans out and is rejected). Past the repository cap the call fails loudly instead of paging silently. A mismatch names the diff and the generations to re-parse; it never repairs. A graph read that hits a graph outage answers 503 with Retry-After and the fixed detail of the shared graph-unavailable verdict (safe to retry), and one that exceeds its graph deadline answers 504 with the fixed detail of the shared deadline verdict; both keep the {status, detail} body, and a caller that sends Accept application/eshu.envelope+json gets the shared error envelope, which names capability operator.delta_union_completeness.check. A 503 without Retry-After means the check is not configured on the instance. Any other failed ACTIVE or graph read answers 500 with the fixed detail \"delta-union completeness check failed\" (499 when the client canceled); a filter past the repository cap answers 500 with the narrow-the-filter text. No 5xx body carries backend error text.",
        "parameters": [
          {"name": "scope", "in": "query", "description": "Scope id filter, repeatable, at least one required.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Completeness report: one entry per checked repository with outcome complete, mismatch, or skipped.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Delta-union completeness report sorted by repository id. Owner: recovery.DeltaUnionCompletenessReport served by admin.deltaUnionCompleteness.",
                  "required": ["repos", "mismatches"],
                  "properties": {
                    "repos": {
                      "type": "array",
                      "description": "One completeness entry per checked repository. Owner: recovery.DeltaUnionCompletenessReport Repos.",
                      "items": {
                        "type": "object",
                        "description": "One repository's completeness result. Owner: recovery.DeltaUnionRepoReport.",
                        "required": ["repo_id", "scope_id", "outcome", "base", "active", "graph_files", "active_files"],
                        "properties": {
                          "repo_id": {"type": "string", "description": "Checked repository id. Owner: recovery.DeltaUnionRepoReport RepoID."},
                          "scope_id": {"type": "string", "description": "Ingestion scope id of the repository. Owner: recovery.DeltaUnionRepoReport ScopeID."},
                          "outcome": {"type": "string", "enum": ["complete", "mismatch", "skipped"], "description": "Completeness verdict. Owner: recovery.DeltaUnionRepoOutcome."},
                          "base": {
                            "type": "object",
                            "description": "Union-base generation behind the verdict. Owner: recovery.DeltaUnionRepoReport Base.",
                            "required": ["GenerationID", "Status", "IsDelta"],
                            "properties": {
                              "GenerationID": {"type": "string", "description": "Base generation name, empty when absent. Owner: recovery.DeltaUnionGeneration GenerationID."},
                              "Status": {"type": "string", "description": "Base generation status. Owner: recovery.DeltaUnionGeneration Status."},
                              "IsDelta": {"type": "boolean", "description": "Whether the base carries only file-scoped changes. Owner: recovery.DeltaUnionGeneration IsDelta."}
                            }
                          },
                          "active": {
                            "type": "object",
                            "description": "Active generation behind the verdict. Owner: recovery.DeltaUnionRepoReport Active.",
                            "required": ["GenerationID", "Status", "IsDelta"],
                            "properties": {
                              "GenerationID": {"type": "string", "description": "Active generation name, empty when absent. Owner: recovery.DeltaUnionGeneration GenerationID."},
                              "Status": {"type": "string", "description": "Active generation status. Owner: recovery.DeltaUnionGeneration Status."},
                              "IsDelta": {"type": "boolean", "description": "Whether the active generation carries only file-scoped changes. Owner: recovery.DeltaUnionGeneration IsDelta."}
                            }
                          },
                          "graph_files": {"type": "integer", "description": "File count on the GRAPH side. Owner: recovery.DeltaUnionRepoReport GraphFiles."},
                          "active_files": {"type": "integer", "description": "File count on the ACTIVE side. Owner: recovery.DeltaUnionRepoReport ActiveFiles."},
                          "missing": {"type": "array", "items": {"type": "string"}, "description": "ACTIVE paths absent from the graph; present only on a mismatch. Owner: recovery.DeltaUnionRepoReport Missing."},
                          "extra": {"type": "array", "items": {"type": "string"}, "description": "Graph paths absent from ACTIVE; present only on a mismatch. Owner: recovery.DeltaUnionRepoReport Extra."},
                          "drifted": {"type": "array", "items": {"type": "string"}, "description": "Paths whose entity counts disagree; present only on a mismatch. Owner: recovery.DeltaUnionRepoReport Drifted."},
                          "annotation": {"type": "string", "description": "Generations behind the verdict, for repair. Owner: recovery.DeltaUnionRepoReport Annotation."}
                        }
                      }
                    },
                    "mismatches": {"type": "integer", "description": "Repositories with a mismatch outcome. Owner: recovery.DeltaUnionCompletenessReport Mismatches."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {
            "description": "A graph outage (transient, safe to retry, with Retry-After), or the completeness check is not configured on this API instance (no Retry-After). The body is {status, detail}; no backend error text.",
            "headers": {"Retry-After": {"description": "Seconds to wait before retrying. Present only on the graph-outage verdict.", "schema": {"type": "integer", "minimum": 1}}}
          },
          "504": {"description": "The graph read exceeded its deadline. The body is {status, detail} with the fixed deadline text; no backend error text."}
        }
      }
    },
`
