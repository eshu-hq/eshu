package query

const openAPIPathsServiceCatalog = `
    "/api/v0/service-catalog/correlations": {
      "get": {
        "tags": ["service-catalog"],
        "summary": "List service catalog correlations",
        "description": "Lists reducer-owned service catalog ownership and drift correlation rows. Catalog declarations remain provenance until graph, runtime, deployment, or source evidence corroborates them.",
        "operationId": "listServiceCatalogCorrelations",
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
                      "items": {
                        "type": "object",
                        "properties": {
                          "correlation_id": {"type": "string"},
                          "provider": {"type": "string"},
                          "entity_ref": {"type": "string"},
                          "entity_type": {"type": "string"},
                          "display_name": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "service_id": {"type": "string"},
                          "workload_id": {"type": "string"},
                          "owner_ref": {"type": "string"},
                          "lifecycle": {"type": "string"},
                          "tier": {"type": "string"},
                          "outcome": {"type": "string"},
                          "reason": {"type": "string"},
                          "provenance_only": {"type": "boolean"},
                          "drift_kind": {"type": "string"},
                          "drift_status": {"type": "string"},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["correlation_id", "outcome", "provenance_only"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_correlation_id": {"type": "string"}
                      },
                      "required": ["after_correlation_id"]
                    }
                  },
                  "required": ["correlations", "count", "limit", "truncated"]
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
