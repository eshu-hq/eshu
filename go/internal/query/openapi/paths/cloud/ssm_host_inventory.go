// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

// SSMHostInventory documents the admitted SSM host inventory observation
// readback route. It serves committed SSMHostApplication and SSMHostPlatform
// observation nodes with the read-time capture status overlay
// (successful/partial/stale/unknown) derived from the active generation's
// admitted facts. The route is read-only, bounded, host-paged, and
// truth-labeled; it surfaces observations only and never synthesizes
// vulnerability, package-correlation, or supply-chain findings.
const SSMHostInventory = `
    "/api/v0/cloud/ssm-host-inventory": {
      "get": {
        "summary": "List SSM host inventory observations with capture status (bounded, filterable, host-paged)",
        "operationId": "listSSMHostInventory",
        "x-scoped-token-support": true,
        "description": "Reads committed SSMHostApplication/SSMHostPlatform observation nodes overlaid with the active generation's admitted aws_ssm_host_application/platform facts. Filterable by instance_id, account_id, region, and status (successful, partial, stale, unknown). local_lightweight and local_authoritative return unsupported_capability (the read joins graph observation nodes with admitted Postgres facts). Scoped tokens receive hosts intersected with the caller's granted repositories/ingestion scopes; a scoped caller with no grants receives an empty page without a query. A 503 means the freshness bounds are not configured on this API instance or a reader verdict is retryable; any other read failure answers 500 internal_error with a fixed message. No 5xx body carries backend error text.",
        "parameters": [
          {"name": "instance_id", "in": "query", "description": "Filter by the exact EC2 managed node instance id.", "schema": {"type": "string"}},
          {"name": "account_id", "in": "query", "description": "Filter by the exact AWS account id.", "schema": {"type": "string"}},
          {"name": "region", "in": "query", "description": "Filter by the exact AWS region.", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "description": "Filter by read-time capture status: successful, partial, stale, or unknown. Applies after host paging, so a filtered page may be short.", "schema": {"type": "string", "enum": ["successful", "partial", "stale", "unknown"]}},
          {"name": "limit", "in": "query", "description": "Host page size.", "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 10}},
          {"name": "cursor", "in": "query", "description": "Continuation cursor: non-negative integer host offset returned in next_cursor of the previous page.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Bounded SSM host inventory page ordered by partition, account, region, instance",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "hosts": {
                      "type": "array",
                      "description": "Page of SSM host views, emitted by query.ssmHostInventoryResponse from ssmHostInventoryReadModel.Hosts.",
                      "items": {
                        "type": "object",
                        "description": "One SSM host view. Owner: query.ssmHostInventoryResponse.",
                        "properties": {
                          "partition": {"type": "string", "description": "AWS partition of the managed node, emitted by query.ssmHostView.Partition."},
                          "account_id": {"type": "string", "description": "AWS account of the managed node, emitted by query.ssmHostView.AccountID."},
                          "region": {"type": "string", "description": "AWS region of the managed node, emitted by query.ssmHostView.Region."},
                          "instance_id": {"type": "string", "description": "EC2 managed node instance id, emitted by query.ssmHostView.InstanceID."},
                          "status": {"type": "string", "enum": ["successful", "partial", "stale", "unknown"], "description": "Read-time capture status overlaid from the active generation's admitted facts, emitted by query.ssmHostView.Status."},
                          "status_reason": {"type": "string", "description": "Machine-readable reason for a non-successful status, emitted by query.ssmHostView.StatusReason."},
                          "capture_time": {"type": "string", "description": "Observation capture time emitted by query.ssmHostView.CaptureTime."},
                          "scope_id": {"type": "string", "description": "Canonical ingestion scope of the host, emitted by query.ssmHostView.ScopeID."},
                          "generation_id": {"type": "string", "description": "Active generation the overlay was assessed against, emitted by query.ssmHostView.GenerationID."},
                          "applications": {
                            "type": "array",
                            "description": "Committed allowlisted application rows, capped per host with applications_truncated set when the cap bites. Emitted by query.ssmHostView.Applications via the ssm_host_inventory_overlay application projection.",
                            "items": {
                              "type": "object",
                            "description": "One allowlisted application row. Owner: ssm_host_inventory_overlay application projection.",
                              "properties": {
                                "uid": {"type": "string", "description": "Application row identity emitted by the ssm_host_inventory_overlay application projection."},
                                "name": {"type": "string", "description": "Application name emitted by the ssm_host_inventory_overlay application projection."},
                                "version": {"type": "string", "description": "Application version emitted by the ssm_host_inventory_overlay application projection."},
                                "release": {"type": "string", "description": "Application release emitted by the ssm_host_inventory_overlay application projection."},
                                "epoch": {"type": "string", "description": "Application epoch emitted by the ssm_host_inventory_overlay application projection."},
                                "package_id": {"type": "string", "description": "Application package identity emitted by the ssm_host_inventory_overlay application projection."},
                                "architecture": {"type": "string", "description": "Application architecture emitted by the ssm_host_inventory_overlay application projection."},
                                "capture_time": {"type": "string", "description": "Application capture time emitted by the ssm_host_inventory_overlay application projection."}
                              },
                              "required": ["uid"]
                            }
                          },
                          "applications_truncated": {"type": "boolean", "description": "True when the per-host application cap bit, emitted by query.ssmHostView.ApplicationsTruncated."},
                          "platforms": {
                            "type": "array",
                            "description": "Platform detail rows for the host, emitted by query.ssmHostView.Platforms via the ssm_host_inventory_overlay platform projection.",
                            "items": {
                              "type": "object",
                            "description": "One platform detail row. Owner: ssm_host_inventory_overlay platform projection.",
                              "properties": {
                                "uid": {"type": "string", "description": "Platform row identity emitted by the ssm_host_inventory_overlay platform projection."},
                                "source_type": {"type": "string", "description": "Platform source class emitted by the ssm_host_inventory_overlay platform projection."},
                                "platform_type": {"type": "string", "description": "Platform type emitted by the ssm_host_inventory_overlay platform projection."},
                                "platform_name": {"type": "string", "description": "Platform name emitted by the ssm_host_inventory_overlay platform projection."},
                                "platform_version": {"type": "string", "description": "Platform version emitted by the ssm_host_inventory_overlay platform projection."},
                                "capture_time": {"type": "string", "description": "Platform capture time emitted by the ssm_host_inventory_overlay platform projection."},
                                "observation_time": {"type": "string", "description": "Platform observation time emitted by the ssm_host_inventory_overlay platform projection."}
                              },
                              "required": ["uid"]
                            }
                          },
                          "facts_truncated": {"type": "boolean", "description": "True when admitted facts for this host were capped, emitted by query.ssmHostView.FactsTruncated."}
                        },
                        "required": ["partition", "account_id", "region", "instance_id", "status"]
                      }
                    },
                    "malformed_facts": {"type": "integer", "description": "Count of admitted facts skipped as undecodable for this page, emitted by query.ssmHostInventoryResponse from ssmHostInventoryReadModel.MalformedFacts."},
                    "next_cursor": {"type": "string", "description": "Continuation cursor: merged-universe host offset for the next page (empty when the universe is exhausted), emitted by query.ssmHostInventoryResponse from ssmHostInventoryReadModel.NextCursor."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },`
