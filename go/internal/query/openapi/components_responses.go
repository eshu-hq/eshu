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
        "description": "Service unavailable. A graph backend outage, or a PostgreSQL read replica that has not replayed to the writer checkpoint, or whose connection acquisition (pool wait or dial) or identity check timed out inside the replay window, answers with the stable backend_unavailable error code, a fixed message, and a Retry-After hint; those graph-read availability verdicts are transient and the request is safe to retry. Any other 503 backend_unavailable, such as a route that needs a graph backend the deployment did not configure, is a configuration state and carries no Retry-After. A PostgreSQL reader failure that is not a timeout (authentication or TLS failure, connection refused, permission denied, a client disconnect) answers 500.",
        "headers": {
          "Retry-After": {
            "description": "Seconds to wait before retrying. Present only on the transient graph-read availability 503 verdicts (graph unavailable, stale or timed-out PostgreSQL reader) and the checkpoint 503; absent from a permanent 503 backend_unavailable.",
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
