// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// SecurityAlerts is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/security-alerts/reconciliations` route. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const SecurityAlerts = `
    "/api/v0/supply-chain/security-alerts/reconciliations": {
      "get": {
        "summary": "List provider security alert reconciliations",
        "description": "Requires limit plus repository_id, provider, package_id, cve_id, or ghsa_id. provider_state and reconciliation_status filter anchored pages only.",
        "operationId": "listSecurityAlertReconciliations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty page.", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string", "enum": ["open", "fixed", "dismissed", "auto_dismissed"]}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string", "enum": ["matched", "unmatched", "stale", "dismissed", "fixed", "provider_only", "unsupported", "ambiguous"]}},
          {"name": "after_reconciliation_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Provider security alert reconciliation page. Provider alert state and Eshu impact state are separate fields.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "reconciliations": {
                      "type": "array",
                      "description": "Reducer-owned reconciliation rows emitted by listSecurityAlertReconciliations (SecurityAlertReconciliationRow).",
                      "items": {
                        "type": "object", "description": "One security-alert reconciliation row. emitted by listSecurityAlertReconciliations (SecurityAlertReconciliationRow).",
                        "properties": {
                          "reconciliation_id": {"type": "string", "description": "Reconciliation fact id emitted by listSecurityAlertReconciliations (SecurityAlertReconciliationRow.ReconciliationID)."},
                          "provider_alert": {
                            "type": "object",
                            "description": "Provider-reported alert state preserved by the reconciliation read model, emitted by listSecurityAlertReconciliations (ProviderSecurityAlertRow via alerts.decodeRow).",
                            "properties": {
                              "provider": {"type": "string", "description": "Alert provider emitted by alerts.decodeRow (ProviderSecurityAlertRow.Provider)."},
                              "provider_alert_id": {"type": "string", "description": "Provider alert id emitted by alerts.decodeRow (ProviderSecurityAlertRow.ProviderAlertID)."},
                              "provider_alert_number": {"type": "integer", "description": "Provider alert number emitted by alerts.decodeRow (ProviderSecurityAlertRow.ProviderAlertNumber)."},
                              "provider_state": {"type": "string", "description": "Provider alert state emitted by alerts.decodeRow (ProviderSecurityAlertRow.ProviderState)."},
                              "repository_id": {"type": "string", "description": "Repository the alert was collected from, emitted by alerts.decodeRow (ProviderSecurityAlertRow.RepositoryID)."},
                              "package_id": {"type": "string", "description": "Affected package id emitted by alerts.decodeRow (ProviderSecurityAlertRow.PackageID)."},
                              "ecosystem": {"type": "string", "description": "Package ecosystem emitted by alerts.decodeRow (ProviderSecurityAlertRow.Ecosystem)."},
                              "package_name": {"type": "string", "description": "Package name emitted by alerts.decodeRow (ProviderSecurityAlertRow.PackageName)."},
                              "manifest_path": {"type": "string", "description": "Manifest path emitted by alerts.decodeRow (ProviderSecurityAlertRow.ManifestPath)."},
                              "dependency_scope": {"type": "string", "description": "Dependency scope emitted by alerts.decodeRow (ProviderSecurityAlertRow.DependencyScope)."},
                              "relationship": {"type": "string", "description": "Dependency relationship emitted by alerts.decodeRow (ProviderSecurityAlertRow.Relationship)."},
                              "ghsa_ids": {"type": "array", "items": {"type": "string"}, "description": "GHSA ids attached to the alert, emitted by alerts.decodeRow (ProviderSecurityAlertRow.GHSAIDs)."},
                              "cve_ids": {"type": "array", "items": {"type": "string"}, "description": "CVE ids attached to the alert, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CVEIDs)."},
                              "vulnerable_range": {"type": "string", "description": "Provider-reported vulnerable range emitted by alerts.decodeRow (ProviderSecurityAlertRow.VulnerableRange)."},
                              "patched_version": {"type": "string", "description": "Provider-reported patched version emitted by alerts.decodeRow (ProviderSecurityAlertRow.PatchedVersion)."},
                              "severity": {"type": "string", "description": "Provider-reported severity emitted by alerts.decodeRow (ProviderSecurityAlertRow.Severity)."},
                              "cvss": {"type": "object", "additionalProperties": {}, "description": "Provider-reported CVSS map with provider-defined keys, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CVSS)."},
                              "epss": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Provider-reported EPSS entries as string pairs, emitted by alerts.decodeRow (ProviderSecurityAlertRow.EPSS)."},
                              "cwes": {"type": "array", "items": {"type": "object", "description": "One provider CWE entry (open string map). emitted by alerts.decodeRow (ProviderSecurityAlertRow.CWEs).", "additionalProperties": {"type": "string"}}, "description": "Provider-reported CWE entries as string maps, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CWEs)."},
                              "summary": {"type": "string", "description": "Provider alert summary emitted by alerts.decodeRow (ProviderSecurityAlertRow.Summary)."},
                              "source_url": {"type": "string", "description": "Provider alert URL emitted by alerts.decodeRow (ProviderSecurityAlertRow.SourceURL)."},
                              "created_at": {"type": "string", "description": "Provider alert creation time emitted by alerts.decodeRow (ProviderSecurityAlertRow.CreatedAt)."},
                              "updated_at": {"type": "string", "description": "Provider alert update time emitted by alerts.decodeRow (ProviderSecurityAlertRow.UpdatedAt)."},
                              "fixed_at": {"type": "string", "description": "Provider alert fix time emitted by alerts.decodeRow (ProviderSecurityAlertRow.FixedAt)."},
                              "dismissed_at": {"type": "string", "description": "Provider alert dismissal time emitted by alerts.decodeRow (ProviderSecurityAlertRow.DismissedAt)."},
                              "collection_coverage_state": {"type": "string", "enum": ["complete", "incomplete"], "description": "Whether the provider read behind this row was complete, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CollectionCoverageState)."},
                              "collection_truncated": {"type": "boolean", "description": "Whether the provider read behind this row was truncated, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CollectionTruncated)."},
                              "collection_pages_fetched": {"type": "integer", "description": "Provider pages fetched behind this row, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CollectionPagesFetched)."},
                              "collection_state_filter": {"type": "string", "enum": ["open"], "description": "Provider state filter applied during collection, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CollectionStateFilter)."},
                              "collection_incomplete_reasons": {"type": "array", "items": {"type": "string"}, "description": "Reasons the provider read was incomplete, emitted by alerts.decodeRow (ProviderSecurityAlertRow.CollectionIncompleteReasons)."}
                            }
                          },
                          "eshu_package": {
                            "type": "object",
                            "description": "Eshu-owned dependency evidence matched to the provider alert. observed_version is populated only from Eshu package evidence, never provider alert fields.",
                            "properties": {
                              "observed_version": {"type": "string", "description": "Installed or observed package version from Eshu-owned dependency evidence when exact evidence exists."},
                              "requested_range": {"type": "string", "description": "Manifest/requested dependency range preserved separately from the installed version."},
                              "dependency_range": {"type": "string", "description": "Raw Eshu dependency range or lockfile version evidence used during matching."},
                              "dependency_evidence_id": {"type": "string", "description": "Dependency evidence id behind the match, emitted by alerts.decodeRow (SecurityAlertEshuPackageRow.DependencyEvidenceID)."},
                              "dependency_evidence_kind": {"type": "string", "description": "Dependency evidence kind behind the match, emitted by alerts.decodeRow (SecurityAlertEshuPackageRow.DependencyEvidenceKind)."},
                              "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Explicit evidence gaps such as missing or malformed installed package version evidence."}
                            }
                          },
                          "eshu_impact": {
                            "type": "object",
                            "description": "Reducer-owned impact state matched to the alert, when Eshu has admitted owned evidence.",
                            "properties": {
                              "impact_status": {"type": "string", "description": "Reducer-owned impact status matched to the alert, emitted by alerts.decodeRow (SecurityAlertEshuImpactRow.ImpactStatus)."},
                              "finding_id": {"type": "string", "description": "Reducer-owned impact finding id matched to the alert, emitted by alerts.decodeRow (SecurityAlertEshuImpactRow.FindingID)."}
                            }
                          },
                          "reconciliation_status": {"type": "string", "enum": ["matched", "unmatched", "stale", "dismissed", "fixed", "provider_only", "unsupported", "ambiguous"], "description": "Reducer reconciliation outcome emitted by alerts.decodeRow (SecurityAlertReconciliationRow.ReconciliationStatus)."},
                          "reason": {"type": "string", "description": "Human reason for the reconciliation outcome, emitted by alerts.decodeRow (SecurityAlertReconciliationRow.Reason)."},
                          "reason_code": {"type": "string", "description": "Machine reason code for the reconciliation outcome, emitted by alerts.decodeRow (SecurityAlertReconciliationRow.ReasonCode)."},
                          "impact_wait_outcome": {"type": "string", "enum": ["gave_up_bound_exceeded", "gave_up_unmatched"], "description": "Present only on an unmatched row the reducer wrote after it stopped waiting for impact evidence: gave_up_unmatched (impact work was never enqueued, dead-lettered, failed, or superseded) or gave_up_bound_exceeded (impact work stayed in flight with no sign of life past the readiness bound). Absent on a genuine no-match, where every impact work item for the generation succeeded and none matched the advisory. Rows written by a binary from before #8956, including during a rolling deploy, carry no impact_wait_outcome, so an untagged unmatched row from such a generation is not proof of a genuine no-match."},
                          "missing_evidence": {
                            "type": "array",
                            "description": "Structured row-level gaps that explain provider-only, stale, unsupported, or ambiguous outcomes without embedding raw provider payloads, emitted by alerts.decodeRow (SecurityAlertMissingEvidence).",
                            "items": {
                              "type": "object", "description": "One missing-evidence entry. emitted by alerts.decodeRow (SecurityAlertMissingEvidence).",
                              "properties": {
                                "kind": {"type": "string", "enum": ["owned_dependency", "impact_finding", "current_manifest", "ecosystem_matcher"], "description": "Gap kind emitted by alerts.decodeRow (SecurityAlertMissingEvidence.Kind)."},
                                "reason": {"type": "string", "description": "Human reason for the gap, emitted by alerts.decodeRow (SecurityAlertMissingEvidence.Reason)."},
                                "evidence_id": {"type": "string", "description": "Evidence id behind the gap when known, emitted by alerts.decodeRow (SecurityAlertMissingEvidence.EvidenceID)."},
                                "detail": {"type": "string", "description": "Gap detail emitted by alerts.decodeRow (SecurityAlertMissingEvidence.Detail)."}
                              },
                              "required": ["kind", "reason"]
                            }
                          },
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids behind the row, emitted by alerts.decodeRow (SecurityAlertReconciliationRow.EvidenceFactIDs)."},
                          "source_freshness": {"type": "string", "description": "Source freshness signal emitted by alerts.decodeRow (SecurityAlertReconciliationRow.SourceFreshness)."},
                          "source_confidence": {"type": "string", "description": "Source confidence signal emitted by alerts.decodeRow (SecurityAlertReconciliationRow.SourceConfidence)."}
                        },
                        "required": ["reconciliation_id", "provider_alert", "eshu_package", "eshu_impact", "reconciliation_status"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listSecurityAlertReconciliations."},
                    ` + securityAlertCoverageSchema + `,
                    "limit": {"type": "integer", "description": "Requested page size echoed by listSecurityAlertReconciliations."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listSecurityAlertReconciliations."},
                    "next_cursor": {"type": "object", "description": "Keyset cursor emitted by listSecurityAlertReconciliations only when truncated carries the last reconciliation id forward; absent otherwise.", "properties": {
                      "after_reconciliation_id": {"type": "string", "description": "Reconciliation id of the last row, emitted by listSecurityAlertReconciliations for the next page cursor."}
                    }}
                  }
                }
              }
            }
          }
        }
      }
    },
`
