// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

const componentsResponses = `    "responses": {
      "Unauthorized": {
        "description": "Authentication is required or the presented credential is not active",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "BadRequest": {
        "description": "Bad request",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "NotFound": {
        "description": "Resource not found",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "Forbidden": {
        "description": "Permission denied",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "Conflict": {
        "description": "Ambiguous request or conflicting scope",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "InternalError": {
        "description": "Internal server error",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "NotImplemented": {
        "description": "Capability is not available in the current runtime profile",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "ServiceUnavailable": {
        "description": "Service unavailable. A graph backend outage, or a PostgreSQL read replica that has not replayed to the writer checkpoint or whose reader pool wait timed out, answers with the stable backend_unavailable error code, a fixed message, and a Retry-After hint; those conditions are transient and the request is safe to retry. Any other PostgreSQL reader failure (for example a permission or connection error) is not transient and answers 500.",
        "headers": {
          "Retry-After": {
            "description": "Seconds to wait before retrying a backend_unavailable response.",
            "schema": {"type": "integer", "minimum": 1}
          }
        },
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "GatewayTimeout": {
        "description": "Backend operation exceeded its deadline",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      }
    }
`
