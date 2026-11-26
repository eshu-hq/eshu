// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// Dependencies is the OpenAPI path fragment documenting the
// `/api/v0/dependencies` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Dependencies = `
    "/api/v0/dependencies": {
      "get": {
        "tags": ["dependencies"],
        "summary": "List package dependencies (forward and reverse)",
        "description": "Bounded package-native dependency inventory. The forward view (direction=forward) lists what a package depends on; the reverse view (direction=reverse) lists which packages depend on the anchor package. Repository and service ownership are not asserted here; they remain reducer correlation concerns.",
        "operationId": "listDependencies",
        "parameters": [
          {"name": "direction", "in": "query", "schema": {"type": "string", "enum": ["forward", "reverse"], "default": "forward"}, "description": "forward lists dependencies of the anchor package; reverse lists dependents of the anchor package. Defaults to forward."},
          {"name": "package", "in": "query", "schema": {"type": "string"}, "description": "Normalized package name to anchor on. Required when direction=reverse. Optional for forward (omit to browse all forward dependency edges)."},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}, "description": "Restrict to a package ecosystem such as npm or maven."},
          {"name": "after_name", "in": "query", "schema": {"type": "string"}, "description": "Keyset cursor name component from a prior next_cursor. Must be sent with after_edge."},
          {"name": "after_edge", "in": "query", "schema": {"type": "string"}, "description": "Keyset cursor edge component from a prior next_cursor. Must be sent with after_name."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "description": "Maximum rows to return. Defaults to 50, capped at 200."}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded dependency inventory page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "dependencies": {
                      "type": "array",
                      "description": "Bounded package-native dependency edges, emitted by the dependency handler as []dependency.Row.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "direction": {"type": "string", "enum": ["forward", "reverse"], "description": "View direction of this edge emitted by dependency.Row.Direction (forward: related package is the dependency target; reverse: related package is the dependent)."},
                          "anchor_package_id": {"type": "string", "description": "Anchor package identity emitted by dependency.Row.AnchorPackageID (absent when the graph did not materialize a stable identity)."},
                          "anchor_package": {"type": "string", "description": "Anchor package name emitted by dependency.Row.AnchorPackage (absent when the graph did not materialize a stable identity)."},
                          "anchor_ecosystem": {"type": "string", "description": "Anchor package ecosystem emitted by dependency.Row.AnchorEcosystem (absent when the graph did not materialize a stable identity)."},
                          "declaring_version": {"type": "string", "description": "Version that declared the edge emitted by dependency.Row.DeclaringVersion."},
                          "related_package_id": {"type": "string", "description": "Related package identity emitted by dependency.Row.RelatedPackageID (absent when the graph did not materialize a stable identity)."},
                          "related_package": {"type": "string", "description": "Related package name emitted by dependency.Row.RelatedPackage (absent when the graph did not materialize a stable identity)."},
                          "related_ecosystem": {"type": "string", "description": "Related package ecosystem emitted by dependency.Row.RelatedEcosystem (absent when the graph did not materialize a stable identity)."},
                          "dependency_range": {"type": "string", "description": "Declared dependency range emitted by dependency.Row.DependencyRange."},
                          "dependency_type": {"type": "string", "description": "Dependency type emitted by dependency.Row.DependencyType."},
                          "optional": {"type": "boolean", "description": "Whether the edge is optional, emitted by dependency.Row.Optional."},
                          "edge_id": {"type": "string", "description": "Graph edge identity emitted by dependency.Row.EdgeID."}
                        },
                        "required": ["direction", "optional"]
                      }
                    },
                    "direction": {"type": "string", "enum": ["forward", "reverse"], "description": "Echo of the caller's view direction, emitted by the dependency handler from the normalized direction."},
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the dependency handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the dependency handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more edges exist past this page, emitted by the dependency handler from the lookahead row."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor for the next page (present only when truncated), emitted by the dependency handler from the last row.",
                      "properties": {
                        "after_name": {"type": "string", "description": "Cursor name of the last row, emitted by the dependency handler from the last row cursor name."},
                        "after_edge": {"type": "string", "description": "Edge id of the last row, emitted by the dependency handler from the last Row.EdgeID."}
                      },
                      "required": ["after_name", "after_edge"]
                    }
                  },
                  "required": ["dependencies", "direction", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"description": "Invalid direction, limit, cursor, or missing reverse anchor"},
          "501": {"description": "Capability unsupported for the active query profile"},
          "503": {"description": "Authoritative graph backend unavailable"}
        }
      }
    },`
