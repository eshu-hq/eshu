// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// ContainerImages is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/container-images/identities` route. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const ContainerImages = `
    "/api/v0/supply-chain/container-images/identities": {
      "get": {
        "summary": "List container image identities",
        "description": "Lists reducer-owned container image identity rows. At least one anchor (digest, image_ref, source_repository_id, repository_id, or outcome) is required; a request with none returns 400. outcome must be exact_digest or tag_resolved when supplied.",
        "operationId": "listContainerImageIdentities",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "OCI/image repository identity observed for the container image. This is not a source repository selector.", "schema": {"type": "string"}},
          {"name": "source_repository_id", "in": "query", "description": "Source repository id or selector used to read reducer-owned source-to-image bridge evidence. This does not reinterpret the OCI/image repository_id field.", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact_digest", "tag_resolved"]}},
          {"name": "after_identity_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Container image identity page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "identities": {
                      "type": "array",
                      "description": "Reducer-owned container image identity rows emitted by listContainerImageIdentities (ContainerImageIdentityResult).",
                      "items": {
                        "type": "object", "description": "One container image identity row. emitted by listContainerImageIdentities (ContainerImageIdentityResult).",
                        "properties": {
                          "identity_id": {"type": "string", "description": "Identity id emitted by listContainerImageIdentities (ContainerImageIdentityResult.IdentityID)."},
                          "digest": {"type": "string", "description": "Image digest emitted by listContainerImageIdentities (ContainerImageIdentityResult.Digest)."},
                          "image_ref": {"type": "string", "description": "Image reference emitted by listContainerImageIdentities (ContainerImageIdentityResult.ImageRef)."},
                          "repository_id": {"type": "string", "description": "OCI/image repository identity emitted by listContainerImageIdentities (ContainerImageIdentityResult.RepositoryID)."},
                          "source_repository_ids": {"type": "array", "items": {"type": "string"}, "description": "Source repository ids behind the identity, emitted by listContainerImageIdentities (ContainerImageIdentityResult.SourceRepositoryIDs)."},
                          "source_revision": {"type": "string", "description": "Source revision emitted by listContainerImageIdentities (ContainerImageIdentityResult.SourceRevision)."},
                          "source_revision_provenance": {"type": "string", "description": "Where the source revision came from (oci_config_source_label or ci_run_commit), emitted by listContainerImageIdentities (ContainerImageIdentityResult.SourceRevisionProvenance)."},
                          "workload_ids": {"type": "array", "items": {"type": "string"}, "description": "Workload ids emitted by listContainerImageIdentities (ContainerImageIdentityResult.WorkloadIDs)."},
                          "service_ids": {"type": "array", "items": {"type": "string"}, "description": "Service ids emitted by listContainerImageIdentities (ContainerImageIdentityResult.ServiceIDs)."},
                          "outcome": {"type": "string", "description": "Identity outcome (exact_digest, tag_resolved) emitted by listContainerImageIdentities (ContainerImageIdentityResult.Outcome)."},
                          "reason": {"type": "string", "description": "Outcome reason emitted by listContainerImageIdentities (ContainerImageIdentityResult.Reason)."},
                          "identity_strength": {"type": "string", "description": "Identity strength emitted by listContainerImageIdentities (ContainerImageIdentityResult.IdentityStrength)."},
                          "canonical_id": {"type": "string", "description": "Canonical identity id emitted by listContainerImageIdentities (ContainerImageIdentityResult.CanonicalID)."},
                          "canonical_writes": {"type": "integer", "description": "Canonical write count emitted by listContainerImageIdentities (ContainerImageIdentityResult.CanonicalWrites)."},
                          "source_layers": {"type": "array", "items": {"type": "string"}, "description": "Source layers emitted by listContainerImageIdentities (ContainerImageIdentityResult.SourceLayers)."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids emitted by listContainerImageIdentities (ContainerImageIdentityResult.EvidenceFactIDs)."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence gaps emitted by listContainerImageIdentities (ContainerImageIdentityResult.MissingEvidence)."},
                          "source_freshness": {"type": "string", "description": "Source freshness signal emitted by listContainerImageIdentities (ContainerImageIdentityResult.SourceFreshness)."},
                          "source_confidence": {"type": "string", "description": "Source confidence signal emitted by listContainerImageIdentities (ContainerImageIdentityResult.SourceConfidence)."}
                        },
                        "required": ["identity_id", "outcome", "canonical_writes"]
                      }
                    },
                    "source_bridge": {
                      "type": "object",
                      "description": "Source-repository-scoped identity evidence emitted by listContainerImageIdentities (ContainerImageIdentitySourceBridge); present only when the source_repository_id filter was given. Arrays are absent when empty.",
                      "properties": {
                        "source_repository_id": {"type": "string", "description": "Source repository id emitted by listContainerImageIdentities (ContainerImageIdentitySourceBridge.SourceRepositoryID)."},
                        "image_repository_ids": {"type": "array", "items": {"type": "string"}, "description": "Image repository ids emitted by listContainerImageIdentities (ContainerImageIdentitySourceBridge.ImageRepositoryIDs); absent when empty."},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence gaps emitted by listContainerImageIdentities (ContainerImageIdentitySourceBridge.MissingEvidence); absent when empty."},
                        "warnings": {"type": "array", "items": {"type": "string"}, "description": "Bridge warnings emitted by listContainerImageIdentities (ContainerImageIdentitySourceBridge.Warnings); absent when empty."}
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listContainerImageIdentities."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listContainerImageIdentities."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listContainerImageIdentities."},
                    "next_cursor": {"type": "object", "description": "Keyset cursor emitted by listContainerImageIdentities only when truncated carries the last identity id forward; absent otherwise.", "properties": {
                      "after_identity_id": {"type": "string", "description": "Identity id of the last row, emitted by listContainerImageIdentities for the next page cursor."}
                    }},
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
