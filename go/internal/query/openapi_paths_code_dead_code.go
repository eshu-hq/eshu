// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCodeDeadCodeInvestigation = `
    "/api/v0/code/dead-code/investigate": {
      "post": {
        "tags": ["code"],
        "summary": "Investigate dead-code candidates",
        "description": "Returns a bounded dead-code investigation packet with repository coverage, language maturity, exactness blockers, cleanup-ready and ambiguous candidate buckets, suppressed modeled roots, source handles, and recommended drill-down calls. JavaScript and TypeScript candidates remain ambiguous until corpus precision evidence proves cleanup safety.",
        "operationId": "investigateDeadCode",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional parser language filter such as go, python, typescript, tsx, javascript, java, rust, c, cpp, csharp, or sql"},
                  "limit": {"type": "integer", "description": "Maximum active candidates to return after policy filtering (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based offset across active candidates for paging.", "default": 0},
                  "exclude_decorated_with": {
                    "type": "array",
                    "description": "Optional list of decorator names to suppress from active candidates.",
                    "items": {"type": "string"}
                  }
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Dead-code investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "display_truncated": {"type": "boolean", "description": "True when active candidates exceeded limit and the displayed buckets were clipped."},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the shared candidate scan limit was reached before all selected labels were exhausted."},
                    "suppressed_truncated": {"type": "boolean", "description": "True when suppressed modeled-root examples exceeded the bounded suppressed bucket."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "Next active-candidate offset to request when truncated is true; null when the page is complete."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the investigation may inspect across all selected candidate labels before policy exclusions."},
                    "candidate_scan_limit_per_label": {"type": "integer", "description": "Maximum share one candidate label may consume from the investigation's shared raw-row limit."},
                    "candidate_scan_pages": {"type": "integer", "description": "Number of raw candidate pages read before returning the investigation packet."},
                    "candidate_scan_rows": {"type": "integer", "description": "Number of raw candidate rows inspected before policy exclusions."},
                    "coverage": {"type": "object", "additionalProperties": true},
                    "candidate_buckets": {"type": "object", "additionalProperties": true},
                    "bucket_counts": {"type": "object", "additionalProperties": true},
                    "root_policy": {"type": "object", "additionalProperties": true},
                    "language_maturity": {"type": "object", "additionalProperties": true},
                    "exactness_blockers": {"type": "object", "additionalProperties": true},
                    "observed_exactness_blockers": {"type": "object", "additionalProperties": true},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "analysis": {"type": "object", "additionalProperties": true}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`

const openAPIPathsCodeCrossRepoDeadCode = `
    "/api/v0/code/dead-code/cross-repo": {
      "post": {
        "tags": ["code"],
        "summary": "Find cross-repo dead-code candidates",
        "description": "Classifies producer repository dead-code candidates against deterministic consumer evidence. Symbols or routes kept live by another repository are returned as live_by_consumer. Ambiguous ownership, stale generations, missing read-model coverage, and scoped-token-hidden consumers are returned as unknown_needs_evidence rather than dead.",
        "operationId": "findCrossRepoDeadCode",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Producer repository selector (canonical ID, name, slug, or path)."},
                  "consumer_repo_ids": {
                    "type": "array",
                    "description": "Optional consumer repository selectors that bound cross-repo liveness evidence.",
                    "items": {"type": "string"}
                  },
                  "language": {"type": "string", "description": "Optional parser language filter."},
                  "limit": {"type": "integer", "description": "Maximum active producer candidates to classify (default 100, max 500).", "default": 100},
                  "exclude_decorated_with": {
                    "type": "array",
                    "description": "Optional decorator names to suppress from active candidates.",
                    "items": {"type": "string"}
                  }
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Cross-repo dead-code classification packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "limit": {"type": "integer"},
                    "consumer_repo_ids": {"type": "array", "items": {"type": "string"}},
                    "query_shape": {"type": "string", "enum": ["bounded_cross_repo_dead_code"]},
                    "truncated": {"type": "boolean"},
                    "display_truncated": {"type": "boolean"},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the shared candidate scan limit was reached before all selected labels were exhausted."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the classification may inspect across all selected candidate labels."},
                    "candidate_scan_limit_per_label": {"type": "integer", "description": "Maximum share one candidate label may consume from the classification's shared raw-row limit."},
                    "candidate_scan_pages": {"type": "integer"},
                    "candidate_scan_rows": {"type": "integer"},
                    "candidate_buckets": {
                      "type": "object",
                      "properties": {
                        "dead": {"type": "array", "items": {"type": "object"}},
                        "live_by_consumer": {"type": "array", "items": {"type": "object"}},
                        "unknown": {"type": "array", "items": {"type": "object"}},
                        "suppressed": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "bucket_counts": {"type": "object", "additionalProperties": true},
                    "analysis": {"type": "object", "additionalProperties": true}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
