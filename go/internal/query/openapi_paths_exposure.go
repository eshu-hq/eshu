// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsExposure documents the code-to-cloud reachability routes (epic
// #2704). It is a separate const file so the impact path spec stays within the
// repo line cap. The leading comma joins it after openAPIPathsImpact in the
// concatenated paths object.
const openAPIPathsExposure = `
    "/api/v0/impact/trace-exposure-path": {
      "post": {
        "tags": ["impact"],
        "summary": "Trace code-to-cloud exposure path",
        "description": "Traces bounded reachability from an internet-exposed handler source through CALLS edges (and, when materialized, code-to-cloud bridge edges) to a cloud sink from the curated catalog. Findings are derived (symbol-level reachability, not value-flow) and use the conservative truth-state vocabulary (exact/partial/ambiguous/unresolved). Never fabricates a path: when a bridge edge is not materialized the cloud-sink segment is reported unresolved.",
        "operationId": "traceExposurePath",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "source": {"type": "string", "description": "Source handler entity name (resolved within repo_id)"},
                  "source_entity_id": {"type": "string", "description": "Source handler entity id (preferred when known)"},
                  "repo_id": {"type": "string", "description": "Repository id scoping source resolution by name"},
                  "max_depth": {"type": "integer", "default": 5, "minimum": 1, "maximum": 10}
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Exposure finding",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "source": {"type": "object"},
                    "source_kind": {"type": "string"},
                    "exposure_rank": {"type": "string", "enum": ["internet_exposed", "network_reachable", "internal"]},
                    "truth_label": {"type": "string", "enum": ["derived"]},
                    "state": {"type": "string", "enum": ["exact", "partial", "ambiguous", "unresolved"]},
                    "paths": {"type": "array", "items": {"type": "object"}},
                    "coverage": {"type": "object"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
