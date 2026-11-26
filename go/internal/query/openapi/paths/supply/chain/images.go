// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// Images is the OpenAPI path fragment documenting the `/api/v0/images` route.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Images = `
    "/api/v0/images": {
      "get": {
        "tags": ["images"],
        "summary": "List container images (OCI)",
        "description": "Lists container images observed by the OCI registry collector over the authoritative (:ContainerImage) graph. The list is bounded by limit+1 with deterministic ordering by digest then uid, and exposes offset-based continuation via next_cursor when truncated. Optional filters narrow on digest, registry/repository_id, and tag. This is the console Images browse surface; source-to-image provenance is exposed by the supply-chain container image identity endpoints. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; a 503 without Retry-After means a configuration state, such as no graph backend configured on this API instance. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.container_image_list in its error envelope.",
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
                      "description": "Container image rows observed by the OCI registry collector over the authoritative graph, emitted by listImages (ImageRow).",
                      "items": {
                        "type": "object", "description": "One image row. emitted by listImages (ImageRow).",
                        "properties": {
                          "id": {"type": "string", "description": "Image node id emitted by listImages (ImageRow.ID)."},
                          "digest": {"type": "string", "description": "Image digest emitted by listImages (ImageRow.Digest)."},
                          "repository_id": {"type": "string", "description": "OCI repository id emitted by listImages (ImageRow.RepositoryID)."},
                          "registry": {"type": "string", "description": "Registry host emitted by listImages (ImageRow.Registry)."},
                          "repository": {"type": "string", "description": "Repository path emitted by listImages (ImageRow.Repository)."},
                          "name": {"type": "string", "description": "Image name emitted by listImages (ImageRow.Name)."},
                          "tag": {"type": "string", "description": "Source tag observed for the image, emitted by listImages (ImageRow.Tag)."},
                          "media_type": {"type": "string", "description": "Media type emitted by listImages (ImageRow.MediaType)."},
                          "artifact_type": {"type": "string", "description": "Artifact type emitted by listImages (ImageRow.ArtifactType)."},
                          "config_digest": {"type": "string", "description": "Config digest emitted by listImages (ImageRow.ConfigDigest)."},
                          "size_bytes": {"type": "integer", "description": "Image size in bytes emitted by listImages (ImageRow.SizeBytes)."},
                          "source_system": {"type": "string", "description": "Source system emitted by listImages (ImageRow.SourceSystem)."}
                        },
                        "required": ["id"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listImages."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listImages."},
                    "offset": {"type": "integer", "description": "Requested row offset echoed by listImages."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listImages."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Offset continuation emitted by listImages only when truncated; absent otherwise.",
                      "properties": {
                        "offset": {"type": "integer", "description": "Next row offset, emitted by listImages as offset plus limit."}
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
