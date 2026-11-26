// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// AdvisoryEvidence is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/advisories/evidence`,
// `/api/v0/supply-chain/vulnerabilities/{advisory_id}` routes. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md. Advisory ids are raw
// source record ids matched verbatim: CVE, GHSA, and every recognized
// OSV-family/source id (PYSEC, GO, RUSTSEC, MAL, OSV, distro, and
// colon-suffixed errata ids) share one structural recognizer, and every
// recognized non-CVE, non-GHSA id files into osv_ids.
const AdvisoryEvidence = `
    "/api/v0/supply-chain/advisories/evidence": {
      "get": {
        "summary": "List source-only advisory evidence",
        "description": "Requires limit plus cve_id, advisory_id, package_id, repository_id, service_id, or workload_id. advisory_id accepts any recognized advisory id (CVE, GHSA, or OSV-family/source id such as PYSEC, GO, RUSTSEC, MAL, OSV, distro, or colon-suffixed errata); alias and correlation-anchor hops expand one hop over the same structural recognizer with no upstream/related/transitive expansion. Repository, service, and workload scopes derive advisory anchors from reducer-owned impact findings before reading source facts. Returns source-specific GHSA, CVE/NVD, OSV-family (osv_ids carries every recognized non-CVE, non-GHSA advisory id: PYSEC, GO, RUSTSEC, MAL, OSV, distro, and errata ids), GLAD, EPSS, KEV, CWE, range, fixed-version, withdrawal, reference, and disagreement evidence without implying additional repository, image, workload, or deployment impact.",
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Canonical advisory rows emitted by listAdvisoryEvidence from advisory.EvidenceRow; source evidence only, no repository, image, workload, or deployment impact implied.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "advisory_key": {"type": "string", "description": "Canonical advisory key emitted by listAdvisoryEvidence (advisory.EvidenceRow.Key)."},
                          "canonical_id": {"type": "string", "description": "Canonical advisory id emitted by listAdvisoryEvidence (advisory.EvidenceRow.CanonicalID)."},
                          "cve_ids": {"type": "array", "items": {"type": "string"}, "description": "CVE ids on the grouped row, emitted by listAdvisoryEvidence (advisory.EvidenceRow.CVEIDs)."},
                          "ghsa_ids": {"type": "array", "items": {"type": "string"}, "description": "GHSA ids on the grouped row, emitted by listAdvisoryEvidence (advisory.EvidenceRow.GHSAIDs)."},
                          "osv_ids": {"type": "array", "items": {"type": "string"}, "description": "Every recognized OSV-family/source advisory id on the grouped row (PYSEC, GO, RUSTSEC, MAL, OSV, distro, and colon-suffixed errata ids) emitted by listAdvisoryEvidence (advisory.EvidenceRow.OSVIDs). CVE and GHSA ids stay in cve_ids/ghsa_ids; per-source identity stays in source_ids as source:advisory_id."},
                          "source_ids": {"type": "array", "items": {"type": "string"}, "description": "Per-source advisory identities as source:advisory_id pairs, emitted by listAdvisoryEvidence (advisory.EvidenceRow.SourceIDs)."},
                          "sources": {
                            "type": "array",
                            "description": "Source-reported advisory identity, CVSS v3/v4 vectors, severity, CWE, and withdrawal evidence, emitted by listAdvisoryEvidence (advisory.SourceEvidence).",
                            "items": {
                              "type": "object",
                              "properties": {
                                "source": {"type": "string", "description": "Source system that reported the observation, emitted by listAdvisoryEvidence (advisory.SourceEvidence.Source)."},
                                "advisory_id": {"type": "string", "description": "Source-native advisory id, emitted by listAdvisoryEvidence (advisory.SourceEvidence.ID)."},
                                "cve_id": {"type": "string", "description": "CVE id attached by the source, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVEID)."},
                                "ghsa_id": {"type": "string", "description": "GHSA id attached by the source, emitted by listAdvisoryEvidence (advisory.SourceEvidence.GHSAID)."},
                                "aliases": {"type": "array", "items": {"type": "string"}, "description": "Alias ids reported by the source, emitted by listAdvisoryEvidence (advisory.SourceEvidence.Aliases)."},
                                "published_at": {"type": "string", "description": "Source publication time, emitted by listAdvisoryEvidence (advisory.SourceEvidence.PublishedAt)."},
                                "modified_at": {"type": "string", "description": "Source modification time, emitted by listAdvisoryEvidence (advisory.SourceEvidence.ModifiedAt)."},
                                "withdrawn_at": {"type": "string", "description": "Source withdrawal time, emitted by listAdvisoryEvidence (advisory.SourceEvidence.WithdrawnAt); absent unless withdrawn."},
                                "severity_label": {"type": "string", "description": "Source severity label, emitted by listAdvisoryEvidence (advisory.SourceEvidence.SeverityLabel)."},
                                "cvss_score": {"type": "number", "description": "Source CVSS score, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVSSScore)."},
                                "cvss_vector": {"type": "string", "description": "Source CVSS vector, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVSSVector)."},
                                "cvss_v2": {"type": "string", "description": "Source CVSS v2 vector, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVSSVectorV2)."},
                                "cvss_v3": {"type": "string", "description": "Source CVSS v3 vector, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVSSVectorV3)."},
                                "cvss_v4": {"type": "string", "description": "Source CVSS v4 vector, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVSSVectorV4)."},
                                "cvss_metrics": {"type": "object", "additionalProperties": {}, "description": "Source CVSS metric map with implementation-defined keys, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CVSSMetrics)."},
                                "severity": {"type": "array", "items": {"type": "object", "additionalProperties": {"type": "string"}}, "description": "Source severity entries as string maps, emitted by listAdvisoryEvidence (advisory.SourceEvidence.Severity)."},
                                "cwes": {"type": "array", "items": {"type": "string"}, "description": "CWE ids reported by the source, emitted by listAdvisoryEvidence (advisory.SourceEvidence.CWEs)."},
                                "source_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Source fact ids behind the observation, emitted by listAdvisoryEvidence (advisory.SourceEvidence.SourceFactIDs)."}
                              }
                            }
                          },
                          "affected_packages": {
                            "type": "array",
                            "description": "Source-reported package ranges and fixed versions emitted by listAdvisoryEvidence (advisory.AffectedPackage). These are not installed-version or repository impact claims.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "source": {"type": "string", "description": "Source system that reported the range, emitted by listAdvisoryEvidence (advisory.AffectedPackage.Source)."},
                                "advisory_id": {"type": "string", "description": "Source-native advisory id, emitted by listAdvisoryEvidence (advisory.AffectedPackage.ID)."},
                                "cve_id": {"type": "string", "description": "CVE id attached by the source, emitted by listAdvisoryEvidence (advisory.AffectedPackage.CVEID)."},
                                "ghsa_id": {"type": "string", "description": "GHSA id attached by the source, emitted by listAdvisoryEvidence (advisory.AffectedPackage.GHSAID)."},
                                "ecosystem": {"type": "string", "description": "Package ecosystem, emitted by listAdvisoryEvidence (advisory.AffectedPackage.Ecosystem)."},
                                "package_id": {"type": "string", "description": "Package id, emitted by listAdvisoryEvidence (advisory.AffectedPackage.PackageID)."},
                                "purl": {"type": "string", "description": "Package URL, emitted by listAdvisoryEvidence (advisory.AffectedPackage.PURL)."},
                                "affected_range": {"type": "string", "description": "Affected version range, emitted by listAdvisoryEvidence (advisory.AffectedPackage.AffectedRange)."},
                                "parsed_affected_range": {"type": "object", "additionalProperties": {}, "description": "Parsed affected-range map with source-defined keys, emitted by listAdvisoryEvidence (advisory.AffectedPackage.ParsedAffectedRange)."},
                                "affected_ranges": {"type": "array", "items": {"type": "object", "additionalProperties": {}}, "description": "Affected range entries as open maps, emitted by listAdvisoryEvidence (advisory.AffectedPackage.AffectedRanges)."},
                                "affected_versions": {"type": "array", "items": {"type": "string"}, "description": "Affected versions, emitted by listAdvisoryEvidence (advisory.AffectedPackage.AffectedVersions)."},
                                "fixed_versions": {"type": "array", "items": {"type": "string"}, "description": "Fixed versions, emitted by listAdvisoryEvidence (advisory.AffectedPackage.FixedVersions)."},
                                "source_fact_id": {"type": "string", "description": "Source fact id behind the row, emitted by listAdvisoryEvidence (advisory.AffectedPackage.SourceFactID)."}
                              }
                            }
                          },
                          "affected_products": {
                            "type": "array",
                            "description": "NVD CPE/product applicability rows emitted by listAdvisoryEvidence (advisory.AffectedProduct). These are product evidence only.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "source": {"type": "string", "description": "Source system that reported the product row, emitted by listAdvisoryEvidence (advisory.AffectedProduct.Source)."},
                                "cve_id": {"type": "string", "description": "CVE id the product row applies to, emitted by listAdvisoryEvidence (advisory.AffectedProduct.CVEID)."},
                                "criteria": {"type": "string", "description": "CPE criteria string, emitted by listAdvisoryEvidence (advisory.AffectedProduct.Criteria)."},
                                "match_criteria_id": {"type": "string", "description": "NVD match criteria id, emitted by listAdvisoryEvidence (advisory.AffectedProduct.MatchCriteriaID)."},
                                "vulnerable": {"type": "boolean", "description": "Whether the product row marks the criteria vulnerable, emitted by listAdvisoryEvidence (advisory.AffectedProduct.Vulnerable)."},
                                "version_start_including": {"type": "string", "description": "Inclusive vulnerable version start, emitted by listAdvisoryEvidence (advisory.AffectedProduct.VersionStartIncluding)."},
                                "version_start_excluding": {"type": "string", "description": "Exclusive vulnerable version start, emitted by listAdvisoryEvidence (advisory.AffectedProduct.VersionStartExcluding)."},
                                "version_end_including": {"type": "string", "description": "Inclusive vulnerable version end, emitted by listAdvisoryEvidence (advisory.AffectedProduct.VersionEndIncluding)."},
                                "version_end_excluding": {"type": "string", "description": "Exclusive vulnerable version end, emitted by listAdvisoryEvidence (advisory.AffectedProduct.VersionEndExcluding)."},
                                "source_configuration_operator": {"type": "string", "description": "NVD configuration operator, emitted by listAdvisoryEvidence (advisory.AffectedProduct.SourceConfigurationOperator)."},
                                "source_configuration_negate": {"type": "boolean", "description": "NVD configuration negation, emitted by listAdvisoryEvidence (advisory.AffectedProduct.SourceConfigurationNegate)."},
                                "source_node_operator": {"type": "string", "description": "NVD node operator, emitted by listAdvisoryEvidence (advisory.AffectedProduct.SourceNodeOperator)."},
                                "source_node_negate": {"type": "boolean", "description": "NVD node negation, emitted by listAdvisoryEvidence (advisory.AffectedProduct.SourceNodeNegate)."},
                                "source_fact_id": {"type": "string", "description": "Source fact id behind the row, emitted by listAdvisoryEvidence (advisory.AffectedProduct.SourceFactID)."}
                              }
                            }
                          },
                          "epss": {"type": "array", "description": "FIRST EPSS score observations emitted by listAdvisoryEvidence (advisory.EPSSObservation).", "items": {
                            "type": "object",
                            "properties": {
                              "source": {"type": "string", "description": "Source system behind the observation, emitted by listAdvisoryEvidence (advisory.EPSSObservation.Source)."},
                              "cve_id": {"type": "string", "description": "CVE id the score applies to, emitted by listAdvisoryEvidence (advisory.EPSSObservation.CVEID)."},
                              "probability": {"type": "string", "description": "EPSS probability, emitted by listAdvisoryEvidence (advisory.EPSSObservation.Probability)."},
                              "percentile": {"type": "string", "description": "EPSS percentile, emitted by listAdvisoryEvidence (advisory.EPSSObservation.Percentile)."},
                              "score_date": {"type": "string", "description": "EPSS score date, emitted by listAdvisoryEvidence (advisory.EPSSObservation.ScoreDate)."},
                              "fact_id": {"type": "string", "description": "Fact id behind the observation, emitted by listAdvisoryEvidence (advisory.EPSSObservation.FactID)."}
                            }
                          }},
                          "kev": {"type": "array", "description": "CISA KEV known-exploited observations emitted by listAdvisoryEvidence (advisory.KEVObservation).", "items": {
                            "type": "object",
                            "properties": {
                              "source": {"type": "string", "description": "Source system behind the observation, emitted by listAdvisoryEvidence (advisory.KEVObservation.Source)."},
                              "cve_id": {"type": "string", "description": "CVE id the KEV entry applies to, emitted by listAdvisoryEvidence (advisory.KEVObservation.CVEID)."},
                              "date_added": {"type": "string", "description": "KEV catalog date added, emitted by listAdvisoryEvidence (advisory.KEVObservation.DateAdded)."},
                              "required_action": {"type": "string", "description": "KEV required action, emitted by listAdvisoryEvidence (advisory.KEVObservation.RequiredAction)."},
                              "due_date": {"type": "string", "description": "KEV remediation due date, emitted by listAdvisoryEvidence (advisory.KEVObservation.DueDate)."},
                              "known_ransomware_campaign_use": {"type": "string", "description": "KEV known ransomware campaign use, emitted by listAdvisoryEvidence (advisory.KEVObservation.KnownRansomwareCampaignUse)."},
                              "cwes": {"type": "array", "items": {"type": "string"}, "description": "CWE ids on the KEV entry, emitted by listAdvisoryEvidence (advisory.KEVObservation.CWEs)."},
                              "fact_id": {"type": "string", "description": "Fact id behind the observation, emitted by listAdvisoryEvidence (advisory.KEVObservation.FactID)."}
                            }
                          }},
                          "references": {"type": "array", "description": "Sanitized source reference URLs emitted by listAdvisoryEvidence (advisory.ReferenceEvidence).", "items": {
                            "type": "object",
                            "properties": {
                              "source": {"type": "string", "description": "Source system behind the reference, emitted by listAdvisoryEvidence (advisory.ReferenceEvidence.Source)."},
                              "advisory_id": {"type": "string", "description": "Source-native advisory id, emitted by listAdvisoryEvidence (advisory.ReferenceEvidence.ID)."},
                              "cve_id": {"type": "string", "description": "CVE id attached by the source, emitted by listAdvisoryEvidence (advisory.ReferenceEvidence.CVEID)."},
                              "reference_type": {"type": "string", "description": "Reference type, emitted by listAdvisoryEvidence (advisory.ReferenceEvidence.ReferenceType)."},
                              "url": {"type": "string", "description": "Sanitized reference URL, emitted by listAdvisoryEvidence (advisory.ReferenceEvidence.URL)."},
                              "fact_id": {"type": "string", "description": "Fact id behind the reference, emitted by listAdvisoryEvidence (advisory.ReferenceEvidence.FactID)."}
                            }
                          }},
                          "source_disagreements": {
                            "type": "array",
                            "description": "Source-level disagreement on severity, withdrawn status, affected ranges, or fixed versions without selecting a winner, emitted by listAdvisoryEvidence (advisory.SourceDisagreement).",
                            "items": {
                              "type": "object",
                              "properties": {
                                "field": {"type": "string", "description": "Disagreed field, emitted by listAdvisoryEvidence (advisory.SourceDisagreement.Field)."},
                                "values": {"type": "array", "description": "One value per disagreeing source, emitted by listAdvisoryEvidence (advisory.DisagreementValue).", "items": {
                                  "type": "object",
                                  "properties": {
                                    "source": {"type": "string", "description": "Disagreeing source, emitted by listAdvisoryEvidence (advisory.DisagreementValue.Source)."},
                                    "value": {"type": "string", "description": "That source's value, emitted by listAdvisoryEvidence (advisory.DisagreementValue.Value)."}
                                  }
                                }}
                              }
                            }
                          },
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids behind the row, emitted by listAdvisoryEvidence (advisory.EvidenceRow.EvidenceFactIDs)."},
                          "latest_observed_at": {"type": "string", "description": "Latest source observation time, emitted by listAdvisoryEvidence (advisory.EvidenceRow.LatestObservedAt)."},
                          "source_freshness": {"type": "string", "description": "Source freshness signal, emitted by listAdvisoryEvidence (advisory.EvidenceRow.SourceFreshness)."},
                          "source_confidence": {"type": "string", "description": "Source confidence signal, emitted by listAdvisoryEvidence (advisory.EvidenceRow.SourceConfidence)."}
                        },
                        "required": ["advisory_key", "canonical_id"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listAdvisoryEvidence."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listAdvisoryEvidence."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the applied evidence filters, as string pairs (cve_id, advisory_id, package_id, repository_id, service_id, workload_id) emitted by advisoryEvidenceResponseScope; only non-empty filters appear. Render as filter chips, never a fixed form."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listAdvisoryEvidence."},
                    "next_cursor": {"type": "object", "description": "Keyset cursor emitted by listAdvisoryEvidence only when truncated carries the last row key forward; absent otherwise.", "properties": {
                      "after_advisory_key": {"type": "string", "description": "Advisory key of the last row, emitted by listAdvisoryEvidence for the next page cursor."}
                    }}
                  },
                  "required": ["advisories", "count", "limit", "scope", "truncated"]
                }
              }
            }
          },
          "400": {"description": "Missing limit or advisory scope"},
          "404": {"description": "Repository selector did not match an indexed repository"},
          "409": {"description": "Repository selector matched multiple repositories; the body carries error.code ambiguous with error.details.candidates, each an object carrying repo_id plus, when known, repo_name and match_basis (the selector field that matched)"},
          "503": {"description": "Postgres source fact read model unavailable"}
        }
      }
    },
    "/api/v0/supply-chain/vulnerabilities/{advisory_id}": {
      "get": {
        "summary": "Get a single advisory by identifier",
        "description": "Path-param convenience over the advisory evidence read model. Returns one canonical advisory (matched by canonical id, GHSA id, CVE id, or any recognized OSV-family/source advisory id such as PYSEC, distro, or colon-suffixed errata ids) with its source-specific CVSS, EPSS, KEV, CWE, range, fixed-version, reference, and affected-package evidence. osv_ids carries every recognized non-CVE, non-GHSA advisory id on the row. This is source evidence only; repository, service, image, and workload impact remain available through the supply-chain impact findings surface.",
        "operationId": "getVulnerabilityDetail",
        "parameters": [
          {"name": "advisory_id", "in": "path", "required": true, "schema": {"type": "string"}, "description": "Canonical advisory id, GHSA id, CVE id, or recognized OSV-family/source advisory id (raw source record id, verbatim)."}
        ],
        "responses": {
          "200": {
            "description": "Canonical source-only advisory evidence for one advisory",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "advisory_key": {"type": "string", "description": "Canonical advisory key emitted by getVulnerabilityDetail (advisory.EvidenceRow.Key)."},
                    "canonical_id": {"type": "string", "description": "Canonical advisory id emitted by getVulnerabilityDetail (advisory.EvidenceRow.CanonicalID)."},
                    "cve_ids": {"type": "array", "items": {"type": "string"}, "description": "CVE ids on the advisory, emitted by getVulnerabilityDetail (advisory.EvidenceRow.CVEIDs)."},
                    "ghsa_ids": {"type": "array", "items": {"type": "string"}, "description": "GHSA ids on the advisory, emitted by getVulnerabilityDetail (advisory.EvidenceRow.GHSAIDs)."},
                    "osv_ids": {"type": "array", "items": {"type": "string"}, "description": "Every recognized OSV-family/source advisory id on the advisory (PYSEC, GO, RUSTSEC, MAL, OSV, distro, and colon-suffixed errata ids) emitted by getVulnerabilityDetail (advisory.EvidenceRow.OSVIDs)."},
                    "source_ids": {"type": "array", "items": {"type": "string"}, "description": "Per-source advisory identities as source:advisory_id pairs, emitted by getVulnerabilityDetail (advisory.EvidenceRow.SourceIDs)."},
                    "sources": {"type": "array", "description": "Source-reported advisory identity, CVSS vectors, severity, CWE, and withdrawal evidence, emitted by getVulnerabilityDetail (advisory.SourceEvidence with the same keys as the evidence page sources items).", "items": {
                      "type": "object",
                      "properties": {
                        "source": {"type": "string", "description": "Source system that reported the observation, emitted by getVulnerabilityDetail (advisory.SourceEvidence.Source)."},
                        "advisory_id": {"type": "string", "description": "Source-native advisory id, emitted by getVulnerabilityDetail (advisory.SourceEvidence.ID)."},
                        "cve_id": {"type": "string", "description": "CVE id attached by the source, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVEID)."},
                        "ghsa_id": {"type": "string", "description": "GHSA id attached by the source, emitted by getVulnerabilityDetail (advisory.SourceEvidence.GHSAID)."},
                        "aliases": {"type": "array", "items": {"type": "string"}, "description": "Alias ids reported by the source, emitted by getVulnerabilityDetail (advisory.SourceEvidence.Aliases)."},
                        "published_at": {"type": "string", "description": "Source publication time, emitted by getVulnerabilityDetail (advisory.SourceEvidence.PublishedAt)."},
                        "modified_at": {"type": "string", "description": "Source modification time, emitted by getVulnerabilityDetail (advisory.SourceEvidence.ModifiedAt)."},
                        "withdrawn_at": {"type": "string", "description": "Source withdrawal time, emitted by getVulnerabilityDetail (advisory.SourceEvidence.WithdrawnAt); absent unless withdrawn."},
                        "severity_label": {"type": "string", "description": "Source severity label, emitted by getVulnerabilityDetail (advisory.SourceEvidence.SeverityLabel)."},
                        "cvss_score": {"type": "number", "description": "Source CVSS score, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVSSScore)."},
                        "cvss_vector": {"type": "string", "description": "Source CVSS vector, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVSSVector)."},
                        "cvss_v2": {"type": "string", "description": "Source CVSS v2 vector, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVSSVectorV2)."},
                        "cvss_v3": {"type": "string", "description": "Source CVSS v3 vector, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVSSVectorV3)."},
                        "cvss_v4": {"type": "string", "description": "Source CVSS v4 vector, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVSSVectorV4)."},
                        "cvss_metrics": {"type": "object", "additionalProperties": {}, "description": "Source CVSS metric map with implementation-defined keys, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CVSSMetrics)."},
                        "severity": {"type": "array", "items": {"type": "object", "additionalProperties": {"type": "string"}}, "description": "Source severity entries as string maps, emitted by getVulnerabilityDetail (advisory.SourceEvidence.Severity)."},
                        "cwes": {"type": "array", "items": {"type": "string"}, "description": "CWE ids reported by the source, emitted by getVulnerabilityDetail (advisory.SourceEvidence.CWEs)."},
                        "source_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Source fact ids behind the observation, emitted by getVulnerabilityDetail (advisory.SourceEvidence.SourceFactIDs)."}
                      }
                    }},
                    "affected_packages": {"type": "array", "description": "Source-reported package ranges and fixed versions, emitted by getVulnerabilityDetail (advisory.AffectedPackage with the same keys as the evidence page affected_packages items).", "items": {
                      "type": "object",
                      "properties": {
                        "source": {"type": "string", "description": "Source system that reported the range, emitted by getVulnerabilityDetail (advisory.AffectedPackage.Source)."},
                        "advisory_id": {"type": "string", "description": "Source-native advisory id, emitted by getVulnerabilityDetail (advisory.AffectedPackage.ID)."},
                        "cve_id": {"type": "string", "description": "CVE id attached by the source, emitted by getVulnerabilityDetail (advisory.AffectedPackage.CVEID)."},
                        "ghsa_id": {"type": "string", "description": "GHSA id attached by the source, emitted by getVulnerabilityDetail (advisory.AffectedPackage.GHSAID)."},
                        "ecosystem": {"type": "string", "description": "Package ecosystem, emitted by getVulnerabilityDetail (advisory.AffectedPackage.Ecosystem)."},
                        "package_id": {"type": "string", "description": "Package id, emitted by getVulnerabilityDetail (advisory.AffectedPackage.PackageID)."},
                        "purl": {"type": "string", "description": "Package URL, emitted by getVulnerabilityDetail (advisory.AffectedPackage.PURL)."},
                        "affected_range": {"type": "string", "description": "Affected version range, emitted by getVulnerabilityDetail (advisory.AffectedPackage.AffectedRange)."},
                        "parsed_affected_range": {"type": "object", "additionalProperties": {}, "description": "Parsed affected-range map with source-defined keys, emitted by getVulnerabilityDetail (advisory.AffectedPackage.ParsedAffectedRange)."},
                        "affected_ranges": {"type": "array", "items": {"type": "object", "additionalProperties": {}}, "description": "Affected range entries as open maps, emitted by getVulnerabilityDetail (advisory.AffectedPackage.AffectedRanges)."},
                        "affected_versions": {"type": "array", "items": {"type": "string"}, "description": "Affected versions, emitted by getVulnerabilityDetail (advisory.AffectedPackage.AffectedVersions)."},
                        "fixed_versions": {"type": "array", "items": {"type": "string"}, "description": "Fixed versions, emitted by getVulnerabilityDetail (advisory.AffectedPackage.FixedVersions)."},
                        "source_fact_id": {"type": "string", "description": "Source fact id behind the row, emitted by getVulnerabilityDetail (advisory.AffectedPackage.SourceFactID)."}
                      }
                    }},
                    "affected_products": {"type": "array", "description": "NVD CPE/product applicability rows, emitted by getVulnerabilityDetail (advisory.AffectedProduct with the same keys as the evidence page affected_products items).", "items": {
                      "type": "object",
                      "properties": {
                        "source": {"type": "string", "description": "Source system that reported the product row, emitted by getVulnerabilityDetail (advisory.AffectedProduct.Source)."},
                        "cve_id": {"type": "string", "description": "CVE id the product row applies to, emitted by getVulnerabilityDetail (advisory.AffectedProduct.CVEID)."},
                        "criteria": {"type": "string", "description": "CPE criteria string, emitted by getVulnerabilityDetail (advisory.AffectedProduct.Criteria)."},
                        "match_criteria_id": {"type": "string", "description": "NVD match criteria id, emitted by getVulnerabilityDetail (advisory.AffectedProduct.MatchCriteriaID)."},
                        "vulnerable": {"type": "boolean", "description": "Whether the product row marks the criteria vulnerable, emitted by getVulnerabilityDetail (advisory.AffectedProduct.Vulnerable)."},
                        "version_start_including": {"type": "string", "description": "Inclusive vulnerable version start, emitted by getVulnerabilityDetail (advisory.AffectedProduct.VersionStartIncluding)."},
                        "version_start_excluding": {"type": "string", "description": "Exclusive vulnerable version start, emitted by getVulnerabilityDetail (advisory.AffectedProduct.VersionStartExcluding)."},
                        "version_end_including": {"type": "string", "description": "Inclusive vulnerable version end, emitted by getVulnerabilityDetail (advisory.AffectedProduct.VersionEndIncluding)."},
                        "version_end_excluding": {"type": "string", "description": "Exclusive vulnerable version end, emitted by getVulnerabilityDetail (advisory.AffectedProduct.VersionEndExcluding)."},
                        "source_configuration_operator": {"type": "string", "description": "NVD configuration operator, emitted by getVulnerabilityDetail (advisory.AffectedProduct.SourceConfigurationOperator)."},
                        "source_configuration_negate": {"type": "boolean", "description": "NVD configuration negation, emitted by getVulnerabilityDetail (advisory.AffectedProduct.SourceConfigurationNegate)."},
                        "source_node_operator": {"type": "string", "description": "NVD node operator, emitted by getVulnerabilityDetail (advisory.AffectedProduct.SourceNodeOperator)."},
                        "source_node_negate": {"type": "boolean", "description": "NVD node negation, emitted by getVulnerabilityDetail (advisory.AffectedProduct.SourceNodeNegate)."},
                        "source_fact_id": {"type": "string", "description": "Source fact id behind the row, emitted by getVulnerabilityDetail (advisory.AffectedProduct.SourceFactID)."}
                      }
                    }},
                    "epss": {"type": "array", "description": "FIRST EPSS score observations, emitted by getVulnerabilityDetail (advisory.EPSSObservation with the same keys as the evidence page epss items).", "items": {
                      "type": "object",
                      "properties": {
                        "source": {"type": "string", "description": "Source system behind the observation, emitted by getVulnerabilityDetail (advisory.EPSSObservation.Source)."},
                        "cve_id": {"type": "string", "description": "CVE id the score applies to, emitted by getVulnerabilityDetail (advisory.EPSSObservation.CVEID)."},
                        "probability": {"type": "string", "description": "EPSS probability, emitted by getVulnerabilityDetail (advisory.EPSSObservation.Probability)."},
                        "percentile": {"type": "string", "description": "EPSS percentile, emitted by getVulnerabilityDetail (advisory.EPSSObservation.Percentile)."},
                        "score_date": {"type": "string", "description": "EPSS score date, emitted by getVulnerabilityDetail (advisory.EPSSObservation.ScoreDate)."},
                        "fact_id": {"type": "string", "description": "Fact id behind the observation, emitted by getVulnerabilityDetail (advisory.EPSSObservation.FactID)."}
                      }
                    }},
                    "kev": {"type": "array", "description": "CISA KEV known-exploited observations, emitted by getVulnerabilityDetail (advisory.KEVObservation with the same keys as the evidence page kev items).", "items": {
                      "type": "object",
                      "properties": {
                        "source": {"type": "string", "description": "Source system behind the observation, emitted by getVulnerabilityDetail (advisory.KEVObservation.Source)."},
                        "cve_id": {"type": "string", "description": "CVE id the KEV entry applies to, emitted by getVulnerabilityDetail (advisory.KEVObservation.CVEID)."},
                        "date_added": {"type": "string", "description": "KEV catalog date added, emitted by getVulnerabilityDetail (advisory.KEVObservation.DateAdded)."},
                        "required_action": {"type": "string", "description": "KEV required action, emitted by getVulnerabilityDetail (advisory.KEVObservation.RequiredAction)."},
                        "due_date": {"type": "string", "description": "KEV remediation due date, emitted by getVulnerabilityDetail (advisory.KEVObservation.DueDate)."},
                        "known_ransomware_campaign_use": {"type": "string", "description": "KEV known ransomware campaign use, emitted by getVulnerabilityDetail (advisory.KEVObservation.KnownRansomwareCampaignUse)."},
                        "cwes": {"type": "array", "items": {"type": "string"}, "description": "CWE ids on the KEV entry, emitted by getVulnerabilityDetail (advisory.KEVObservation.CWEs)."},
                        "fact_id": {"type": "string", "description": "Fact id behind the observation, emitted by getVulnerabilityDetail (advisory.KEVObservation.FactID)."}
                      }
                    }},
                    "references": {"type": "array", "description": "Sanitized source reference URLs, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence with the same keys as the evidence page references items).", "items": {
                      "type": "object",
                      "properties": {
                        "source": {"type": "string", "description": "Source system behind the reference, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence.Source)."},
                        "advisory_id": {"type": "string", "description": "Source-native advisory id, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence.ID)."},
                        "cve_id": {"type": "string", "description": "CVE id attached by the source, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence.CVEID)."},
                        "reference_type": {"type": "string", "description": "Reference type, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence.ReferenceType)."},
                        "url": {"type": "string", "description": "Sanitized reference URL, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence.URL)."},
                        "fact_id": {"type": "string", "description": "Fact id behind the reference, emitted by getVulnerabilityDetail (advisory.ReferenceEvidence.FactID)."}
                      }
                    }},
                    "source_disagreements": {"type": "array", "description": "Source-level disagreement without selecting a winner, emitted by getVulnerabilityDetail (advisory.SourceDisagreement with the same keys as the evidence page source_disagreements items).", "items": {
                      "type": "object",
                      "properties": {
                        "field": {"type": "string", "description": "Disagreed field, emitted by getVulnerabilityDetail (advisory.SourceDisagreement.Field)."},
                        "values": {"type": "array", "description": "One value per disagreeing source, emitted by getVulnerabilityDetail (advisory.DisagreementValue).", "items": {
                          "type": "object",
                          "properties": {
                            "source": {"type": "string", "description": "Disagreeing source, emitted by getVulnerabilityDetail (advisory.DisagreementValue.Source)."},
                            "value": {"type": "string", "description": "That source's value, emitted by getVulnerabilityDetail (advisory.DisagreementValue.Value)."}
                          }
                        }}
                      }
                    }},
                    "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids behind the advisory, emitted by getVulnerabilityDetail (advisory.EvidenceRow.EvidenceFactIDs)."},
                    "latest_observed_at": {"type": "string", "description": "Latest source observation time, emitted by getVulnerabilityDetail (advisory.EvidenceRow.LatestObservedAt)."},
                    "source_freshness": {"type": "string", "description": "Source freshness signal, emitted by getVulnerabilityDetail (advisory.EvidenceRow.SourceFreshness)."},
                    "source_confidence": {"type": "string", "description": "Source confidence signal, emitted by getVulnerabilityDetail (advisory.EvidenceRow.SourceConfidence)."}
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
