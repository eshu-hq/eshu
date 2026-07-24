// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsImages = `
    "/api/v0/images": {
      "get": {
        "tags": ["images"],
        "summary": "List container images (OCI)",
        "description": "Lists container images observed by the OCI registry collector over the authoritative (:ContainerImage) graph. The list is bounded by limit+1 with deterministic ordering by digest then uid, and exposes offset-based continuation via next_cursor when truncated. Optional filters narrow on digest, registry/repository_id, and tag. This is the console Images browse surface; source-to-image provenance is exposed by the supply-chain container image identity endpoints.",
        "operationId": "listContainerImages",
        "parameters": [
          {"name": "digest", "in": "query", "schema": {"type": "string"}, "description": "Exact image digest such as sha256:... to anchor lookup."},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "OCI repository id such as oci-registry://host/path to filter by registry repository."},
          {"name": "tag", "in": "query", "schema": {"type": "string"}, "description": "Source tag observed for the image."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "description": "Maximum images per page (1..200, default 50)."},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Row offset for continuation; use next_cursor.offset from a truncated page."}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Container image rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "images": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "digest": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "registry": {"type": "string"},
                          "repository": {"type": "string"},
                          "name": {"type": "string"},
                          "tag": {"type": "string"},
                          "media_type": {"type": "string"},
                          "artifact_type": {"type": "string"},
                          "config_digest": {"type": "string"},
                          "size_bytes": {"type": "integer"},
                          "source_system": {"type": "string"}
                        },
                        "required": ["id"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "offset": {"type": "integer"}
                      },
                      "required": ["offset"]
                    }
                  },
                  "required": ["images", "count", "limit", "offset", "truncated"]
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
