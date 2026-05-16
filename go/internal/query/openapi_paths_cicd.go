package query

const openAPIPathsCICD = `
    "/api/v0/ci-cd/run-correlations": {
      "get": {
        "tags": ["ci-cd"],
        "summary": "List CI/CD run correlations",
        "description": "Lists reducer-owned CI/CD run, artifact, and environment correlations. CI success and shell-only hints are not deployment truth; exact rows require explicit artifact identity evidence.",
        "operationId": "listCICDRunCorrelations",
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Repository ID to anchor lookup."},
          {"name": "commit_sha", "in": "query", "schema": {"type": "string"}, "description": "Commit SHA to answer what happened after a commit."},
          {"name": "provider", "in": "query", "schema": {"type": "string"}, "description": "CI/CD provider such as github_actions or gitlab_ci; required when provider_run_id is the only anchor."},
          {"name": "provider_run_id", "in": "query", "schema": {"type": "string"}, "description": "Provider-native run, build, or pipeline ID. Pair with provider when no other bounded anchor is present."},
          {"name": "run_id", "in": "query", "schema": {"type": "string"}, "description": "Alias for provider_run_id."},
          {"name": "artifact_digest", "in": "query", "schema": {"type": "string"}, "description": "Artifact or image digest anchor."},
          {"name": "environment", "in": "query", "schema": {"type": "string"}, "description": "Provider environment observation anchor."},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "rejected"]}},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "CI/CD run correlation rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "correlations": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "correlation_id": {"type": "string"},
                          "provider": {"type": "string"},
                          "run_id": {"type": "string"},
                          "run_attempt": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "commit_sha": {"type": "string"},
                          "environment": {"type": "string"},
                          "artifact_digest": {"type": "string"},
                          "image_ref": {"type": "string"},
                          "outcome": {"type": "string"},
                          "reason": {"type": "string"},
                          "provenance_only": {"type": "boolean"},
                          "canonical_writes": {"type": "integer"},
                          "canonical_target": {"type": "string"},
                          "correlation_kind": {"type": "string"},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["correlation_id", "outcome", "provenance_only", "canonical_writes"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_correlation_id": {"type": "string"}
                      },
                      "required": ["after_correlation_id"]
                    }
                  },
                  "required": ["correlations", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
