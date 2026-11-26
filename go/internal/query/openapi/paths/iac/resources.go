// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// Resources documents the bounded Terraform/IaC resource list
// browse read. It is concatenated into the public OpenAPI spec in openapi/spec.go
// and must stay in lockstep with the iac_resources.go handler and
// docs/public/reference/http-api/iac-content-infra.md.
const Resources = `
    "/api/v0/iac/resources": {
      "get": {
        "tags": ["iac"],
        "summary": "List Terraform/IaC resources",
        "description": "Bounded, enveloped browse over the current active-generation Terraform/IaC inventory, hydrated from the authoritative graph. Defaults to Terraform resources; set kind to list modules or data sources. Search and filters execute server-side across the full caller-authorized current inventory. The list is keyset-paginated by (name, id); follow next_cursor.after_name and next_cursor.after_id to page. Requires the local-authoritative profile or higher.",
        "operationId": "listIaCResources",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "q",
            "in": "query",
            "required": false,
            "description": "Case-insensitive full-inventory search across name, source path, type, provider, module, repository id, and kind.",
            "schema": {"type": "string"}
          },
          {
            "name": "kind",
            "in": "query",
            "required": false,
            "description": "IaC node kind to list: resource (default), module, or data-source.",
            "schema": {"type": "string", "enum": ["resource", "module", "data-source"], "default": "resource"}
          },
          {
            "name": "type",
            "in": "query",
            "required": false,
            "description": "Filter by Terraform resource type (e.g. aws_iam_role) or, for data sources, the data type.",
            "schema": {"type": "string"}
          },
          {
            "name": "provider",
            "in": "query",
            "required": false,
            "description": "Filter by provider (e.g. aws). Present only on canonical-sourced nodes.",
            "schema": {"type": "string"}
          },
          {
            "name": "module",
            "in": "query",
            "required": false,
            "description": "Filter by module name. For resources and data sources this matches the module.\"<name>\". address prefix; for modules it matches the module name exactly.",
            "schema": {"type": "string"}
          },
          {
            "name": "repository",
            "in": "query",
            "required": false,
            "description": "Filter by canonical repository id.",
            "schema": {"type": "string"}
          },
          {
            "name": "include_facets",
            "in": "query",
            "required": false,
            "description": "Include current caller-authorized totals and bounded kind, type, provider, module, and repository facets.",
            "schema": {"type": "boolean", "default": false}
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "description": "Maximum resources to return (1-200, default 50).",
            "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}
          },
          {
            "name": "after_name",
            "in": "query",
            "required": false,
            "description": "Keyset cursor: return rows whose (name, id) sorts after this name. Use the value from next_cursor.",
            "schema": {"type": "string"}
          },
          {
            "name": "after_id",
            "in": "query",
            "required": false,
            "description": "Keyset cursor: tiebreaker id paired with after_name. Use the value from next_cursor.",
            "schema": {"type": "string"}
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded IaC resource list",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "kind": {"type": "string", "description": "Echo of the caller's node kind, emitted by the iac resources handler from the normalized resourceKind (resource, module, or data-source)."},
                    "count": {"type": "integer", "description": "Number of resources in this page, emitted by the iac resources handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac resources handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the iac resources handler from the lookahead row."},
                    "summary": {
                      "type": "object",
                      "description": "Authoritative current totals and bounded selector facets, emitted by iac.InventorySummary via PostgresIaCInventoryStore.Summary; present when include_facets=true.",
                      "properties": {
                        "total": {"type": "integer", "description": "Caller-authorized current inventory total, emitted by iac.InventorySummary.Total."},
                        "by_kind": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Count per IaC node kind, emitted by iac.InventorySummary.ByKind. Keys are the resourceKind names (resource, module, data-source)."},
                        "types": {"type": "array", "items": {"type": "object", "description": "One type facet row. Owner: iac.InventoryFacet.", "properties": {"kind": {"type": "string", "description": "Node kind of this facet row, emitted by iac.InventoryFacet.Kind."}, "value": {"type": "string", "description": "Terraform type of this facet row, emitted by iac.InventoryFacet.Value."}, "count": {"type": "integer", "description": "Resources of this type, emitted by iac.InventoryFacet.Count."}}}, "description": "Bounded type facets emitted by iac.InventorySummary.Types."},
                        "providers": {"type": "array", "items": {"type": "object", "description": "One provider facet row. Owner: iac.InventoryFacet.", "properties": {"kind": {"type": "string", "description": "Node kind of this facet row, emitted by iac.InventoryFacet.Kind."}, "value": {"type": "string", "description": "Provider of this facet row, emitted by iac.InventoryFacet.Value."}, "count": {"type": "integer", "description": "Resources of this provider, emitted by iac.InventoryFacet.Count."}}}, "description": "Bounded provider facets emitted by iac.InventorySummary.Providers."},
                        "modules": {"type": "array", "items": {"type": "object", "description": "One module facet row. Owner: iac.InventoryFacet.", "properties": {"kind": {"type": "string", "description": "Node kind of this facet row, emitted by iac.InventoryFacet.Kind."}, "value": {"type": "string", "description": "Module of this facet row, emitted by iac.InventoryFacet.Value."}, "count": {"type": "integer", "description": "Resources of this module, emitted by iac.InventoryFacet.Count."}}}, "description": "Bounded module facets emitted by iac.InventorySummary.Modules."},
                        "repositories": {"type": "array", "items": {"type": "object", "description": "One repository facet row. Owner: iac.InventoryFacet.", "properties": {"value": {"type": "string", "description": "Repository id of this facet row, emitted by iac.InventoryFacet.Value (kind is omitted for repository facets)."}, "count": {"type": "integer", "description": "Resources of this repository, emitted by iac.InventoryFacet.Count."}}}, "description": "Bounded repository facets emitted by iac.InventorySummary.Repositories."},
                        "facet_limit": {"type": "integer", "description": "Bound applied to each facet list, emitted by iac.InventorySummary.FacetLimit."},
                        "truncated": {"type": "object", "additionalProperties": {"type": "boolean"}, "description": "Per-dimension facet cap flags emitted by iac.InventorySummary.Truncated. Keys are the facet dimension names (types, providers, modules, repositories)."}
                      },
                      "required": ["total", "by_kind", "types", "providers", "modules", "repositories", "facet_limit", "truncated"]
                    },
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor for the next page (present only when truncated), emitted by the iac resources handler from the last page row.",
                      "properties": {
                        "after_name": {"type": "string", "description": "Name of the last row, emitted by the iac resources handler from last.Name."},
                        "after_id": {"type": "string", "description": "Tiebreaker id of the last row, emitted by the iac resources handler from last.ID."}
                      },
                      "required": ["after_name", "after_id"]
                    },
                    "resources": {
                      "type": "array",
                      "description": "Page of IaC resource rows hydrated from the authoritative graph, emitted by the iac resources handler from resourceRowFromGraph.",
                      "items": {
                        "type": "object",
                        "description": "One IaC resource row. Owner: iac resourceRowFromGraph.",
                        "properties": {
                          "id": {"type": "string", "description": "Graph resource identity emitted by iac.resourceRow.ID."},
                          "kind": {"type": "string", "description": "IaC node kind emitted by iac.resourceRow.Kind."},
                          "name": {"type": "string", "description": "Resource name emitted by iac.resourceRow.Name."},
                          "resource_name": {"type": "string", "description": "Terraform resource name emitted by iac.resourceRow.ResourceName."},
                          "type": {"type": "string", "description": "Terraform resource or data type emitted by iac.resourceRow.Type."},
                          "provider": {"type": "string", "description": "Provider emitted by iac.resourceRow.Provider."},
                          "resource_service": {"type": "string", "description": "Owning service emitted by iac.resourceRow.Service."},
                          "resource_category": {"type": "string", "description": "Resource category emitted by iac.resourceRow.Category."},
                          "module": {"type": "string", "description": "Module address emitted by iac.resourceRow.Module."},
                          "repo_id": {"type": "string", "description": "Canonical repository id emitted by iac.resourceRow.RepoID."},
                          "relative_path": {"type": "string", "description": "Source-relative path emitted by iac.resourceRow.RelativePath."},
                          "line_number": {"type": "integer", "description": "Source line number emitted by iac.resourceRow.LineNumber."}
                        },
                        "required": ["id", "kind", "name"]
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
