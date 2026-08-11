// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsEvidenceBundle documents GET /api/v0/evidence/bundle (#4045).
// It lives in its own file, separate from openapi_paths_evidence.go, only
// because that file is already close to the repository's 500-line cap; the
// route itself belongs to the same "evidence" tag and namespace as its
// siblings there.
const openAPIPathsEvidenceBundle = `
    "/api/v0/evidence/bundle": {
      "get": {
        "tags": ["evidence"],
        "summary": "Get live evidence bundle",
        "description": "Composes and returns a share-safe evidence_bundle.v1 artifact from the same status providers backing GET /api/v0/status/index, GET /api/v0/status/pipeline, and GET /api/v0/status/collectors, so the console and 'eshu evidence bundle export --live' can link to or generate the identical artifact (#4045). The bundle is stack-wide: none of the composed status data carries a repository or tenant selector, so this route carries no scoped-token support and always rejects a scoped-bearer-token caller, the same posture as its two stack-wide source routes. A browser-session caller's admission is policy-dependent: a tenant-bound all-scopes session (the normal single-tenant/local owner console session) is admitted in the default, local, and hosted-single-tenant governance modes, and rejected only in a hosted-multi-tenant or unrecognized mode, or for a restricted-scope session.",
        "operationId": "getLiveEvidenceBundle",
        "responses": {
          "200": {
            "description": "Live evidence_bundle.v1 artifact",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "enum": ["evidence_bundle.v1"]},
                    "bundle_id": {"type": "string"},
                    "identity": {"type": "object", "properties": {
                      "scope_id": {"type": "string"},
                      "profile": {"type": "string"},
                      "created_at": {"type": "string", "format": "date-time"}
                    }},
                    "source": {"type": "object"},
                    "redaction": {"type": "object"},
                    "contents": {"type": "object", "properties": {
                      "pipeline_state": {"type": "object"},
                      "semantic_provider_state": {"type": "object"},
                      "operator_state": {"type": "array", "items": {"type": "object"}}
                    }},
                    "missing_evidence": {"type": "array", "items": {"type": "object"}},
                    "reproduce": {"type": "array", "items": {"type": "object"}},
                    "bounds": {"type": "object"},
                    "validation": {"type": "object", "properties": {
                      "status": {"type": "string", "enum": ["passed"]},
                      "checks": {"type": "array", "items": {"type": "string"}}
                    }}
                  },
                  "required": ["schema_version", "bundle_id", "identity", "redaction", "contents", "reproduce", "bounds", "validation"]
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
