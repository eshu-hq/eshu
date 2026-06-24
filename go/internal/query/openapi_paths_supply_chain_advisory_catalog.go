// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainAdvisoryCatalog = `
    "/api/v0/supply-chain/advisories": {
      "get": {
        "summary": "Browse the vulnerability-intelligence catalog",
        "description": "Lists canonical vulnerability advisories from active vulnerability source facts without requiring an advisory, package, repository, service, or workload anchor. Rows are summary-only source intelligence (id, severity, CVSS, KEV, ecosystem, package) ordered by descending CVSS then ascending advisory key, with keyset pagination. These rows are known CVE intelligence and do not imply repository, image, workload, or deployment impact; service reachability remains the separate supply-chain impact findings surface. Use GET /api/v0/supply-chain/vulnerabilities/{advisory_id} for full source evidence on one advisory.",
        "operationId": "listAdvisoryCatalog",
        "parameters": [
          {"name": "severity", "in": "query", "schema": {"type": "string"}, "description": "Canonical severity label (case-insensitive), e.g. CRITICAL, HIGH, MEDIUM, LOW."},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}, "description": "Affected-package ecosystem (case-insensitive), e.g. npm, pypi, maven."},
          {"name": "kev", "in": "query", "schema": {"type": "boolean"}, "description": "When true, limits the page to advisories present in the CISA KEV catalog."},
          {"name": "q", "in": "query", "schema": {"type": "string"}, "description": "Prefix match against canonical advisory id, CVE id, GHSA id, affected package id, or PURL."},
          {"name": "after_cvss", "in": "query", "schema": {"type": "number"}, "description": "Keyset cursor CVSS score from a prior next_cursor. Must be sent with after_advisory_key."},
          {"name": "after_advisory_key", "in": "query", "schema": {"type": "string"}, "description": "Keyset cursor advisory key from a prior next_cursor. Must be sent with after_cvss."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Bounded page of catalog advisories",
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
                          "cve_id": {"type": "string"},
                          "ghsa_id": {"type": "string"},
                          "severity_label": {"type": "string"},
                          "cvss_score": {"type": "number"},
                          "kev": {"type": "boolean"},
                          "ecosystems": {"type": "array", "items": {"type": "string"}},
                          "package_ids": {"type": "array", "items": {"type": "string"}},
                          "published_at": {"type": "string"},
                          "sources": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["advisory_key", "canonical_id", "kev"]
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
          "400": {"description": "Missing limit, invalid kev flag, or incomplete cursor"},
          "501": {"description": "Capability unsupported for the active profile"},
          "503": {"description": "Postgres source fact read model unavailable"}
        }
      }
    },
`
