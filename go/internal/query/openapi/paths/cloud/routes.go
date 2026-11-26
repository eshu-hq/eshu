// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

// Routes is the OpenAPI path fragment documenting the
// `/api/v0/cloud/resources` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/cloud/resources": {
      "get": {
        "summary": "List cloud provider resources from the authoritative graph (bounded, filterable, keyset-paged)",
        "description": "Selects an authorized, current page from the graph owner ledger before hydrating only those resource IDs from the authoritative graph. Scoped tokens are filtered by repository and ingestion-scope grants before the page limit. A 503 with Retry-After means a transient condition that is safe to retry: the owner-ledger page selection hit a stale or timed-out PostgreSQL reader, or the graph backend is unavailable for the hydration read; a 503 without Retry-After means a configuration state, such as a graph backend or owner-ledger store that is not configured on this API instance. A graph hydration read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.cloud_resource_list in its error envelope.",
        "operationId": "listCloudResources",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "provider", "in": "query", "description": "Filter by collector provider (for example aws).", "schema": {"type": "string"}},
          {"name": "resource_type", "in": "query", "description": "Filter by resource type (for example aws_iam_role).", "schema": {"type": "string"}},
          {"name": "region", "in": "query", "description": "Filter by region (for example us-east-1).", "schema": {"type": "string"}},
          {"name": "account_id", "in": "query", "description": "Filter by cloud account id.", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "description": "Page size.", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "after_resource_type", "in": "query", "description": "Keyset cursor: resource_type of the last row from the previous page. Use the values returned in next_cursor.", "schema": {"type": "string"}},
          {"name": "after_id", "in": "query", "description": "Keyset cursor: id of the last row from the previous page. Use the values returned in next_cursor.", "schema": {"type": "string"}}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded cloud resource list envelope ordered by resource_type then id",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "resources": {
                      "type": "array",
                      "description": "Authorized page of cloud resource rows hydrated from the authoritative graph, emitted by query.writeCloudResourceListResponse.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string", "description": "Graph resource identity emitted by query.CloudResourceRow.ID."},
                          "resource_type": {"type": "string", "description": "Graph resource type emitted by query.CloudResourceRow.ResourceType."},
                          "name": {"type": "string", "description": "Resource display name emitted by query.CloudResourceRow.Name."},
                          "provider": {"type": "string", "description": "Collector provider emitted by query.CloudResourceRow.Provider."},
                          "region": {"type": "string", "description": "Cloud region emitted by query.CloudResourceRow.Region."},
                          "account_id": {"type": "string", "description": "Cloud account id emitted by query.CloudResourceRow.AccountID."},
                          "arn": {"type": "string", "description": "Provider ARN emitted by query.CloudResourceRow.ARN."},
                          "service_name": {"type": "string", "description": "Owning service name emitted by query.CloudResourceRow.ServiceName."},
                          "state": {"type": "string", "description": "Resource state emitted by query.CloudResourceRow.State."}
                        },
                        "required": ["id"]
                      }
                    },
                    "count": {"type": "integer", "description": "Number of resources in this page, emitted by query.writeCloudResourceListResponse as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by query.writeCloudResourceListResponse from cloudResourceListFilter via limit."},
                    "truncated": {"type": "boolean", "description": "True when more resources exist past this page, emitted by query.writeCloudResourceListResponse."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Present only when truncated is true. Pass these values back as after_resource_type and after_id to fetch the next page.",
                      "properties": {
                        "after_resource_type": {"type": "string", "description": "Keyset cursor resource_type of the last row, emitted by query.writeCloudResourceListResponse from the last page row."},
                        "after_id": {"type": "string", "description": "Keyset cursor id of the last row, emitted by query.writeCloudResourceListResponse from the last page row."}
                      },
                      "required": ["after_resource_type", "after_id"]
                    },
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded filter scope applied to the list, emitted by query.cloudResourceListScope. Keys vary per request (filter echo of the caller-supplied provider, resource_type, region, and account_id filters); values are the caller-supplied filter strings."}
                  }
                }
              }
            }
          },
          "400": {"description": "Invalid limit or incomplete cursor"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"description": "Capability unsupported by the active query profile"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
