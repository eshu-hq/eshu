// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// RuntimeDeclarations is the OpenAPI path fragment documenting the
// `/api/v0/runtime-declarations/query` route (#8817). openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handler in
// go/internal/query/runtime/declaration and docs/public/reference/http-api.md.
const RuntimeDeclarations = `
    "/api/v0/runtime-declarations/query": {
      "post": {
        "tags": ["repositories"],
        "summary": "List declared runtime versions across repositories",
        "description": "Lists the runtime versions repositories declare in pin files (.nvmrc, .tool-versions, rust-toolchain), package.json engines, volta and packageManager, and the go.mod go and toolchain directives, from the RuntimeDeclaration graph nodes. There is no effective-version view: every declaration is returned with its scope_dir, so a root .nvmrc and a services/web/.nvmrc are two rows. Rows are ordered by (repo_id, relative_path, line_number, runtime, order_index) and paged with the opaque cursor. major_below and version_below compare the declared version within one runtime (runtime is required) and return only the declarations they can place: exact and partial specs. Range, alias and unresolved specs, and partial specs too imprecise for a version_below bound, are excluded from the comparison and named in partial_reasons, with their counts in excluded_from_comparison. Scoped tokens receive only declarations of their granted repositories; repo_ids narrows within the grant.",
        "operationId": "listRuntimeVersionDeclarations",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "additionalProperties": false,
                "properties": {
                  "runtime": {"type": "string", "maxLength": 128, "description": "Exact runtime name, such as node, python or go. At most 128 characters."},
                  "major_below": {"type": "integer", "minimum": 0, "description": "Return declarations whose resolved major is below this value. Requires runtime. Mutually exclusive with version_below."},
                  "version_below": {"type": "string", "pattern": "^[0-9]{1,9}(\\.[0-9]{1,9}){0,2}$", "description": "Return declarations whose resolved version is below major, major.minor or major.minor.patch. Requires runtime. Mutually exclusive with major_below."},
                  "source_kind": {"type": "string", "maxLength": 128, "description": "Exact declaring source, such as nvmrc, tool_versions, rust_toolchain, package_json_engines, package_json_volta, package_json_package_manager, go_mod_go, go_mod_toolchain, dockerfile_from or workflow_setup_version. At most 128 characters."},
                  "repo_ids": {"type": "array", "maxItems": 100, "items": {"type": "string", "maxLength": 512}, "description": "Restrict to these repository ids (at most 100, each at most 512 characters)."},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50, "description": "Maximum declarations to return."},
                  "cursor": {"type": "string", "description": "Opaque next_cursor from a prior page."}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded page of declared runtime versions",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "declarations": {
                      "type": "array",
                      "description": "Bounded page of declared runtime versions, emitted by the declaration handler from the RuntimeDeclaration graph nodes as declaration rows.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "repo_id": {"type": "string", "description": "Repository identity emitted by the declaration row."},
                          "repo_name": {"type": "string", "description": "Repository name emitted by the declaration row. Omitted when the name read degraded; see partial_reasons."},
                          "relative_path": {"type": "string", "description": "Declaring file path emitted by the declaration row."},
                          "scope_dir": {"type": "string", "description": "Directory the declaration governs, emitted by the declaration row; . at the repository root."},
                          "line_number": {"type": "integer", "description": "Declaring line number emitted by the declaration row."},
                          "order_index": {"type": "integer", "description": "Declaration order within the file, emitted by the declaration row."},
                          "runtime": {"type": "string", "description": "Runtime name emitted by the declaration row."},
                          "source_kind": {"type": "string", "description": "Declaring source kind emitted by the declaration row."},
                          "tool_name": {"type": "string", "nullable": true, "description": "Tool name emitted by the declaration row (present when the source names one)."},
                          "raw_spec": {"type": "string", "description": "Declared version text as written, emitted by the declaration row."},
                          "spec_kind": {"type": "string", "description": "Spec kind emitted by the declaration row: exact, partial, range, alias or unresolved. Readers must tolerate a new value."},
                          "version_major": {"type": "integer", "nullable": true, "description": "Resolved major version emitted by the declaration row. Set only for exact and partial specs."},
                          "version_minor": {"type": "integer", "nullable": true, "description": "Resolved minor version emitted by the declaration row (present for exact and partial specs)."},
                          "version_patch": {"type": "integer", "nullable": true, "description": "Resolved patch version emitted by the declaration row (present for exact and partial specs)."},
                          "distribution": {"type": "string", "nullable": true, "description": "Distribution label emitted by the declaration row (present when the source names one)."},
                          "range_lower": {"type": "string", "nullable": true, "description": "Range lower bound text emitted by the declaration row (present for range specs)."},
                          "range_upper": {"type": "string", "nullable": true, "description": "Range upper bound text emitted by the declaration row (present for range specs)."},
                          "range_lower_major": {"type": "integer", "nullable": true, "description": "Range lower major emitted by the declaration row (present for range specs with a comparable bound)."},
                          "range_upper_major": {"type": "integer", "nullable": true, "description": "Range upper major emitted by the declaration row (present for range specs with a comparable bound)."},
                          "range_lower_inclusive": {"type": "boolean", "nullable": true, "description": "Whether the lower bound is inclusive, emitted by the declaration row (present for range specs)."},
                          "range_upper_inclusive": {"type": "boolean", "nullable": true, "description": "Whether the upper bound is inclusive, emitted by the declaration row (present for range specs)."}
                        },
                        "required": ["repo_id", "relative_path", "scope_dir", "line_number", "order_index", "runtime", "source_kind", "raw_spec", "spec_kind", "version_major", "version_minor", "version_patch"]
                      }
                    },
                    "count": {"type": "integer", "description": "Declarations returned in this page, emitted by the declaration handler as len(rows)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page bound, emitted by the declaration handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more declarations exist past this page, emitted by the declaration handler from the lookahead row."},
                    "next_cursor": {"type": "string", "description": "Opaque cursor for the next page, emitted by the declaration handler from the last row. Present only when truncated is true."},
                    "filters": {
                      "type": "object",
                      "description": "The filters as applied, emitted by the declaration handler from request.echo (only non-empty filters appear); version_below echoes the bound that was read.",
                      "properties": {
                        "runtime": {"type": "string", "description": "Runtime filter echoed by the declaration handler (present when the caller set runtime)."},
                        "source_kind": {"type": "string", "description": "Source-kind filter echoed by the declaration handler (present when the caller set source_kind)."},
                        "repo_ids": {"type": "array", "items": {"type": "string"}, "description": "Repository filter echoed by the declaration handler (present when the caller set repo_ids)."},
                        "version_below": {"type": "string", "description": "Comparison bound echoed by the declaration handler (present when the caller set major_below or version_below)."}
                      }
                    },
                    "excluded_from_comparison": {
                      "type": "object",
                      "description": "Comparison exclusion report emitted by the declaration handler from summarizeExcluded as excludedSummary.",
                      "properties": {
                        "total": {"type": "integer", "description": "Declarations the comparison could not place, emitted by declaration.excludedSummary.Total."},
                        "by_spec_kind": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Exclusions grouped by closed spec-kind class, emitted by declaration.excludedSummary.BySpecKind. Counts keyed range, alias, unresolved, partial or other."}
                      },
                      "required": ["total", "by_spec_kind"]
                    },
                    "partial_reasons": {"type": "array", "items": {"type": "string"}, "description": "runtime_declaration_{range|alias|unresolved|partial|other}_specs_excluded when a comparison left declarations out, and runtime_declaration_repository_names_unavailable when repo_name could not be read. Emitted by the declaration handler from the exclusion summary."}
                  },
                  "required": ["declarations", "count", "limit", "truncated", "filters", "excluded_from_comparison", "partial_reasons"]
                }
              }
            }
          },
          "400": {"description": "Invalid filter, limit or cursor, an unknown field, or a version comparison without runtime"},
          "501": {"description": "Capability unsupported for the active query profile"},
          "503": {"description": "Authoritative graph backend unavailable"}
        }
      }
    },`
