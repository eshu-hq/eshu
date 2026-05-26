package query

const openAPIPathsSecurityAlertAggregate = `
    "/api/v0/supply-chain/security-alerts/reconciliations/count": {
      "get": {
        "summary": "Count provider security alert reconciliations without paging the list endpoint",
        "operationId": "countSecurityAlertReconciliations",
        "parameters": [
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string"}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Reducer-owned provider alert reconciliation totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_reconciliations": {"type": "integer"},
                    "by_reconciliation_status": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_provider": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_provider_state": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/security-alerts/reconciliations/inventory": {
      "get": {
        "summary": "Group provider security alert reconciliations by one dimension without paging the list endpoint",
        "operationId": "getSecurityAlertReconciliationInventory",
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["reconciliation_status", "provider", "provider_state", "repository_id", "package_id"], "default": "reconciliation_status"}},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string"}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
          "200": {
            "description": "Grouped count buckets ordered by count desc",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string"},
                          "value": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "group_by": {"type": "string"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
