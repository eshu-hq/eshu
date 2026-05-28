package query

const openAPIPathsSupplyChainImpactAggregate = `
    "/api/v0/supply-chain/impact/findings/count": {
      "get": {
        "summary": "Count supply-chain impact findings without paging the list endpoint",
        "operationId": "countSupplyChainImpactFindings",
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}}
        ],
        "responses": {
          "200": {
            "description": "Reducer-owned vulnerability impact totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_findings": {"type": "integer"},
                    "affected_findings": {"type": "integer"},
                    "affected_exact": {"type": "integer"},
                    "affected_derived": {"type": "integer"},
                    "possibly_affected": {"type": "integer"},
                    "not_affected": {"type": "integer"},
                    "by_priority_bucket": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_severity": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/impact/inventory": {
      "get": {
        "summary": "Group supply-chain impact findings by one dimension without paging the list endpoint",
        "operationId": "getSupplyChainImpactInventory",
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["impact_status", "priority_bucket", "severity", "repository_id"], "default": "impact_status"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
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
