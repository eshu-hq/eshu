// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsDependencies = `
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
                      "items": {
                        "type": "object",
                        "properties": {
                          "direction": {"type": "string", "enum": ["forward", "reverse"]},
                          "anchor_package_id": {"type": "string"},
                          "anchor_package": {"type": "string"},
                          "anchor_ecosystem": {"type": "string"},
                          "declaring_version": {"type": "string"},
                          "related_package_id": {"type": "string"},
                          "related_package": {"type": "string"},
                          "related_ecosystem": {"type": "string"},
                          "dependency_range": {"type": "string"},
                          "dependency_type": {"type": "string"},
                          "optional": {"type": "boolean"},
                          "edge_id": {"type": "string"}
                        },
                        "required": ["direction", "optional"]
                      }
                    },
                    "direction": {"type": "string", "enum": ["forward", "reverse"]},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_name": {"type": "string"},
                        "after_edge": {"type": "string"}
                      }
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
