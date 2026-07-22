// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIComponentsResponses = `    "responses": {
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
        "description": "Service unavailable",
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
