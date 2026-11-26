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
                    "repository_id": {"type": "string", "description": "Echo of the caller's repository scope anchor, emitted by the manifest handler from the normalized repository id."},
                    "dependencies": {
                      "type": "array",
                      "description": "Bounded declared manifest dependency rows, emitted by the manifest handler from the page as []manifest.ManifestDependencyRow.",
                      "items": {
                        "type": "object",
                        "description": "One declared manifest dependency row. Owner: manifest handler (manifest.ManifestDependencyRow).",
                        "properties": {
                          "package_name": {"type": "string", "description": "Declared package name emitted by manifest.ManifestDependencyRow.PackageName."},
                          "declared_range": {"type": "string", "description": "Declared version or range emitted by manifest.ManifestDependencyRow.DeclaredRange."},
                          "manifest_file": {"type": "string", "description": "Manifest file carrying the declaration, emitted by manifest.ManifestDependencyRow.ManifestFile."},
                          "ecosystem": {"type": "string", "description": "Package ecosystem emitted by manifest.ManifestDependencyRow.Ecosystem."},
                          "package_manager": {"type": "string", "description": "Package manager emitted by manifest.ManifestDependencyRow.PackageManager."},
                          "section": {"type": "string", "description": "Manifest section emitted by manifest.ManifestDependencyRow.Section."},
                          "scope": {"type": "string", "enum": ["runtime", "dev", "test"], "description": "Normalized dependency scope emitted by manifest.ManifestDependencyRow.Scope."},
                          "fact_id": {"type": "string", "description": "Declaring fact identity emitted by manifest.ManifestDependencyRow.FactID."},
                          "generation_id": {"type": "string", "description": "Generation that admitted the declaration, emitted by manifest.ManifestDependencyRow.GenerationID."},
                          "observed_at": {"type": "string", "format": "date-time", "description": "Declaration observation time emitted by manifest.ManifestDependencyRow.ObservedAt."}
                        },
                        "required": ["package_name", "scope", "fact_id"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the manifest handler as len(rows)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the manifest handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the manifest handler from the lookahead row."},
                    "include_dev_dependencies": {"type": "boolean", "description": "Echo of the caller's dev-dependency flag, emitted by the manifest handler from the normalized includeDev."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor for the next page (present only when truncated), emitted by the manifest handler from the last row.",
                      "properties": {
                        "after_observed_at": {"type": "string", "format": "date-time", "description": "Observed_at of the last row, emitted by the manifest handler from the last ManifestDependencyRow.ObservedAt."},
                        "after_fact_id": {"type": "string", "description": "Fact id of the last row, emitted by the manifest handler from the last ManifestDependencyRow.FactID."}
                      },
                      "required": ["after_observed_at", "after_fact_id"]
                    },
                    "freshness": {
                      "type": "object",
                      "description": "Generation state behind this page, emitted by the manifest handler from ManifestDependencyPage.Freshness.",
                      "properties": {
                        "scope_found": {"type": "boolean", "description": "Whether the scope resolved, emitted by manifest.ManifestDependencyFreshness.ScopeFound."},
                        "active_generation_id": {"type": "string", "description": "Active generation identity emitted by manifest.ManifestDependencyFreshness.ActiveGenerationID."},
                        "generation_status": {"type": "string", "description": "Generation status emitted by manifest.ManifestDependencyFreshness.GenerationStatus."},
                        "observed_at": {"type": "string", "format": "date-time", "description": "Freshness observation time emitted by manifest.ManifestDependencyFreshness.ObservedAt."},
                        "freshness_state": {"type": "string", "enum": ["fresh", "unavailable"], "description": "Freshness verdict emitted by manifest.ManifestDependencyFreshness.FreshnessState."}
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
