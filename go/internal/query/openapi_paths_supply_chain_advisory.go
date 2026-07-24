// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainAdvisoryEvidence = `
    "/api/v0/supply-chain/advisories/evidence": {
      "get": {
        "summary": "List source-only advisory evidence",
        "description": "Requires limit plus cve_id, advisory_id, package_id, repository_id, service_id, or workload_id. Repository, service, and workload scopes derive advisory anchors from reducer-owned impact findings before reading source facts. Returns source-specific GHSA, CVE/NVD, OSV, GLAD, EPSS, KEV, CWE, range, fixed-version, withdrawal, reference, and disagreement evidence without implying additional repository, image, workload, or deployment impact.",
        "operationId": "listAdvisoryEvidence",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of a broad advisory page."},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted service anchor used to select impact findings before reading advisory source facts."},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted workload anchor used to select impact findings before reading advisory source facts."},
          {"name": "source", "in": "query", "schema": {"type": "string"}},
          {"name": "after_advisory_key", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
                    "scope": {"type": "object"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"}
                  },
                  "required": ["advisories", "count", "limit", "scope", "truncated"]
                }
              }
            }
          },
          "400": {"description": "Missing limit, advisory scope, or ambiguous selector"},
          "404": {"description": "Repository selector did not match an indexed repository"},
          "503": {"description": "Postgres source fact read model unavailable"}
        }
      }
    },
    "/api/v0/supply-chain/vulnerabilities/{advisory_id}": {
      "get": {
        "summary": "Get a single advisory by identifier",
        "description": "Path-param convenience over the advisory evidence read model. Returns one canonical advisory (matched by canonical id, GHSA id, or CVE id) with its source-specific CVSS, EPSS, KEV, CWE, range, fixed-version, reference, and affected-package evidence. This is source evidence only; repository, service, image, and workload impact remain available through the supply-chain impact findings surface.",
        "operationId": "getVulnerabilityDetail",
        "parameters": [
          {"name": "advisory_id", "in": "path", "required": true, "schema": {"type": "string"}, "description": "Canonical advisory id, GHSA id, or CVE id."}
        ],
        "responses": {
          "200": {
            "description": "Canonical source-only advisory evidence for one advisory",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "advisory_key": {"type": "string"},
                    "canonical_id": {"type": "string"},
                    "cve_ids": {"type": "array", "items": {"type": "string"}},
                    "ghsa_ids": {"type": "array", "items": {"type": "string"}},
                    "osv_ids": {"type": "array", "items": {"type": "string"}},
                    "sources": {"type": "array", "items": {"type": "object"}},
                    "affected_packages": {"type": "array", "items": {"type": "object"}},
                    "affected_products": {"type": "array", "items": {"type": "object"}},
                    "epss": {"type": "array", "items": {"type": "object"}},
                    "kev": {"type": "array", "items": {"type": "object"}},
                    "references": {"type": "array", "items": {"type": "object"}},
                    "latest_observed_at": {"type": "string"},
                    "source_freshness": {"type": "string"},
                    "source_confidence": {"type": "string"}
                  },
                  "required": ["advisory_key", "canonical_id"]
                }
              }
            }
          },
          "400": {"description": "Missing advisory_id"},
          "404": {"description": "No advisory matched the identifier"},
          "501": {"description": "Capability unsupported for the active profile"},
          "503": {"description": "Postgres source fact read model unavailable"}
        }
      }
    },
`
