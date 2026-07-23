// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsTagHistory = `
    "/api/v0/images/tag-history": {
      "get": {
        "tags": ["images"],
        "summary": "List one image_ref's captured tag-mutation history (OCI)",
        "description": "Lists the bounded, ordered ContainerImageTagObservation history captured for one repository_id+tag over the authoritative graph (issue #5459): what digest the tag was first observed as, and the order its digests changed. Anchored on the existing container_image_tag_observation_ref index over image_ref, which the API composes server-side from repository_id and tag. The list is bounded by limit+1 with deterministic ordering by first_observed_at then uid, and exposes offset-based continuation via next_cursor when truncated. A tag that flips back to a previously observed digest collapses onto the same observation node, and first_observed_at is a set-once value that holds the first projected observation rather than a full chronological event log; see TagHistoryHandler's doc comment for both limitations.",
        "operationId": "listContainerImageTagHistory",
        "parameters": [
          {"name": "repository_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "OCI repository id such as oci-registry://host/path. Required; must carry the oci-registry:// prefix."},
          {"name": "tag", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Tag observed for the image, such as 1.0.0. Required."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "description": "Maximum tag-observation rows per page (1..200, default 50)."},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Row offset for continuation; use next_cursor.offset from a truncated page."}
        ],
        "responses": {
          "200": {
            "description": "Container image tag-observation history rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "tag_history": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "tag": {"type": "string"},
                          "resolved_digest": {"type": "string"},
                          "previous_digest": {"type": "string"},
                          "mutated": {"type": "boolean"},
                          "first_observed_at": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "identity_strength": {"type": "string"}
                        },
                        "required": ["tag", "resolved_digest", "mutated", "repository_id"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "image_ref": {"type": "string"},
                    "repository_id": {"type": "string"},
                    "tag": {"type": "string"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "offset": {"type": "integer"}
                      },
                      "required": ["offset"]
                    }
                  },
                  "required": ["tag_history", "count", "limit", "offset", "truncated", "image_ref", "repository_id", "tag"]
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
