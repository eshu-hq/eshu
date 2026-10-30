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
                      "items": {
                        "type": "object",
                        "properties": {
                          "partition": {"type": "string"},
                          "account_id": {"type": "string"},
                          "region": {"type": "string"},
                          "instance_id": {"type": "string"},
                          "status": {"type": "string", "enum": ["successful", "partial", "stale", "unknown"]},
                          "status_reason": {"type": "string"},
                          "capture_time": {"type": "string"},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "applications": {
                            "type": "array",
                            "description": "Committed allowlisted application rows, capped per host with applications_truncated set when the cap bites.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "uid": {"type": "string"},
                                "name": {"type": "string"},
                                "version": {"type": "string"},
                                "release": {"type": "string"},
                                "epoch": {"type": "string"},
                                "package_id": {"type": "string"},
                                "architecture": {"type": "string"},
                                "capture_time": {"type": "string"}
                              }
                            }
                          },
                          "applications_truncated": {"type": "boolean"},
                          "platforms": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "uid": {"type": "string"},
                                "source_type": {"type": "string"},
                                "platform_type": {"type": "string"},
                                "platform_name": {"type": "string"},
                                "platform_version": {"type": "string"},
                                "capture_time": {"type": "string"},
                                "observation_time": {"type": "string"}
                              }
                            }
                          },
                          "facts_truncated": {"type": "boolean"}
                        }
                      }
                    },
                    "malformed_facts": {"type": "integer"},
                    "next_cursor": {"type": "string"}
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
