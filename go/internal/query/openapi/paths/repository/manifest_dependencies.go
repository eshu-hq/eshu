// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// ManifestDependencies is the OpenAPI path fragment documenting the
// `/api/v0/repository/manifest-dependencies` route. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handler and docs/public/reference/http-api.md.
const ManifestDependencies = `
    "/api/v0/repository/manifest-dependencies": {
      "get": {
        "tags": ["dependencies"],
        "summary": "List a repository's declared manifest dependencies",
        "description": "Bounded repository-scoped inventory of declared manifest dependencies: package name, declared version or range, manifest file, ecosystem, and the normalized dependency scope (runtime, dev, or test). Runtime dependencies are the default; dev and test dependencies appear only with include_dev_dependencies. The rows are declared, not resolved, and no repository ownership beyond the scope anchor is asserted. Scoped tokens receive results only for a granted repository; an ungranted or unknown repository returns the same empty list as a repository with no manifest. The repository context response carries the same list as a bounded section with a pointer here.",
        "operationId": "listRepositoryManifestDependencies",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Repository scope anchor. Only a granted repository returns rows."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "description": "Maximum rows to return. Defaults to 50, capped at 200."},
          {"name": "include_dev_dependencies", "in": "query", "schema": {"type": "boolean", "default": false}, "description": "Include dev and test dependencies. Defaults to false (runtime dependencies only)."},
          {"name": "after_observed_at", "in": "query", "schema": {"type": "string", "format": "date-time"}, "description": "Keyset cursor observed_at component from a prior next_cursor. Must be sent with after_fact_id."},
          {"name": "after_fact_id", "in": "query", "schema": {"type": "string"}, "description": "Keyset cursor fact_id component from a prior next_cursor. Must be sent with after_observed_at."}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded declared manifest dependency page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository_id": {"type": "string"},
                    "dependencies": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "package_name": {"type": "string"},
                          "declared_range": {"type": "string"},
                          "manifest_file": {"type": "string"},
                          "ecosystem": {"type": "string"},
                          "package_manager": {"type": "string"},
                          "section": {"type": "string"},
                          "scope": {"type": "string", "enum": ["runtime", "dev", "test"]},
                          "fact_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "observed_at": {"type": "string", "format": "date-time"}
                        },
                        "required": ["package_name", "scope", "fact_id"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "include_dev_dependencies": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_observed_at": {"type": "string", "format": "date-time"},
                        "after_fact_id": {"type": "string"}
                      }
                    },
                    "freshness": {
                      "type": "object",
                      "properties": {
                        "scope_found": {"type": "boolean"},
                        "active_generation_id": {"type": "string"},
                        "generation_status": {"type": "string"},
                        "observed_at": {"type": "string", "format": "date-time"},
                        "freshness_state": {"type": "string", "enum": ["fresh", "unavailable"]}
                      },
                      "required": ["scope_found", "freshness_state"]
                    }
                  },
                  "required": ["repository_id", "dependencies", "count", "limit", "truncated", "freshness"]
                }
              }
            }
          },
          "400": {"description": "Invalid limit, cursor, or missing repository anchor"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"description": "Capability unsupported for the active query profile"},
          "503": {"description": "Fact store unavailable or the delta base refused"}
        }
      }
    },`
