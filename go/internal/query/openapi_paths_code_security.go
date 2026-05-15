package query

const openAPIPathsCodeSecurity = `
    "/api/v0/code/security/secrets/investigate": {
      "post": {
        "tags": ["code", "security"],
        "summary": "Investigate hardcoded secret candidates",
        "description": "Scans indexed content for hardcoded password, token, key, and risky literal candidates. Findings are redacted before they leave the API.",
        "operationId": "investigateHardcodedSecrets",
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
          "200": {
            "description": "Redacted hardcoded secret investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "repo_id": {"type": "string"},
                          "relative_path": {"type": "string"},
                          "line_number": {"type": "integer"},
                          "finding_kind": {"type": "string"},
                          "confidence": {"type": "string"},
                          "severity": {"type": "string"},
                          "redacted_excerpt": {"type": "string"},
                          "suppressed": {"type": "boolean"},
                          "suppression_notes": {"type": "array", "items": {"type": "string"}},
                          "source_handle": {"type": "object", "additionalProperties": true}
                        }
                      }
                    },
                    "coverage": {"type": "object", "additionalProperties": true},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
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
