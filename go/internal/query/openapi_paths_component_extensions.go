// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsComponentExtensions = `
    "/api/v0/component-extensions": {
      "get": {
        "tags": ["status"],
        "summary": "List component extensions",
        "description": "Returns a bounded local component package inventory and policy diagnostics from the configured component registry without exposing server-local manifest or activation config paths.",
        "operationId": "listComponentExtensions",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100},
            "description": "Maximum number of component rows to return."
          }
        ],
        "responses": {
          "200": {
            "description": "Component extension inventory",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "status": {"type": "string", "enum": ["available"]},
                    "component_home_configured": {"type": "boolean"},
                    "components": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "name": {"type": "string"},
                          "publisher": {"type": "string"},
                          "version": {"type": "string"},
                          "manifest_digest": {"type": "string"},
                          "verified": {"type": "boolean"},
                          "trust_mode": {"type": "string"},
                          "installed_at": {"type": "string", "format": "date-time"},
                          "states": {"type": "array", "items": {"type": "string"}},
                          "activations": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "instance_id": {"type": "string"},
                                "mode": {"type": "string"},
                                "claims_enabled": {"type": "boolean"},
                                "config_handle": {"type": "string"},
                                "enabled_at": {"type": "string", "format": "date-time"}
                              }
                            }
                          },
                          "diagnostics": {"type": "object"},
                          "trust_decision": {"type": "object"},
                          "policy_gate": {"type": "object"},
                          "last_conformance_proof": {"type": "object"},
                          "scheduler_state": {"type": "object"},
                          "read_model_availability": {"type": "object"},
                          "error": {"type": "object"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "total_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "policy": {"type": "object"}
                  }
                }
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/component-extensions/{component_id}/diagnostics": {
      "get": {
        "tags": ["status"],
        "summary": "Get component extension diagnostics",
        "description": "Returns one component extension's lifecycle and policy diagnostics from local registry readback. Local manifest paths and activation config paths are redacted.",
        "operationId": "getComponentExtensionDiagnostics",
        "parameters": [
          {
            "name": "component_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Component package ID"
          }
        ],
        "responses": {
          "200": {
            "description": "Component extension diagnostics",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "status": {"type": "string", "enum": ["available"]},
                    "component_home_configured": {"type": "boolean"},
                    "component": {"type": "object"},
                    "policy": {"type": "object"}
                  }
                }
              }
            }
          },
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
