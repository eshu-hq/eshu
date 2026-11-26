// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

const impactExplain = `
    "/api/v0/supply-chain/impact/explain": {
      "get": {
        "summary": "Explain one supply-chain impact finding",
        "operationId": "explainSupplyChainImpact",
        "x-scoped-token-support": true,
        "description": "Scoped tokens receive an explanation intersected with granted repositories: the matched finding's repository_id/scope_id must fall within the token's grant, so an out-of-grant finding_id or a bare advisory/CVE plus package/image/workload/service anchor that would otherwise resolve to another tenant's finding returns a no_finding outcome instead of leaking it. A scoped token with no granted repositories always receives the bounded no-evidence explanation without a store read. At least one anchor (finding_id, or advisory_id/cve_id plus package_id, repository_id, subject_digest, image_ref, workload_id, or service_id) is required; a request with none returns 400.",
        "parameters": [
          {"name": "finding_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of a no-evidence explanation.", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "description": "Exact image reference stored on reducer-owned impact findings.", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "description": "Reducer-admitted workload anchor. Missing runtime mapping remains missing evidence.", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "description": "Reducer-admitted service anchor derived from workload/service evidence.", "schema": {"type": "string"}}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded finding explanation, no-evidence response, or ambiguous-scope refusal envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "outcome": {"type": "string", "enum": ["finding_explained", "no_finding", "ambiguous_scope"], "description": "Explanation outcome emitted by impact.BuildExplanation, impact.BuildNoEvidenceExplanation, or impact.BuildAmbiguousExplanation (packet.ExplanationResult.Outcome)."},
                    "evidence_packet_handle": {"type": "string", "description": "Opaque stable handle for this bounded explanation packet, emitted by impact.BuildExplanation (packet.ExplanationResult.EvidencePacketHandle). Finding packets use the returned finding id; no-finding scopes use a hashed normalized scope so private anchors are not exposed in the handle."},
                    "input": {"type": "object", "description": "Bounded scope echoed back by explainImpact (packet.ExplanationFilter); scoped-token grants are excluded from JSON.", "properties": {
                      "finding_id": {"type": "string", "description": "Finding id anchor echoed by explainImpact (packet.ExplanationFilter.FindingID)."},
                      "advisory_id": {"type": "string", "description": "Advisory anchor echoed by explainImpact (packet.ExplanationFilter.AdvisoryID)."},
                      "cve_id": {"type": "string", "description": "CVE anchor echoed by explainImpact (packet.ExplanationFilter.CVEID)."},
                      "package_id": {"type": "string", "description": "Package anchor echoed by explainImpact (packet.ExplanationFilter.PackageID)."},
                      "repository_id": {"type": "string", "description": "Repository anchor echoed by explainImpact (packet.ExplanationFilter.RepositoryID)."},
                      "subject_digest": {"type": "string", "description": "Subject digest anchor echoed by explainImpact (packet.ExplanationFilter.SubjectDigest)."},
                      "image_ref": {"type": "string", "description": "Image reference anchor echoed by explainImpact (packet.ExplanationFilter.ImageRef)."},
                      "workload_id": {"type": "string", "description": "Workload anchor echoed by explainImpact (packet.ExplanationFilter.WorkloadID)."},
                      "service_id": {"type": "string", "description": "Service anchor echoed by explainImpact (packet.ExplanationFilter.ServiceID)."}
                    }},
                    "finding": {
                      "type": "object",
                      "description": "The same finding shape returned by the list route, emitted by explainImpact (packet.FindingResult via impact.BuildFindingResult), including read-time runtime context, exact digest-bound Kubernetes runtime refs, and bounded probe metadata.",
                      "properties": {
                        "kubernetes_runtime_workload_refs": {"type": "array", "description": "Exact digest-bound Kubernetes workload refs emitted by explainImpact (packet.KubernetesRuntimeWorkloadRef).", "items": {"type": "object", "description": "One digest-bound Kubernetes workload ref. emitted by explainImpact (packet.KubernetesRuntimeWorkloadRef).", "properties": {"workload_uid": {"type": "string", "description": "Workload uid emitted by explainImpact (packet.KubernetesRuntimeWorkloadRef.UID)."}, "cluster_id": {"type": "string", "description": "Cluster id emitted by explainImpact (packet.KubernetesRuntimeWorkloadRef.ClusterID)."}, "namespace": {"type": "string", "description": "Workload namespace emitted by explainImpact (packet.KubernetesRuntimeWorkloadRef.Namespace)."}, "name": {"type": "string", "description": "Workload name emitted by explainImpact (packet.KubernetesRuntimeWorkloadRef.Name)."}}, "required": ["workload_uid"]}},
                        "kubernetes_runtime_probe": {"type": "object", "description": "The page-weighted digest probe budget and nullable truncation state from the list finding contract, emitted by explainImpact (packet.KubernetesRuntimeProbeMetadata); a single explanation keeps the full 200-reference cap.", "properties": {"candidate_limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Digest-local candidate budget emitted by explainImpact (packet.KubernetesRuntimeProbeMetadata.CandidateLimit)."}, "workload_refs_truncated": {"type": "boolean", "nullable": true, "description": "Whether authorized refs exceeded the budget, emitted by explainImpact (packet.KubernetesRuntimeProbeMetadata.WorkloadRefsTruncated); null when authorization hides the answer."}}, "required": ["candidate_limit", "workload_refs_truncated"]},
                        "runtime_context": ` + RuntimeContext + `
                      },
                      "additionalProperties": true
                    },
                    "advisory": {
                      "type": "object",
                      "description": "Advisory evidence selected for the finding, emitted by explainImpact (packet.AdvisoryExplanation).",
                      "properties": {
                        "cve_id": {"type": "string", "description": "CVE id emitted by explainImpact (packet.AdvisoryExplanation.CVEID)."},
                        "advisory_id": {"type": "string", "description": "Source advisory id emitted by explainImpact (packet.AdvisoryExplanation.AdvisoryID)."},
                        "vulnerable_range": {"type": "string", "description": "Source-reported vulnerable range emitted by explainImpact (packet.AdvisoryExplanation.VulnerableRange)."},
                        "range_source": {"type": "string", "description": "Source of the vulnerable range emitted by explainImpact (packet.AdvisoryExplanation.RangeSource)."},
                        "selected_severity_source": {"type": "string", "description": "Severity source emitted by explainImpact (packet.AdvisoryExplanation.SelectedSeveritySource)."},
                        "selected_fixed_version_source": {"type": "string", "description": "Fixed-version source emitted by explainImpact (packet.AdvisoryExplanation.SelectedFixedVersionSource)."},
                        "sources": {"type": "array", "description": "Source observations behind the advisory, emitted by explainImpact (packet.AdvisorySource).", "items": {
                          "type": "object", "description": "One advisory source evidence row. emitted by explainImpact (packet.AdvisorySource).",
                          "properties": {
                            "source": {"type": "string", "description": "Advisory source emitted by explainImpact (packet.AdvisorySource.Source)."},
                            "advisory_id": {"type": "string", "description": "Source-native advisory id emitted by explainImpact (packet.AdvisorySource.AdvisoryID)."},
                            "source_updated_at": {"type": "string", "description": "Source update time emitted by explainImpact (packet.AdvisorySource.SourceUpdatedAt)."},
                            "withdrawn_at": {"type": "string", "description": "Source withdrawal time emitted by explainImpact (packet.AdvisorySource.WithdrawnAt)."},
                            "status": {"type": "string", "description": "Source-reported lifecycle status, verbatim, emitted by explainImpact (packet.AdvisorySource.Status)."},
                            "replaced_by": {"type": "array", "items": {"type": "string"}, "description": "Replacement CVE ids stated by the source, emitted by explainImpact (packet.AdvisorySource.ReplacedBy)."}
                          },
                          "required": ["source"]
                        }},
                        "references": {"type": "array", "items": {"type": "string"}, "description": "Reference URLs behind the advisory, emitted by explainImpact (packet.AdvisoryExplanation.References)."}
                      }
                    },
                    "component": {
                      "type": "object",
                      "description": "Package matched by reducer-owned impact evidence, emitted by explainImpact (packet.ComponentExplanation).",
                      "properties": {
                        "package_id": {"type": "string", "description": "Package id emitted by explainImpact (packet.ComponentExplanation.PackageID)."},
                        "ecosystem": {"type": "string", "description": "Package ecosystem emitted by explainImpact (packet.ComponentExplanation.Ecosystem)."},
                        "package_name": {"type": "string", "description": "Package name emitted by explainImpact (packet.ComponentExplanation.PackageName)."},
                        "purl": {"type": "string", "description": "Package URL emitted by explainImpact (packet.ComponentExplanation.PURL)."},
                        "product_criteria": {"type": "string", "description": "NVD product criteria emitted by explainImpact (packet.ComponentExplanation.ProductCriteria)."},
                        "match_criteria_id": {"type": "string", "description": "NVD match criteria id emitted by explainImpact (packet.ComponentExplanation.MatchCriteriaID)."},
                        "observed_version": {"type": "string", "description": "Observed version emitted by explainImpact (packet.ComponentExplanation.ObservedVersion)."},
                        "manifest_range": {"type": "string", "description": "Manifest range emitted by explainImpact (packet.ComponentExplanation.ManifestRange)."}
                      }
                    },
                    "version": {
                      "type": "object",
                      "description": "Version observations kept separate from advisory ranges and remediation, emitted by explainImpact (packet.VersionExplanation).",
                      "properties": {
                        "observed_version": {"type": "string", "description": "Observed version emitted by explainImpact (packet.VersionExplanation.ObservedVersion)."},
                        "manifest_range": {"type": "string", "description": "Manifest range emitted by explainImpact (packet.VersionExplanation.ManifestRange)."},
                        "vulnerable_range": {"type": "string", "description": "Vulnerable range emitted by explainImpact (packet.VersionExplanation.VulnerableRange)."},
                        "fixed_version": {"type": "string", "description": "Fixed version emitted by explainImpact (packet.VersionExplanation.FixedVersion)."},
                        "version_evidence": {"type": "string", "enum": ["exact", "range_only", "missing"], "description": "Version evidence tier emitted by explainImpact (packet.VersionExplanation.VersionEvidence)."}
                      },
                      "required": ["version_evidence"]
                    },
                    "dependency_chain": {
                      "type": "object",
                      "description": "Direct versus transitive package evidence emitted by explainImpact (packet.DependencyChain); present when a manifest, lockfile, or SBOM source provided it.",
                      "properties": {
                        "path": {"type": "array", "items": {"type": "string"}, "description": "Dependency path emitted by explainImpact (packet.DependencyChain.Path)."},
                        "depth": {"type": "integer", "description": "Dependency depth emitted by explainImpact (packet.DependencyChain.Depth)."},
                        "direct_dependency": {"type": "boolean", "description": "Whether the finding is a direct dependency, emitted by explainImpact (packet.DependencyChain.DirectDependency)."}
                      }
                    },
                    "anchors": {
                      "type": "object",
                      "description": "Scoped evidence anchors for the explanation, emitted by explainImpact (packet.ExplanationAnchors); empty families stay omitted rather than inferred.",
                      "properties": {
                        "repository_id": {"type": "string", "description": "Repository anchor emitted by explainImpact (packet.ExplanationAnchors.RepositoryID)."},
                        "subject_digest": {"type": "string", "description": "Subject digest anchor emitted by explainImpact (packet.ExplanationAnchors.SubjectDigest)."},
                        "manifest_paths": {"type": "array", "items": {"type": "string"}, "description": "Manifest paths emitted by explainImpact (packet.ExplanationAnchors.ManifestPaths)."},
                        "lockfile_paths": {"type": "array", "items": {"type": "string"}, "description": "Lockfile paths emitted by explainImpact (packet.ExplanationAnchors.LockfilePaths)."},
                        "sbom_documents": {"type": "array", "items": {"type": "string"}, "description": "SBOM documents emitted by explainImpact (packet.ExplanationAnchors.SBOMDocuments)."},
                        "image_digests": {"type": "array", "items": {"type": "string"}, "description": "Image digests emitted by explainImpact (packet.ExplanationAnchors.ImageDigests)."},
                        "image_refs": {"type": "array", "items": {"type": "string"}, "description": "Image references emitted by explainImpact (packet.ExplanationAnchors.ImageRefs)."},
                        "workloads": {"type": "array", "items": {"type": "string"}, "description": "Workloads emitted by explainImpact (packet.ExplanationAnchors.Workloads)."},
                        "deployments": {"type": "array", "items": {"type": "string"}, "description": "Deployments emitted by explainImpact (packet.ExplanationAnchors.Deployments)."},
                        "services": {"type": "array", "items": {"type": "string"}, "description": "Services emitted by explainImpact (packet.ExplanationAnchors.Services)."},
                        "environments": {"type": "array", "items": {"type": "string"}, "description": "Environments emitted by explainImpact (packet.ExplanationAnchors.Environments)."},
                        "catalog_entities": {"type": "array", "items": {"type": "string"}, "description": "Catalog entities emitted by explainImpact (packet.ExplanationAnchors.CatalogEntities)."},
                        "catalog_owners": {"type": "array", "items": {"type": "string"}, "description": "Catalog owners emitted by explainImpact (packet.ExplanationAnchors.CatalogOwners)."},
                        "provider_alerts": {"type": "array", "description": "Provider alert evidence preserved without promoting it into owned truth, emitted by explainImpact (packet.ProviderAlertAnchor).", "items": {
                          "type": "object", "description": "One provider alert row. emitted by explainImpact (packet.ProviderAlertAnchor).",
                          "properties": {
                            "provider": {"type": "string", "description": "Alert provider emitted by explainImpact (packet.ProviderAlertAnchor.Provider)."},
                            "alert_id": {"type": "string", "description": "Provider alert id emitted by explainImpact (packet.ProviderAlertAnchor.AlertID)."},
                            "state": {"type": "string", "description": "Provider alert state emitted by explainImpact (packet.ProviderAlertAnchor.State)."},
                            "manifest_path": {"type": "string", "description": "Manifest path of the alert emitted by explainImpact (packet.ProviderAlertAnchor.ManifestPath)."}
                          }
                        }},
                        "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids emitted by explainImpact (packet.ExplanationAnchors.EvidenceFactIDs)."}
                      }
                    },
                    "impact_path": {
                      "type": "array",
                      "description": "Reducer-owned present and missing hops from advisory/package evidence to repository, image, workload, deployment, service, and environment evidence, emitted by explainImpact (packet.PathHop). Missing hops remain explicit and are not inferred from names or tags.",
                      "items": {
                        "type": "object", "description": "One impact path row. emitted by explainImpact (packet.PathHop).",
                        "properties": {
                          "hop": {"type": "string", "description": "Hop name emitted by explainImpact (packet.PathHop.Hop)."},
                          "status": {"type": "string", "enum": ["present", "missing_evidence"], "description": "Hop status emitted by explainImpact (packet.PathHop.Status)."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids behind the hop, emitted by explainImpact (packet.PathHop.EvidenceFactIDs)."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence gaps on the hop, emitted by explainImpact (packet.PathHop.MissingEvidence)."}
                        },
                        "required": ["hop", "status"]
                      }
                    },
                    "evidence": {
                      "type": "array",
                      "description": "Compact previews of the bounded evidence fact ids referenced by the finding, emitted by explainImpact (packet.EvidenceFactSummary); raw advisory and provider payloads are not returned.",
                      "items": {
                        "type": "object", "description": "One evidence row. emitted by explainImpact (packet.EvidenceFactSummary).",
                        "properties": {
                          "fact_id": {"type": "string", "description": "Evidence fact id emitted by explainImpact (packet.EvidenceFactSummary.FactID)."},
                          "fact_kind": {"type": "string", "description": "Evidence fact kind emitted by explainImpact (packet.EvidenceFactSummary.FactKind)."},
                          "source_system": {"type": "string", "description": "Source system emitted by explainImpact (packet.EvidenceFactSummary.SourceSystem)."},
                          "source_confidence": {"type": "string", "description": "Source confidence emitted by explainImpact (packet.EvidenceFactSummary.SourceConfidence)."},
                          "observed_at": {"type": "string", "description": "Observation time emitted by explainImpact (packet.EvidenceFactSummary.ObservedAt)."}
                        },
                        "required": ["fact_id", "fact_kind"]
                      }
                    },
                    ` + supplyChainReadinessEnvelope + `,
                    "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence gaps for the explanation scope, emitted by explainImpact (packet.ExplanationResult.MissingEvidence); includes impact_finding on no-finding scopes and ambiguous_scope on refusal scopes."},
                    "freshness": {
                      "type": "object",
                      "description": "Evidence observation freshness for the explanation payload, emitted by explainImpact (packet.ExplanationFreshness).",
                      "properties": {
                        "state": {"type": "string", "enum": ["fresh", "stale", "unknown"], "description": "Freshness state emitted by explainImpact (packet.ExplanationFreshness.State)."},
                        "latest_observed_at": {"type": "string", "description": "Latest observation time emitted by explainImpact (packet.ExplanationFreshness.LatestObservedAt)."},
                        "evidence_fact_count": {"type": "integer", "description": "Evidence fact count emitted by explainImpact (packet.ExplanationFreshness.EvidenceFactCount)."}
                      },
                      "required": ["state", "evidence_fact_count"]
                    },
                    "remediation": {
                      "type": "object",
                      "description": "Advisory-only safe-upgrade recommendation for this finding (issue #595), emitted by explainImpact (packet.Remediation, enriched with vulnerable_range, manifest_range, observed_version, and dependency direct/transitive evidence pulled from the referenced source facts). Mirrors the remediation block on the finding row. The reducer never auto-opens pull requests; this block is strictly advisory.",
                      "properties": {
                        "ecosystem": {"type": "string", "description": "Ecosystem the remediation was computed for, emitted by explainImpact (packet.Remediation.Ecosystem)."},
                        "current_version": {"type": "string", "description": "Installed version emitted by explainImpact (packet.Remediation.CurrentVersion)."},
                        "vulnerable_range": {"type": "string", "description": "Source-reported vulnerable range emitted by explainImpact (packet.Remediation.VulnerableRange)."},
                        "fixed_version_source": {"type": "string", "description": "Source of the fixed version emitted by explainImpact (packet.Remediation.FixedVersionSource)."},
                        "match_reason": {"type": "string", "description": "Version/range match reason emitted by explainImpact (packet.Remediation.MatchReason)."},
                        "first_patched_version": {"type": "string", "description": "First patched version Eshu can defend, emitted by explainImpact (packet.Remediation.FirstPatchedVersion)."},
                        "patched_version_branches": {
                          "type": "array",
                          "description": "Every published fixed-version branch emitted by explainImpact (packet.FixedVersionBranch).",
                          "items": {
                            "type": "object", "description": "One patched version branch. emitted by explainImpact (packet.FixedVersionBranch).",
                            "properties": {
                              "version": {"type": "string", "description": "Fixed version emitted by explainImpact (packet.FixedVersionBranch.Version)."},
                              "source": {"type": "string", "description": "Advisory source of the branch emitted by explainImpact (packet.FixedVersionBranch.Source)."}
                            },
                            "required": ["version", "source"]
                          }
                        },
                        "manifest_range": {"type": "string", "description": "Manifest range preserved from package consumption evidence, emitted by explainImpact (packet.Remediation.ManifestRange)."},
                        "manifest_allows_fix": {"type": "string", "enum": ["allowed", "blocked", "unknown"], "description": "Whether the manifest range already admits the first patched version, emitted by explainImpact (packet.Remediation.ManifestAllowsFix)."},
                        "direct": {"type": "boolean", "description": "Direct (true) or transitive (false) designation emitted by explainImpact (packet.Remediation.Direct)."},
                        "parent_package": {"type": "string", "description": "Parent package the caller would upgrade for transitive findings, emitted by explainImpact (packet.Remediation.ParentPackage)."},
                        "confidence": {"type": "string", "enum": ["exact", "partial", "unknown"], "description": "Confidence label emitted by explainImpact (packet.Remediation.Confidence)."},
                        "reason": {"type": "string", "enum": ["direct_upgrade_allowed", "direct_range_blocked", "transitive_parent_upgrade_required", "already_fixed", "no_patched_version", "multiple_patched_branches", "package_manager_unsupported", "manifest_range_missing", "manifest_range_malformed", "installed_version_missing", "installed_version_malformed"], "description": "Machine reason code naming the recommended action, emitted by explainImpact (packet.Remediation.Reason)."},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Structured missing-evidence reasons emitted by explainImpact (packet.Remediation.MissingEvidence)."}
                      }
                    }
                  },
                  "required": ["outcome", "input", "advisory", "component", "version", "anchors", "evidence", "readiness", "freshness"]
                }
              }
            }
          },
          "400": {"description": "Unbounded input: finding_id, or advisory_id/cve_id plus package_id, repository_id, subject_digest, image_ref, workload_id, or service_id is required"}
        }
      }
    },
`
