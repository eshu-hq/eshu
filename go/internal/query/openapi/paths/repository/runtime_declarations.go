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
                  "runtime": {"type": "string", "description": "Exact runtime name, such as node, python or go."},
                  "major_below": {"type": "integer", "minimum": 0, "description": "Return declarations whose resolved major is below this value. Requires runtime. Mutually exclusive with version_below."},
                  "version_below": {"type": "string", "pattern": "^[0-9]{1,9}(\\.[0-9]{1,9}){0,2}$", "description": "Return declarations whose resolved version is below major, major.minor or major.minor.patch. Requires runtime. Mutually exclusive with major_below."},
                  "source_kind": {"type": "string", "description": "Exact declaring source, such as nvmrc, tool_versions, rust_toolchain, package_json_engines, package_json_volta, package_json_package_manager, go_mod_go or go_mod_toolchain."},
                  "repo_ids": {"type": "array", "maxItems": 100, "items": {"type": "string"}, "description": "Restrict to these repository ids (at most 100)."},
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
                      "items": {
                        "type": "object",
                        "properties": {
                          "repo_id": {"type": "string"},
                          "repo_name": {"type": "string", "description": "Omitted when the name read degraded; see partial_reasons."},
                          "relative_path": {"type": "string"},
                          "scope_dir": {"type": "string", "description": "Directory the declaration governs; . at the repository root."},
                          "line_number": {"type": "integer"},
                          "order_index": {"type": "integer"},
                          "runtime": {"type": "string"},
                          "source_kind": {"type": "string"},
                          "tool_name": {"type": "string", "nullable": true},
                          "raw_spec": {"type": "string"},
                          "spec_kind": {"type": "string", "description": "exact, partial, range, alias or unresolved. Readers must tolerate a new value."},
                          "version_major": {"type": "integer", "nullable": true, "description": "Set only for exact and partial specs."},
                          "version_minor": {"type": "integer", "nullable": true},
                          "version_patch": {"type": "integer", "nullable": true},
                          "distribution": {"type": "string", "nullable": true},
                          "range_lower": {"type": "string", "nullable": true},
                          "range_upper": {"type": "string", "nullable": true},
                          "range_lower_major": {"type": "integer", "nullable": true},
                          "range_upper_major": {"type": "integer", "nullable": true},
                          "range_lower_inclusive": {"type": "boolean", "nullable": true},
                          "range_upper_inclusive": {"type": "boolean", "nullable": true}
                        },
                        "required": ["repo_id", "relative_path", "scope_dir", "line_number", "order_index", "runtime", "source_kind", "raw_spec", "spec_kind", "version_major", "version_minor", "version_patch"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "string", "description": "Present only when truncated is true."},
                    "filters": {"type": "object", "description": "The filters as applied; version_below echoes the bound that was read."},
                    "excluded_from_comparison": {
                      "type": "object",
                      "properties": {
                        "total": {"type": "integer"},
                        "by_spec_kind": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Counts keyed range, alias, unresolved, partial or other."}
                      },
                      "required": ["total", "by_spec_kind"]
                    },
                    "partial_reasons": {"type": "array", "items": {"type": "string"}, "description": "runtime_declaration_{range|alias|unresolved|partial|other}_specs_excluded when a comparison left declarations out, and runtime_declaration_repository_names_unavailable when repo_name could not be read."}
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
