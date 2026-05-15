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
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the bounded raw candidate scan reached candidate_scan_limit before exhausting candidates."},
                    "suppressed_truncated": {"type": "boolean", "description": "True when suppressed modeled-root examples exceeded the bounded suppressed bucket."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "Next active-candidate offset to request when truncated is true; null when the page is complete."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the investigation may inspect before policy exclusions."},
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
