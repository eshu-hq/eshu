// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// AdvisoryCatalog is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/advisories` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const AdvisoryCatalog = `
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
                      "description": "Summary-only catalog rows emitted by listAdvisoryCatalog from advisory.CatalogRow; source intelligence only, no impact implied.",
                      "items": {
                        "type": "object", "description": "One canonical advisory row. emitted by listAdvisoryCatalog.",
                        "properties": {
                          "advisory_key": {"type": "string", "description": "Canonical advisory key emitted by listAdvisoryCatalog (advisory.CatalogRow.Key); the keyset cursor tiebreaker."},
                          "canonical_id": {"type": "string", "description": "Canonical advisory id emitted by listAdvisoryCatalog (advisory.CatalogRow.CanonicalID)."},
                          "cve_id": {"type": "string", "description": "CVE id emitted by listAdvisoryCatalog (advisory.CatalogRow.CVEID); absent when the advisory has none."},
                          "ghsa_id": {"type": "string", "description": "GHSA id emitted by listAdvisoryCatalog (advisory.CatalogRow.GHSAID); absent when the advisory has none."},
                          "severity_label": {"type": "string", "description": "Canonical severity label emitted by listAdvisoryCatalog (advisory.CatalogRow.SeverityLabel); absent when the source facts carry no severity."},
                          "cvss_score": {"type": "number", "description": "Catalog CVSS score emitted by listAdvisoryCatalog (advisory.CatalogRow.CVSSScore); the keyset cursor ordering key. Absent when the source facts carry no CVSS score."},
                          "kev": {"type": "boolean", "description": "Whether the advisory is present in the CISA KEV catalog, emitted by listAdvisoryCatalog (advisory.CatalogRow.KEV)."},
                          "ecosystems": {"type": "array", "items": {"type": "string"}, "description": "Affected-package ecosystems emitted by listAdvisoryCatalog (advisory.CatalogRow.Ecosystems)."},
                          "package_ids": {"type": "array", "items": {"type": "string"}, "description": "Affected package ids emitted by listAdvisoryCatalog (advisory.CatalogRow.PackageIDs)."},
                          "published_at": {"type": "string", "description": "Source publication time emitted by listAdvisoryCatalog (advisory.CatalogRow.PublishedAt)."},
                          "sources": {"type": "array", "items": {"type": "string"}, "description": "Source systems behind the row, emitted by listAdvisoryCatalog (advisory.CatalogRow.Sources)."}
                        },
                        "required": ["advisory_key", "canonical_id", "kev"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listAdvisoryCatalog."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listAdvisoryCatalog."},
                    "scope": {"type": "object", "description": "Echo of the applied catalog filters emitted by advisoryCatalogResponseScope; only non-empty filters appear. kev is a boolean while the rest are strings, so this is not a string map.", "properties": {
                      "severity": {"type": "string", "description": "Severity filter echoed by advisoryCatalogResponseScope; absent when unfiltered."},
                      "ecosystem": {"type": "string", "description": "Ecosystem filter echoed by advisoryCatalogResponseScope; absent when unfiltered."},
                      "q": {"type": "string", "description": "Free-text query filter echoed by advisoryCatalogResponseScope; absent when unfiltered."},
                      "kev": {"type": "boolean", "description": "True when the KEV-only filter was applied by advisoryCatalogResponseScope; absent otherwise."}
                    }},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listAdvisoryCatalog."},
                    "next_cursor": {"type": "object", "description": "Keyset cursor emitted by listAdvisoryCatalog only when truncated carries the last row forward; absent otherwise.", "properties": {
                      "after_cvss": {"type": "number", "description": "CVSS score of the last row, emitted by listAdvisoryCatalog for the next page cursor."},
                      "after_advisory_key": {"type": "string", "description": "Advisory key of the last row, emitted by listAdvisoryCatalog for the next page cursor."}
                    }}
                  },
                  "required": ["advisories", "count", "limit", "scope", "truncated"]
                }
              }
            }
          },
          "400": {"description": "Missing limit, invalid kev flag, or incomplete cursor"},
          "501": {"description": "Capability unsupported for the active profile"},
          "503": {"description": "Postgres source fact read model unavailable"},
          "500": {"description": "Advisory catalog read failed"}
        }
      }
    },
`
