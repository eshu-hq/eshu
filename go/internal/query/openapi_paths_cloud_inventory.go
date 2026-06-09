package query

// openAPIPathsCloudInventory documents the canonical multi-cloud resource
// inventory readback route. It surfaces reducer-owned
// reducer_cloud_resource_identity rows (one per cloud_resource_uid) with
// provider, normalized identity, management_origin, evidence-layer flags, and
// the provider-neutral source state. The route is read-only, bounded,
// paginated, and truth-labeled; it never returns raw provider locators, tags,
// or credentials.
const openAPIPathsCloudInventory = `
    "/api/v0/cloud/inventory": {
      "get": {
        "summary": "List canonical multi-cloud resource identities (bounded, filterable, paginated, truth-labeled)",
        "operationId": "listCloudResourceInventory",
        "description": "Reads the reducer-owned canonical CloudResource identity rows (reducer_cloud_resource_identity). Filterable by provider (aws/gcp/azure), canonical scope, and management_origin. local_lightweight returns unsupported_capability.",
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
