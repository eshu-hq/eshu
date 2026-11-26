// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

// Catalog is the OpenAPI path fragment documenting the
// `/api/v0/service-catalog/correlations` route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Catalog = `
    "/api/v0/service-catalog/correlations": {
      "get": {
        "tags": ["service-catalog"],
        "summary": "List service catalog correlations",
        "description": "Lists reducer-owned service catalog ownership and drift correlation rows. Catalog declarations remain provenance until graph, runtime, deployment, or source evidence corroborates them. At least one anchor (scope_id, entity_ref, repository_id, service_id, workload_id, or owner_ref) is required: a request with none answers 400. Unknown selectors answer 404 and ambiguous selectors answer 409.",
        "operationId": "listServiceCatalogCorrelations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "provider", "in": "query", "schema": {"type": "string"}, "description": "Catalog provider such as backstage, opslevel, or cortex."},
          {"name": "entity_ref", "in": "query", "schema": {"type": "string"}, "description": "Provider-native catalog entity reference."},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL) to anchor lookup. Unknown or ambiguous selectors return a selector error instead of an empty page."},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical service ID to anchor lookup."},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical workload ID to anchor lookup."},
          {"name": "owner_ref", "in": "query", "schema": {"type": "string"}, "description": "Provider-native owner reference."},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "stale", "rejected"]}},
          {"name": "drift_status", "in": "query", "schema": {"type": "string"}, "description": "Optional catalog drift status filter."},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Service catalog correlation rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "correlations": {
                      "type": "array",
                      "description": "Reducer-owned service catalog correlation rows emitted by listCorrelations (service.CatalogCorrelationResult).",
                      "items": {
                        "type": "object",
                        "description": "One service catalog correlation row. Owner: listCorrelations (service.CatalogCorrelationResult).",
                        "properties": {
                          "correlation_id": {"type": "string", "description": "Correlation id emitted by listCorrelations (CatalogCorrelationResult.CorrelationID)."},
                          "provider": {"type": "string", "description": "Catalog provider emitted by listCorrelations (CatalogCorrelationResult.Provider)."},
                          "entity_ref": {"type": "string", "description": "Provider-native entity reference emitted by listCorrelations (CatalogCorrelationResult.EntityRef)."},
                          "entity_type": {"type": "string", "description": "Entity type emitted by listCorrelations (CatalogCorrelationResult.EntityType)."},
                          "display_name": {"type": "string", "description": "Display name emitted by listCorrelations (CatalogCorrelationResult.DisplayName)."},
                          "repository_id": {"type": "string", "description": "Repository id emitted by listCorrelations (CatalogCorrelationResult.RepositoryID)."},
                          "service_id": {"type": "string", "description": "Service id emitted by listCorrelations (CatalogCorrelationResult.ServiceID)."},
                          "workload_id": {"type": "string", "description": "Workload id emitted by listCorrelations (CatalogCorrelationResult.WorkloadID)."},
                          "owner_ref": {"type": "string", "description": "Provider-native owner reference emitted by listCorrelations (CatalogCorrelationResult.OwnerRef)."},
                          "lifecycle": {"type": "string", "description": "Lifecycle emitted by listCorrelations (CatalogCorrelationResult.Lifecycle)."},
                          "tier": {"type": "string", "description": "Tier emitted by listCorrelations (CatalogCorrelationResult.Tier)."},
                          "outcome": {"type": "string", "description": "Correlation outcome emitted by listCorrelations (CatalogCorrelationResult.Outcome)."},
                          "reason": {"type": "string", "description": "Outcome reason emitted by listCorrelations (CatalogCorrelationResult.Reason)."},
                          "provenance_only": {"type": "boolean", "description": "Whether the row is provenance only, emitted by listCorrelations (CatalogCorrelationResult.ProvenanceOnly)."},
                          "drift_kind": {"type": "string", "description": "Drift kind emitted by listCorrelations (CatalogCorrelationResult.DriftKind)."},
                          "drift_status": {"type": "string", "description": "Drift status emitted by listCorrelations (CatalogCorrelationResult.DriftStatus)."},
                          "candidate_repository_ids": {"type": "array", "items": {"type": "string"}, "description": "Candidate repository ids emitted by listCorrelations (CatalogCorrelationResult.CandidateRepositoryIDs)."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids emitted by listCorrelations (CatalogCorrelationResult.EvidenceFactIDs)."},
                          "required_anchor_keys": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "Accepted catalog anchor keys that would let a rejected, unresolved, stale, or ambiguous catalog declaration become a durable correlation, emitted by listCorrelations (CatalogCorrelationResult.RequiredAnchorKeys)."
                          }
                        },
                        "required": ["correlation_id", "outcome", "provenance_only"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listCorrelations."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listCorrelations."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listCorrelations."},
                    "missing_evidence": {
                      "type": "array",
                      "description": "Stable missing-evidence classes returned when an anchored read has no matching reducer correlation rows, emitted by listCorrelations (service.CatalogMissingEvidence).",
                      "items": {
                        "type": "object",
                        "description": "One missing-evidence entry. Owner: listCorrelations (service.CatalogMissingEvidence).",
                        "properties": {
                          "class": {"type": "string", "enum": ["repository_service_catalog_correlation", "service_catalog_correlation", "workload_service_catalog_correlation", "entity_service_catalog_correlation", "owner_service_catalog_correlation", "scope_service_catalog_correlation"], "description": "Missing-evidence class emitted by listCorrelations (CatalogMissingEvidence.Class)."},
                          "reason": {"type": "string", "description": "Missing-evidence reason emitted by listCorrelations (CatalogMissingEvidence.Reason)."}
                        },
                        "required": ["class", "reason"]
                      }
                    },
                    "evidence_summary": {
                      "type": "object",
                      "description": "Catalog evidence separate from reducer-owned correlation rows, emitted by listCorrelations (service.CatalogEvidenceSummary).",
                      "properties": {
                        "local_descriptors": {
                          "type": "object",
                          "description": "Repo-local catalog source facts emitted by listCorrelations (service.CatalogLocalDescriptorEvidence).",
                          "properties": {
                            "state": {"type": "string", "enum": ["present", "absent", "not_checked", "unavailable"], "description": "Descriptor state emitted by listCorrelations (CatalogLocalDescriptorEvidence.State)."},
                            "count": {"type": "integer", "description": "Descriptor fact count emitted by listCorrelations (CatalogLocalDescriptorEvidence.Count)."},
                            "providers": {"type": "array", "items": {"type": "string"}, "description": "Descriptor providers emitted by listCorrelations (CatalogLocalDescriptorEvidence.Providers)."},
                            "source_uris": {"type": "array", "items": {"type": "string"}, "description": "Descriptor source URIs emitted by listCorrelations (CatalogLocalDescriptorEvidence.SourceURIs)."},
                            "facts": {
                              "type": "array",
                              "description": "Bounded local descriptor facts emitted by listCorrelations (service.CatalogLocalDescriptorEvidenceFact).",
                              "items": {
                                "type": "object",
                                "description": "One local descriptor fact. Owner: listCorrelations (service.CatalogLocalDescriptorEvidenceFact).",
                                "properties": {
                                  "fact_id": {"type": "string", "description": "Fact id emitted by listCorrelations (CatalogLocalDescriptorEvidenceFact.FactID)."},
                                  "fact_kind": {"type": "string", "description": "Fact kind emitted by listCorrelations (CatalogLocalDescriptorEvidenceFact.FactKind)."},
                                  "provider": {"type": "string", "description": "Provider emitted by listCorrelations (CatalogLocalDescriptorEvidenceFact.Provider)."},
                                  "entity_ref": {"type": "string", "description": "Entity reference emitted by listCorrelations (CatalogLocalDescriptorEvidenceFact.EntityRef)."},
                                  "source_uri": {"type": "string", "description": "Source URI emitted by listCorrelations (CatalogLocalDescriptorEvidenceFact.SourceURI)."}
                                },
                                "required": ["fact_id", "fact_kind"]
                              }
                            },
                            "truncated": {"type": "boolean", "description": "Whether descriptor facts were truncated, emitted by listCorrelations (CatalogLocalDescriptorEvidence.Truncated)."},
                            "reason": {"type": "string", "description": "Descriptor reason emitted by listCorrelations (CatalogLocalDescriptorEvidence.Reason)."}
                          },
                          "required": ["state", "count"]
                        },
                        "external_catalog_confirmation": {
                          "type": "object",
                          "description": "External catalog confirmation emitted by listCorrelations (service.CatalogExternalCatalogEvidence).",
                          "properties": {
                            "state": {"type": "string", "enum": ["present", "missing"], "description": "Confirmation state emitted by listCorrelations (CatalogExternalCatalogEvidence.State)."},
                            "count": {"type": "integer", "description": "Confirmed correlation count emitted by listCorrelations (CatalogExternalCatalogEvidence.Count)."},
                            "truncated": {"type": "boolean", "description": "Whether the confirmation scan was truncated, emitted by listCorrelations (CatalogExternalCatalogEvidence.Truncated)."},
                            "reason": {"type": "string", "description": "Confirmation reason emitted by listCorrelations (CatalogExternalCatalogEvidence.Reason)."}
                          },
                          "required": ["state", "count"]
                        },
                        "reason": {"type": "string", "description": "Summary reason emitted by listCorrelations (CatalogEvidenceSummary.Reason)."}
                      },
                      "required": ["local_descriptors", "external_catalog_confirmation"]
                    },
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor emitted by listCorrelations only when truncated carries the last correlation id forward; absent otherwise.",
                      "properties": {
                        "after_correlation_id": {"type": "string", "description": "Correlation id of the last row, emitted by listCorrelations for the next page cursor."}
                      },
                      "required": ["after_correlation_id"]
                    }
                  },
                  "required": ["correlations", "count", "limit", "truncated", "evidence_summary"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
