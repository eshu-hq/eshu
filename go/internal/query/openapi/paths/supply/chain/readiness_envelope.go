// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//

package chain

// supplyChainReadinessEnvelope keeps the list and explain response
// contracts identical without duplicating the packet.ReadinessEnvelope
// JSON. Both routes serve the same owner type (packet.BuildReadiness or
// packet.BuildReadinessUnavailable), so a single fragment interpolated
// into each route's 200 schema stays in lockstep by construction.
const supplyChainReadinessEnvelope = `"readiness": {
                      "type": "object",
                      "description": "Bounded coverage metadata emitted by packet.BuildReadiness or packet.BuildReadinessUnavailable (packet.ReadinessEnvelope) so zero findings can be distinguished from missing target collection or missing required evidence. readiness_unavailable means the readiness lookup itself failed; the findings page is still returned but coverage cannot be classified.",
                      "properties": {
                        "readiness_state": {"type": "string", "enum": ["not_configured", "target_incomplete", "target_skipped", "evidence_incomplete", "ready_zero_findings", "ready_with_findings", "ambiguous_scope", "readiness_unavailable", "unsupported"], "description": "Coverage classification for the bounded vulnerability impact answer, emitted by packet.BuildReadiness (packet.ReadinessState). ambiguous_scope fires when a single explain scope matches multiple reducer-owned findings and the caller must narrow the request; unsupported fires when Eshu observed real target evidence the matcher cannot resolve (unsupported ecosystem, package-manager file with unsupported lockfile feature, VCS/path/URL/editable dependency source, malformed/unsupported SBOM document, or unsupported image target); target_skipped fires when every other source settled and at least one requested derived package carries planner skip evidence, so the system already knows it will never plan that package."},
                        "target_scope": {
                          "type": "object",
                          "description": "Bounded anchors the readiness verdict was computed for, emitted by packet.BuildReadiness (packet.TargetScope).",
                          "properties": {
                              "cve_id": {"type": "string", "description": "CVE anchor echoed by packet.BuildReadiness (packet.TargetScope.CVEID)."},
                              "advisory_id": {"type": "string", "description": "Advisory anchor echoed by packet.BuildReadiness (packet.TargetScope.AdvisoryID)."},
                              "package_id": {"type": "string", "description": "Package anchor echoed by packet.BuildReadiness (packet.TargetScope.PackageID)."},
                              "repository_id": {"type": "string", "description": "Repository anchor echoed by packet.BuildReadiness (packet.TargetScope.RepositoryID)."},
                              "subject_digest": {"type": "string", "description": "Subject digest anchor echoed by packet.BuildReadiness (packet.TargetScope.SubjectDigest)."},
                              "image_ref": {"type": "string", "description": "Image reference anchor echoed by packet.BuildReadiness (packet.TargetScope.ImageRef)."},
                              "ecosystem": {"type": "string", "description": "Ecosystem anchor echoed by packet.BuildReadiness (packet.TargetScope.Ecosystem)."},
                              "workload_id": {"type": "string", "description": "Workload anchor echoed by packet.BuildReadiness (packet.TargetScope.WorkloadID)."},
                              "service_id": {"type": "string", "description": "Service anchor echoed by packet.BuildReadiness (packet.TargetScope.ServiceID)."},
                              "environment": {"type": "string", "description": "Environment anchor echoed by packet.BuildReadiness (packet.TargetScope.Environment)."},
                              "severity": {"type": "string", "description": "Severity anchor echoed by packet.BuildReadiness (packet.TargetScope.Severity)."},
                              "impact_status": {"type": "string", "description": "Impact status anchor echoed by packet.BuildReadiness (packet.TargetScope.Status)."}
                          }
                        },
                        "evidence_sources": {
                          "type": "array",
                          "description": "Per-family source-evidence summary for the requested scope, emitted by packet.BuildReadiness (packet.EvidenceFamily).",
                          "items": {
                            "type": "object",
                            "properties": {
                              "family": {"type": "string", "enum": ["vulnerability.advisory", "vulnerability.exploitability", "package.consumption", "package.registry", "sbom.component", "sbom.attestation", "container_image.identity", "vulnerability.os_package", "scanner_worker.analysis"], "description": "Evidence family emitted by packet.BuildReadiness (packet.EvidenceFamily.Family)."},
                              "fact_count": {"type": "integer", "minimum": 0, "description": "Fact count for the family emitted by packet.BuildReadiness (packet.EvidenceFamily.FactCount)."},
                              "latest_observed_at": {"type": "string", "description": "Latest observation time for the family, emitted by packet.BuildReadiness (packet.EvidenceFamily.LatestObservedAt)."},
                              "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown"], "description": "Family freshness emitted by packet.BuildReadiness (packet.EvidenceFamily.Freshness)."}
                            },
                            "required": ["family", "fact_count"]
                          }
                        },
                        "source_snapshots": {
                          "type": "array",
                          "description": "Vulnerability source snapshot cache metadata emitted by packet.BuildReadiness (packet.SourceSnapshot); raw advisory payloads are not returned.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "source": {"type": "string", "description": "Snapshot source emitted by packet.BuildReadiness (packet.SourceSnapshot.Source)."},
                              "ecosystem": {"type": "string", "description": "Snapshot ecosystem emitted by packet.BuildReadiness (packet.SourceSnapshot.Ecosystem)."},
                              "cache_artifact_version": {"type": "string", "description": "Cache artifact version emitted by packet.BuildReadiness (packet.SourceSnapshot.CacheArtifactVersion)."},
                              "snapshot_digest": {"type": "string", "description": "Snapshot digest emitted by packet.BuildReadiness (packet.SourceSnapshot.SnapshotDigest)."},
                              "last_updated_at": {"type": "string", "description": "Snapshot update time emitted by packet.BuildReadiness (packet.SourceSnapshot.LastUpdatedAt)."},
                              "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown"], "description": "Snapshot freshness emitted by packet.BuildReadiness (packet.SourceSnapshot.Freshness)."},
                              "complete": {"type": "boolean", "description": "Whether the snapshot is complete, emitted by packet.BuildReadiness (packet.SourceSnapshot.Complete)."},
                              "warning_code": {"type": "string", "description": "Snapshot warning code emitted by packet.BuildReadiness (packet.SourceSnapshot.WarningCode)."},
                              "warning_message": {"type": "string", "description": "Snapshot warning message emitted by packet.BuildReadiness (packet.SourceSnapshot.WarningMessage)."}
                            },
                            "required": ["source", "complete"]
                          }
                        },
                        "source_states": {
                          "type": "array",
                          "description": "Durable source freshness, checkpoint, retry, and terminal status per bounded vulnerability source target, emitted by packet.BuildReadiness (packet.SourceState).",
                          "items": {
                            "type": "object",
                            "properties": {
                              "collector_instance_id": {"type": "string", "description": "Collector instance id emitted by packet.BuildReadiness (packet.SourceState.CollectorInstanceID)."},
                              "scope_id": {"type": "string", "description": "Collection scope id emitted by packet.BuildReadiness (packet.SourceState.ScopeID)."},
                              "source": {"type": "string", "description": "Vulnerability source emitted by packet.BuildReadiness (packet.SourceState.Source)."},
                              "ecosystem": {"type": "string", "description": "Ecosystem emitted by packet.BuildReadiness (packet.SourceState.Ecosystem)."},
                              "collection_window": {
                                "type": "object",
                                "description": "Bounded source collection window emitted by packet.BuildReadiness (packet.SourceStateWindow).",
                                "properties": {
                                  "start": {"type": "string", "description": "Collection window start emitted by packet.BuildReadiness (packet.SourceStateWindow.Start)."},
                                  "end": {"type": "string", "description": "Collection window end emitted by packet.BuildReadiness (packet.SourceStateWindow.End)."}
                                }
                              },
                              "last_attempt_at": {"type": "string", "description": "Last collection attempt time emitted by packet.BuildReadiness (packet.SourceState.LastAttemptAt)."},
                              "last_success_at": {"type": "string", "description": "Last collection success time emitted by packet.BuildReadiness (packet.SourceState.LastSuccessAt)."},
                              "next_retry_at": {"type": "string", "description": "Next retry time emitted by packet.BuildReadiness (packet.SourceState.NextRetryAt)."},
                              "last_error_class": {"type": "string", "description": "Last error class emitted by packet.BuildReadiness (packet.SourceState.LastErrorClass)."},
                              "freshness_state": {"type": "string", "enum": ["not_configured", "pending", "fresh", "stale", "rate_limited", "failed", "partial", "skipped"], "description": "Durable collection state emitted by packet.BuildReadiness (packet.SourceState.FreshnessState). skipped means every unmatched package on this derived aggregate carries planner skip evidence, so nothing is in flight for it."},
                              "terminal_status": {"type": "string", "enum": ["pending", "succeeded", "partial", "failed_retryable", "failed_terminal", "skipped"], "description": "Terminal collection status emitted by packet.BuildReadiness (packet.SourceState.TerminalStatus)."},
                              "result_count": {"type": "integer", "description": "Result count emitted by packet.BuildReadiness (packet.SourceState.ResultCount)."},
                              "warning_count": {"type": "integer", "description": "Warning count emitted by packet.BuildReadiness (packet.SourceState.WarningCount)."},
                              "updated_at": {"type": "string", "description": "State update time emitted by packet.BuildReadiness (packet.SourceState.UpdatedAt)."},
                              "derived": {"type": "boolean", "description": "Whether this row is a derived aggregate standing for every derived owned-package OSV scope, emitted by packet.BuildReadiness (packet.SourceState.Derived)."},
                              "scope_count": {"type": "integer", "description": "Distinct derived scopes kept, emitted by packet.BuildReadiness (packet.SourceState.ScopeCount); set only on derived rows."},
                              "package_count": {"type": "integer", "description": "Requested packages a scope covered or expects, emitted by packet.BuildReadiness (packet.SourceState.PackageCount); set only on derived rows."},
                              "unmatched_package_count": {"type": "integer", "description": "Requested packages no derived scope covered, emitted by packet.BuildReadiness (packet.SourceState.UnmatchedPackageCount); set only on derived rows."},
                              "fresh_count": {"type": "integer", "description": "Kept scopes in fresh state, emitted by packet.BuildReadiness (packet.SourceState.FreshCount); set only on derived rows."},
                              "stale_count": {"type": "integer", "description": "Kept scopes in stale state, emitted by packet.BuildReadiness (packet.SourceState.StaleCount); set only on derived rows."},
                              "failed_count": {"type": "integer", "description": "Kept scopes in failed state, emitted by packet.BuildReadiness (packet.SourceState.FailedCount); set only on derived rows."},
                              "rate_limited_count": {"type": "integer", "description": "Kept scopes in rate-limited state, emitted by packet.BuildReadiness (packet.SourceState.RateLimitedCount); set only on derived rows."},
                              "partial_count": {"type": "integer", "description": "Kept scopes in partial state, emitted by packet.BuildReadiness (packet.SourceState.PartialCount); set only on derived rows."},
                              "pending_count": {"type": "integer", "description": "Kept scopes in pending state, emitted by packet.BuildReadiness (packet.SourceState.PendingCount); set only on derived rows."},
                              "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Kept scope ids, worst state first, emitted by packet.BuildReadiness (packet.SourceState.ScopeIDs); set only on derived rows."},
                              "multi_cve_target": {"type": "boolean", "description": "Whether the target lists several CVEs, emitted by packet.BuildReadiness (packet.SourceState.MultiCVETarget)."},
                              "skipped_package_count": {"type": "integer", "description": "Requested packages no derived scope covered that carry planner skip evidence, emitted by packet.BuildReadiness (packet.SourceState.SkippedPackageCount); set only on derived aggregate rows."},
                              "skip_reasons": {"type": "array", "items": {"type": "string"}, "description": "Distinct planner skip reasons behind the skipped packages, such as derived_target_non_exact_version, emitted by packet.BuildReadiness (packet.SourceState.SkipReasons). Reasons only, never package names."},
                              "retired_releases": {"type": "array", "items": {"type": "string"}, "description": "Derived releases the fleet probe retired for this aggregate, as source:distro:release (for example osv:alpine:v3.17), emitted by packet.BuildReadiness (packet.SourceState.RetiredReleases): a release no active installed package still carries stops counting even when its last state is not fresh. A retired release never appears in incomplete_reasons."}
                            },
                            "required": ["collector_instance_id", "scope_id", "source", "freshness_state", "terminal_status", "result_count", "warning_count"]
                          }
                        },
                        "unsupported_targets": {
                          "type": "array",
                          "description": "Observed vulnerability target evidence the matcher cannot resolve, emitted by packet.BuildReadiness (packet.UnsupportedTarget). Each entry is bounded coverage-gap evidence; counts MUST NOT be interpreted as clean or affected results. Surfaced alongside readiness_state=unsupported when no finding can be admitted for the scope, and additively for ready states when only some targets are unsupported.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "target_kind": {"type": "string", "enum": ["ecosystem", "package_manager_file", "dependency_source", "sbom_target", "package_registry_metadata", "image_target"], "description": "Family of unsupported target observed: dependency in an unsupported ecosystem, package-manager file with an unsupported lockfile feature, VCS/path/URL/editable dependency source evidence, malformed/unsupported SBOM document tied to the requested subject digest, oversized package-registry metadata, or container image target without a supported analyzer."},
                              "reason": {"type": "string", "description": "Stable reason code explaining why the target is unsupported (e.g., unsupported_ecosystem, lockfile_unsupported_feature, vcs_dependency_unsupported, path_dependency_unsupported, unsupported_field, malformed_document, metadata_too_large)."},
                              "count": {"type": "integer", "minimum": 1, "description": "Unsupported target count emitted by packet.BuildReadiness (packet.UnsupportedTarget.Count)."},
                              "ecosystem": {"type": "string", "description": "Ecosystem of the unsupported target, emitted by packet.BuildReadiness (packet.UnsupportedTarget.Ecosystem)."},
                              "lockfile_flavor": {"type": "string", "description": "Lockfile flavor of the unsupported target, emitted by packet.BuildReadiness (packet.UnsupportedTarget.LockfileFlavor)."},
                              "feature_token": {"type": "string", "description": "Feature token of the unsupported target, emitted by packet.BuildReadiness (packet.UnsupportedTarget.FeatureToken)."}
                            },
                            "required": ["target_kind", "reason", "count"]
                          }
                        },
                        "missing_evidence": {"type": "array", "description": "Missing-evidence vocabulary for the scope, emitted by packet.BuildReadiness (packet.ReadinessEnvelope.MissingEvidence); includes ambiguous_scope on ambiguous explain responses.", "items": {"type": "string", "enum": ["advisory_sources", "owned_packages", "package_registry_metadata", "sbom_or_image_evidence", "target_collection_incomplete", "ambiguous_scope", "readiness_unavailable", "unsupported_targets"]}},
                        "incomplete_reasons": {"type": "array", "items": {"type": "string"}, "description": "Collector-emitted reasons explaining why source collection is still in flight, emitted by packet.BuildReadiness (packet.ReadinessEnvelope.IncompleteReasons); only present when readiness_state is target_incomplete. Skip entries read osv:skipped:<reason> and accompany target_skipped; they can also ride along when another scope is still in flight. A derived package release that holds coverage back reads osv:<state>:<distro>:<release>, for example osv:failed:alpine:v3.17, beside the plain osv:<state> entry."},
                        "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown", "pending", "rate_limited", "failed", "partial"], "description": "Aggregate freshness emitted by packet.BuildReadiness (packet.ReadinessEnvelope.Freshness)."},
                        "counts": {
                          "type": "object",
                          "description": "Numeric coverage for the answer, emitted by packet.BuildReadiness (packet.ReadinessCounts).",
                          "properties": {
                            "findings_returned": {"type": "integer", "description": "Number of findings in this page, emitted by packet.BuildReadiness (packet.ReadinessCounts.FindingsReturned); not the total population. Compare with truncated to know if more pages exist. For ambiguous explain responses, this is the matched reducer finding count observed by the ambiguity probe while individual findings are withheld."},
                            "findings_truncated": {"type": "boolean", "description": "Whether the findings considered exceed the page, emitted by packet.BuildReadiness (packet.ReadinessCounts.FindingsTruncated)."},
                            "findings_by_status": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Counts by impact_status across the returned page only, emitted by packet.BuildReadiness (packet.ReadinessCounts.FindingsByStatus)."},
                            "evidence_facts_total": {"type": "integer", "description": "Total evidence facts considered, emitted by packet.BuildReadiness (packet.ReadinessCounts.EvidenceFactsTotal)."}
                          }
                        }
                      },
                      "required": ["readiness_state", "target_scope", "evidence_sources", "freshness", "counts"]
                    }`
