// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// Exposure documents the code-to-cloud reachability routes (epic
// #2704). It is a separate const file so the impact path spec stays within the
// repo line cap. The leading comma joins it after Routes in the
// concatenated paths object.
const Exposure = `
    "/api/v0/impact/trace-exposure-path": {
      "post": {
        "tags": ["impact"],
        "summary": "Trace code-to-cloud exposure path",
        "description": "Traces bounded reachability from an internet-exposed handler source through CALLS edges (and, when materialized, code-to-cloud bridge edges) to a cloud sink from the curated catalog. Findings are derived (symbol-level reachability, not value-flow) and use the conservative truth-state vocabulary (exact/partial/ambiguous/unresolved). Never fabricates a path: when a bridge edge is not materialized the cloud-sink segment is reported unresolved. The walk is bounded: max_depth defaults to 5 and is clamped to 1-10, and at most 25 paths are returned. Scoped tokens receive the same shape filtered to their grant (#5167): the source handler must live in a granted repository (a foreign source, by id or by name, renders as not found and is never walked); every chain Function must carry a granted repo_id; a SqlTable or ShellCommand sink must carry a granted repo_id (a shared-uid ShellCommand last written by another tenant is dropped); a CloudResource sink must be USED by a granted WorkloadInstance; and SecretsIAMSecretMetadataPath and CidrBlock sinks are always withheld, which coverage.unresolved_reason names on every scoped response. A path failing any check is dropped whole. coverage.truncated is computed from the raw row count before the filter, and is also true when the ownership budget left nodes unchecked. The 403 remains for browser sessions refused by policy.",
        "operationId": "traceExposurePath",
        "x-scoped-token-support": true,
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
                    "scoped": {"type": "boolean", "description": "Present and true only for a scoped caller: the response is filtered to the caller's grant."},
                    "withheld_sections": {"type": "array", "items": {"type": "string"}, "description": "Scoped callers only. Static per route, returned whether or not anything was withheld: paths_through_ungranted_nodes (a path crossing any node the grant does not own is dropped whole) and, for exposure, unowned_sink_classes."},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
