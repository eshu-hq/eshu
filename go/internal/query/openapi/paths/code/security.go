// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Security is the OpenAPI path fragment documenting the
// `/api/v0/code/security/secrets/investigate` route. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Security = `
    "/api/v0/code/security/secrets/investigate": {
      "post": {
        "tags": ["code", "security"],
        "summary": "Investigate hardcoded secret candidates",
        "description": "Scans indexed content for hardcoded password, token, key, and risky literal candidates. Findings are redacted before they leave the API. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "investigateHardcodedSecrets",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional language filter"},
                  "finding_kinds": {
                    "type": "array",
                    "items": {"type": "string", "enum": ["api_token", "aws_access_key", "password_literal", "private_key", "secret_literal", "slack_token"]}
                  },
                  "include_suppressed": {"type": "boolean", "default": false},
                  "limit": {"type": "integer", "default": 25, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Redacted hardcoded secret investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Redacted hardcoded-secret investigation packet emitted by hardcodedSecretResponse.",
                  "properties": {
                    "scope": {"type": "object", "description": "Investigation scope (hardcodedSecretScope): repo_id and language selectors echoed by hardcodedSecretResponse.",
                      "properties": {
                        "repo_id": {"type": "string", "description": "Repository selector echoed by hardcodedSecretResponse."},
                        "language": {"type": "string", "description": "Language filter echoed by hardcodedSecretResponse."}
                      },
                      "required": ["repo_id", "language"]
                    },
                    "finding_kinds": {"type": "array", "description": "Normalized finding kinds investigated by hardcodedSecretResponse.", "items": {"type": "string"}},
                    "findings": {
                      "type": "array",
                      "description": "Redacted secret candidates (hardcodedSecretFinding rows) returned by hardcodedSecretResponse.",
                      "items": {
                        "type": "object",
                        "description": "One redacted finding: rank, identity, kind, redacted excerpt, suppression, and a source handle.",
                        "properties": {
                          "rank": {"type": "integer", "description": "One-based rank assigned by hardcodedSecretFinding."},
                          "repo_id": {"type": "string", "description": "Repository id reported by hardcodedSecretFinding."},
                          "relative_path": {"type": "string", "description": "Repo-relative file path reported by hardcodedSecretFinding."},
                          "language": {"type": "string", "description": "File language reported by hardcodedSecretFinding."},
                          "line_number": {"type": "integer", "description": "Line number reported by hardcodedSecretFinding."},
                          "finding_kind": {"type": "string", "description": "Secret kind reported by hardcodedSecretFinding."},
                          "confidence": {"type": "string", "description": "Finding confidence reported by hardcodedSecretFinding."},
                          "severity": {"type": "string", "description": "Finding severity reported by hardcodedSecretFinding."},
                          "redacted_excerpt": {"type": "string", "description": "Redacted line excerpt (secret replaced with the redacted marker), per hardcodedSecretFinding."},
                          "suppressed": {"type": "boolean", "description": "Whether the finding was suppressed, per hardcodedSecretFinding."},
                          "suppression_notes": {"type": "array", "description": "Suppression notes reported by hardcodedSecretFinding.", "items": {"type": "string"}},
                          "source_handle": {"type": "object", "description": "Source locator (repo_id, relative_path, start/end line) emitted by hardcodedSecretFinding.",
                            "properties": {
                              "repo_id": {"type": "string", "description": "Repository id of the finding, per hardcodedSecretFinding."},
                              "relative_path": {"type": "string", "description": "Repo-relative path of the finding, per hardcodedSecretFinding."},
                              "start_line": {"type": "integer", "description": "Finding line, per hardcodedSecretFinding."},
                              "end_line": {"type": "integer", "description": "Finding line, per hardcodedSecretFinding."}
                            },
                            "required": ["repo_id", "relative_path", "start_line", "end_line"]
                          }
                        },
                        "required": ["rank", "repo_id", "relative_path", "line_number", "finding_kind", "confidence", "severity", "redacted_excerpt", "suppressed", "suppression_notes", "source_handle"]
                      }
                    },
                    "recommended_next_calls": {"type": "array", "description": "Follow-up calls from hardcodedSecretRecommendedNextCalls: an evidence-citation hydration for the first finding, or a topic-broadening call when no findings exist.", "items": {"type": "object", "additionalProperties": true, "description": "One follow-up call: tool, reason, and args, per hardcodedSecretRecommendedNextCalls."}},
                    "count": {"type": "integer", "description": "Visible findings counted by hardcodedSecretResponse."},
                    "limit": {"type": "integer", "description": "Page bound echoed by hardcodedSecretResponse."},
                    "offset": {"type": "integer", "description": "Page offset echoed by hardcodedSecretResponse."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past the limit, per hardcodedSecretResponse."},
                    "source_backend": {"type": "string", "description": "Backend that served the investigation (postgres_content_store), per hardcodedSecretResponse."},
                    "coverage": {"type": "object", "description": "Read-path coverage for the investigation emitted by hardcodedSecretResponse.",
                      "properties": {
                        "query_shape": {"type": "string", "description": "Investigation query shape, per hardcodedSecretResponse."},
                        "returned_count": {"type": "integer", "description": "Findings returned on this page, per hardcodedSecretResponse."},
                        "suppressed_count": {"type": "integer", "description": "Findings suppressed on this page, per hardcodedSecretResponse."},
                        "include_suppressed": {"type": "boolean", "description": "Whether suppressed findings were included, per hardcodedSecretResponse."},
                        "limit": {"type": "integer", "description": "Page bound echoed in coverage, per hardcodedSecretResponse."},
                        "offset": {"type": "integer", "description": "Page offset echoed in coverage, per hardcodedSecretResponse."},
                        "truncated": {"type": "boolean", "description": "Truncation echoed in coverage, per hardcodedSecretResponse."},
                        "redaction": {"type": "string", "description": "Redaction marker applied to secret values, per hardcodedSecretResponse."},
                        "empty": {"type": "boolean", "description": "True when no findings exist, per hardcodedSecretResponse."},
                        "searched_all_kinds": {"type": "boolean", "description": "True when no finding_kind filter was given, per hardcodedSecretResponse."},
                        "requires_repo_scope": {"type": "boolean", "description": "Whether the read requires a repository scope, per hardcodedSecretResponse."},
                        "read_path": {"type": "string", "description": "Read side table the investigation ran against, per hardcodedSecretResponse."}
                      },
                      "required": ["query_shape", "returned_count", "suppressed_count", "include_suppressed", "limit", "offset", "truncated", "redaction", "empty", "searched_all_kinds", "requires_repo_scope", "read_path"]
                    }
                  },
                  "required": ["scope", "finding_kinds", "findings", "recommended_next_calls", "count", "limit", "offset", "truncated", "source_backend", "coverage"]
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
