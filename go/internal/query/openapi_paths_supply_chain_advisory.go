package query

const openAPIPathsSupplyChainAdvisoryEvidence = `
    "/api/v0/supply-chain/advisories/evidence": {
      "get": {
        "summary": "List source-only advisory evidence",
        "description": "Requires limit plus cve_id, advisory_id, or package_id. Returns source-specific GHSA, CVE/NVD, OSV, GLAD, EPSS, KEV, CWE, range, fixed-version, withdrawal, reference, and disagreement evidence without implying repository, image, workload, or deployment impact.",
        "operationId": "listAdvisoryEvidence",
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "source", "in": "query", "schema": {"type": "string"}},
          {"name": "after_advisory_key", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Canonical source-only advisory evidence page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "advisories": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "advisory_key": {"type": "string"},
                          "canonical_id": {"type": "string"},
                          "cve_ids": {"type": "array", "items": {"type": "string"}},
                          "ghsa_ids": {"type": "array", "items": {"type": "string"}},
                          "osv_ids": {"type": "array", "items": {"type": "string"}},
                          "source_ids": {"type": "array", "items": {"type": "string"}},
                          "sources": {
                            "type": "array",
                            "description": "Source-reported advisory identity, CVSS v3/v4 vectors, severity, CWE, and withdrawal evidence.",
                            "items": {"type": "object"}
                          },
                          "affected_packages": {
                            "type": "array",
                            "description": "Source-reported package ranges and fixed versions. These are not installed-version or repository impact claims.",
                            "items": {"type": "object"}
                          },
                          "affected_products": {
                            "type": "array",
                            "description": "NVD CPE/product applicability rows. These are product evidence only.",
                            "items": {"type": "object"}
                          },
                          "epss": {"type": "array", "items": {"type": "object"}},
                          "kev": {"type": "array", "items": {"type": "object"}},
                          "references": {"type": "array", "items": {"type": "object"}},
                          "source_disagreements": {
                            "type": "array",
                            "description": "Source-level disagreement on severity, withdrawn status, affected ranges, or fixed versions without selecting a winner.",
                            "items": {"type": "object"}
                          },
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "latest_observed_at": {"type": "string"},
                          "source_freshness": {"type": "string"},
                          "source_confidence": {"type": "string"}
                        },
                        "required": ["advisory_key", "canonical_id"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"}
                  },
                  "required": ["advisories", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"description": "Missing limit or advisory scope"},
          "503": {"description": "Postgres source fact read model unavailable"}
        }
      }
    },
`
