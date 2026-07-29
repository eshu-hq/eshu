// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainSuppressionMutation = `
    "/api/v0/supply-chain/impact/suppressions": {
      "post": {
        "summary": "Create or update an operator-authored vulnerability suppression",
        "operationId": "createVulnerabilitySuppression",
        "x-shared-key-only": true,
        "description": "Creates an immutable full-set suppression generation and queues reducer projection in the same transaction. This route requires shared-key or all-scope operator authorization. The server derives source and author from the authenticated request; callers cannot supply either field. Repeating an identical suppression is an idempotent no-op.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "additionalProperties": false,
                "properties": {
                  "suppression_id": {
                    "type": "string",
                    "maxLength": 256,
                    "description": "Stable operator-owned suppression identifier."
                  },
                  "justification": {
                    "type": "string",
                    "enum": ["not_affected", "accepted_risk", "false_positive", "ignored"]
                  },
                  "authored_at": {
                    "type": "string",
                    "format": "date-time"
                  },
                  "expires_at": {
                    "type": "string",
                    "format": "date-time",
                    "description": "Required when justification is ignored and must be strictly later than authored_at."
                  },
                  "reason": {
                    "type": "string"
                  },
                  "evidence_ref": {
                    "type": "string"
                  },
                  "scope": {
                    "type": "object",
                    "additionalProperties": false,
                    "description": "At least one discoverable identity anchor is required: cve_id, advisory_id, package_id, purl, repository_id, or subject_digest. evidence_path, environment, workload_id, and service_id may narrow an anchored suppression but cannot stand alone.",
                    "properties": {
                      "cve_id": {"type": "string"},
                      "advisory_id": {"type": "string"},
                      "package_id": {"type": "string"},
                      "purl": {"type": "string"},
                      "repository_id": {"type": "string"},
                      "subject_digest": {"type": "string"},
                      "environment": {
                        "type": "string",
                        "description": "Optional canonical environment conjunct; narrows a suppression with a discoverable identity anchor."
                      },
                      "workload_id": {
                        "type": "string",
                        "description": "Optional workload identity conjunct; narrows a suppression with a discoverable identity anchor."
                      },
                      "service_id": {
                        "type": "string",
                        "description": "Optional service identity conjunct; narrows a suppression with a discoverable identity anchor."
                      },
                      "evidence_path": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Optional conjunct that narrows a suppression with another discoverable identity anchor; not valid by itself."
                      }
                    }
                  }
                },
                "required": ["suppression_id", "justification", "authored_at", "reason", "scope"]
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Suppression generation created",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "suppression_id": {"type": "string"},
                    "generation_id": {"type": "string"},
                    "status": {"type": "string", "enum": ["created"]}
                  },
                  "required": ["suppression_id", "generation_id", "status"]
                }
              }
            }
          },
          "200": {
            "description": "Identical suppression already present in the latest generation",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "suppression_id": {"type": "string"},
                    "generation_id": {"type": "string"},
                    "status": {"type": "string", "enum": ["unchanged"]}
                  },
                  "required": ["suppression_id", "generation_id", "status"]
                }
              }
            }
          },
          "400": {"description": "Invalid suppression request"},
          "403": {"description": "All-scopes operator authorization is required"},
          "500": {"description": "Suppression transaction failed"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
