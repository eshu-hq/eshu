// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsCloudInventory documents the canonical multi-cloud resource
// inventory readback route. It surfaces reducer-owned
// reducer_cloud_resource_identity rows (one per cloud_resource_uid) with
// provider, normalized identity, management_origin, evidence-layer flags,
// provider-neutral source state, optional keyed tag fingerprints, and optional
// bounded identity-policy and resource-change freshness evidence. The
// route is read-only, bounded, paginated, and truth-labeled; it never returns
// raw provider locators, raw actors, raw identities, tag values, assignment
// scopes, or credentials.
const openAPIPathsCloudInventory = `
    "/api/v0/cloud/inventory": {
      "get": {
        "summary": "List canonical multi-cloud resource identities (bounded, filterable, paginated, truth-labeled)",
        "operationId": "listCloudResourceInventory",
        "x-scoped-token-support": true,
        "description": "Reads the reducer-owned canonical CloudResource identity rows (reducer_cloud_resource_identity). Filterable by provider (aws/gcp/azure), canonical scope, and management_origin. local_lightweight returns unsupported_capability. Scoped tokens receive rows intersected with the caller's granted repositories/ingestion scopes (fact_records.scope_id); a scoped caller with no grants receives an empty page without a query.",
        "parameters": [
          {"name": "provider", "in": "query", "description": "Filter by cloud provider: aws, gcp, or azure.", "schema": {"type": "string", "enum": ["aws", "gcp", "azure"]}},
          {"name": "scope_id", "in": "query", "description": "Filter by canonical scope id. account_id, project_id, and subscription_id are accepted aliases that target the same canonical scope.", "schema": {"type": "string"}},
          {"name": "account_id", "in": "query", "description": "Alias for scope_id (AWS account scope).", "schema": {"type": "string"}},
          {"name": "project_id", "in": "query", "description": "Alias for scope_id (GCP project scope).", "schema": {"type": "string"}},
          {"name": "subscription_id", "in": "query", "description": "Alias for scope_id (Azure subscription scope).", "schema": {"type": "string"}},
          {"name": "management_origin", "in": "query", "description": "Filter by strongest contributing evidence layer: declared, applied, or observed.", "schema": {"type": "string", "enum": ["declared", "applied", "observed"]}},
          {"name": "limit", "in": "query", "description": "Page size.", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "cursor", "in": "query", "description": "Continuation cursor: non-negative integer offset returned in next_cursor of the previous page.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Bounded canonical cloud inventory list envelope ordered by cloud_resource_uid",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "resources": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "cloud_resource_uid": {"type": "string"},
                          "provider": {"type": "string"},
                          "resource_type": {"type": "string"},
                          "management_origin": {"type": "string", "enum": ["declared", "applied", "observed"]},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "source_state": {"type": "string", "description": "Provider-neutral source-state taxonomy value derived from management_origin."},
                          "tag_value_fingerprints": {
                            "type": "object",
                            "description": "Optional keyed, non-reversible tag value fingerprints attached by reducer evidence; raw tag values are never returned.",
                            "additionalProperties": {"type": "string"}
                          },
                          "attributes": {
                            "type": "object",
                            "description": "Optional bounded provider-specific attributes. GCP surfaces its typed-depth payload (e.g. table_type, schema_field_count, kms_key_name, clustering_fields) as a bounded redaction-safe passthrough. AWS surfaces a CLOSED image/version allowlist only: task_definition_arn, image_uri, resolved_image_uri, code_sha256, version, and a containers array reduced per element to {image, image_digest}. Azure's allowlist is wired but currently empty (its resource fact carries no image/version key yet). No raw provider locator (cluster_arn, role_arn, arm_resource_id, network_interfaces, container name/runtime_id, ...) or secret is ever present.",
                            "additionalProperties": true
                          },
                          "identity_policy_evidence": {
                            "type": "array",
                            "description": "Optional bounded identity-policy evidence rows containing only safe enum/text classes and keyed fingerprints; raw identities, assignment scopes, and principal GUIDs are never returned.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "evidence_key": {"type": "string"},
                                "identity_type": {"type": "string"},
                                "role_class": {"type": "string"},
                                "principal_fingerprint": {"type": "string"},
                                "client_fingerprint": {"type": "string"},
                                "object_fingerprint": {"type": "string"},
                                "tenant_fingerprint": {"type": "string"}
                              }
                            }
                          },
                          "identity_policy_evidence_truncated": {"type": "boolean", "description": "Present and true when reducer evidence was capped for this resource."},
                          "resource_change_freshness": {
                            "type": "array",
                            "description": "Optional sanitized Azure Resource Graph change evidence attached to an already-admitted canonical resource. Delete rows are tombstone candidates only.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "evidence_key": {"type": "string"},
                                "change_type": {"type": "string", "enum": ["created", "updated", "deleted"]},
                                "change_time": {"type": "string", "format": "date-time"},
                                "operation": {"type": "string"},
                                "client_type": {"type": "string"},
                                "actor_class": {"type": "string"},
                                "actor_fingerprint": {"type": "string"},
                                "changed_property_paths": {"type": "array", "items": {"type": "string"}},
                                "changed_property_truncated": {"type": "boolean"},
                                "tombstone_candidate": {"type": "boolean"}
                              }
                            }
                          },
                          "resource_change_freshness_truncated": {"type": "boolean"},
                          "evidence": {
                            "type": "object",
                            "description": "Per-layer evidence flags that contributed to the canonical identity.",
                            "properties": {
                              "declared": {"type": "boolean"},
                              "applied": {"type": "boolean"},
                              "observed": {"type": "boolean"}
                            }
                          }
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "string", "description": "Present only when truncated is true. Pass back as cursor to fetch the next page."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"description": "Invalid provider, management_origin, limit, or cursor"},
          "501": {"description": "Capability unsupported by the active query profile, or canonical identity read model unavailable"}
        }
      }
    },
`
