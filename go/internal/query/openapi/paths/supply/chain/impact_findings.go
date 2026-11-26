// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

const impactFindings = `
    "/api/v0/supply-chain/impact/findings": {
      "get": {
        "summary": "List supply-chain impact findings",
        "description": "Lists reducer-owned supply-chain impact finding rows with readiness coverage. At least one anchor (cve_id, advisory_id, package_id, repository_id, subject_digest, image_ref, impact_status, ecosystem, workload_id, service_id, environment, severity, priority_bucket, or min_priority_score above 0) is required; a request with none returns 400.",
        "operationId": "listSupplyChainImpactFindings",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "description": "Exact source advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "description": "GHSA advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "osv_id", "in": "query", "description": "OSV advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
            {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty page.", "schema": {"type": "string"}},
            {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
            {"name": "image_ref", "in": "query", "schema": {"type": "string"}, "description": "Exact image reference stored on reducer-owned impact findings."},
            {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}, "description": "Package ecosystem from reducer-owned impact facts."},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted workload anchor; missing runtime mapping remains missing evidence."},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted service anchor derived from workload/service evidence."},
          {"name": "environment", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted environment anchor; the read layer does not infer aliases."},
          {"name": "severity", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "none"]}, "description": "CVSS-derived severity bucket."},
          {"name": "profile", "in": "query", "schema": {"type": "string", "enum": ["precise", "comprehensive"], "default": "precise"}, "description": "Detection profile selector. precise (default) returns only findings backed by exact installed-version anchors resolved by supported matchers such as npm, PyPI, Maven, Cargo, Pub, NuGet, Swift, or vendor-backed RPM, Debian/dpkg, and Alpine/APK OS packages. comprehensive also returns range-only manifest, OS-package range, SBOM/CPE-derived, malformed range, and missing-version rows. Unsupported non-OS package ecosystems are reported as readiness unsupported_targets, not finding rows. Each finding row keeps its truth labels (impact_status, confidence, runtime_reachability) and missing-evidence reasons."},
          {"name": "priority_bucket", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"]}, "description": "Reducer triage priority filter; does not change impact truth."},
          {"name": "min_priority_score", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Minimum reducer priority score. Zero is the default no-op value and does not bound a request by itself."},
          {"name": "sort", "in": "query", "schema": {"type": "string", "enum": ["finding_id", "priority", "priority_score_desc", "priority_score_asc"]}},
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Hidden states (not_affected, accepted_risk, false_positive, ignored, advisory_retracted) require include_suppressed=true to be returned.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch", "advisory_retracted"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by an operator-asserted suppression or by a source retraction of the advisory (advisory_retracted). Expired, provider-dismissed, and scope-mismatched findings stay visible regardless because they preserve operator audit signal.", "schema": {"type": "boolean", "default": false}},
          {"name": "after_finding_id", "in": "query", "schema": {"type": "string"}},
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
            "description": "Supply-chain impact finding page with readiness coverage",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "findings": {
                      "type": "array",
                      "description": "Reducer-owned impact finding rows emitted by listImpactFindings (packet.FindingResult via impact.BuildFindingResult).",
                      "items": {
                        "type": "object", "description": "One reducer-owned impact finding row. emitted by listImpactFindings (packet.FindingResult via impact.BuildFindingResult).",
                        "properties": {
                          "finding_id": {"type": "string", "description": "Reducer-owned finding id emitted by listImpactFindings (packet.FindingResult.FindingID)."},
                          "cve_id": {"type": "string", "description": "CVE id emitted by listImpactFindings (packet.FindingResult.CVEID)."},
                          "advisory_id": {"type": "string", "description": "Source advisory id emitted by listImpactFindings (packet.FindingResult.AdvisoryID)."},
                          "package_id": {"type": "string", "description": "Affected package id emitted by listImpactFindings (packet.FindingResult.PackageID)."},
                          "ecosystem": {"type": "string", "description": "Package ecosystem emitted by listImpactFindings (packet.FindingResult.Ecosystem)."},
                          "package_name": {"type": "string", "description": "Package name emitted by listImpactFindings (packet.FindingResult.PackageName)."},
                          "purl": {"type": "string", "description": "Package URL emitted by listImpactFindings (packet.FindingResult.PURL)."},
                          "product_criteria": {"type": "string", "description": "NVD product criteria emitted by listImpactFindings (packet.FindingResult.ProductCriteria)."},
                          "match_criteria_id": {"type": "string", "description": "NVD match criteria id emitted by listImpactFindings (packet.FindingResult.MatchCriteriaID)."},
                          "observed_version": {"type": "string", "description": "Exact installed version from lockfile, manifest, SBOM, or image evidence when known."},
                          "requested_range": {"type": "string", "description": "Original manifest/requested dependency range preserved separately from the installed version."},
                          "fixed_version": {"type": "string", "description": "Source-selected fixed version when advisory evidence reports one."},
                          "vulnerable_range": {"type": "string", "description": "Source-reported affected range expression copied from the advisory the reducer's provenance selector picked. Persisted on the canonical finding payload so list responses expose the same expression as the explain route. Older rows may omit this value."},
                          "match_reason": {"type": "string", "description": "Reducer reason for the version/range decision, including supported matches, range-only evidence, malformed evidence states, or missing version evidence. Unsupported non-OS package ecosystems are surfaced in readiness.unsupported_targets rather than finding rows."},
                          "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Evidence tier the row meets. precise requires an exact installed-version anchor and ecosystem-aware match, including supported npm, PyPI, Maven, Cargo, Pub, NuGet, Swift, and vendor-backed RPM, Debian/dpkg, and Alpine/APK paths. comprehensive covers SBOM/CPE-derived, range-only manifest or OS-package range, malformed, or missing-version rows that still have an owned anchor."},
                          "impact_status": {"type": "string", "description": "Reducer impact verdict emitted by listImpactFindings (packet.FindingResult.Status)."},
                          "confidence": {"type": "string", "description": "Reducer match confidence emitted by listImpactFindings (packet.FindingResult.Confidence)."},
                          "cvss_score": {"type": "number", "description": "Selected CVSS score emitted by listImpactFindings (packet.FindingResult.CVSSScore)."},
                          "advisory_published_at": {"type": "string", "description": "Advisory publication time emitted by listImpactFindings (packet.FindingResult.AdvisoryPublishedAt)."},
                          "advisory_updated_at": {"type": "string", "description": "Advisory update time emitted by listImpactFindings (packet.FindingResult.AdvisoryUpdatedAt)."},
                          "epss_probability": {"type": "string", "description": "Selected EPSS probability emitted by listImpactFindings (packet.FindingResult.EPSSProbability)."},
                          "epss_percentile": {"type": "string", "description": "Selected EPSS percentile emitted by listImpactFindings (packet.FindingResult.EPSSPercentile)."},
                          "known_exploited": {"type": "boolean", "description": "Whether the advisory is known-exploited, emitted by listImpactFindings (packet.FindingResult.KnownExploited)."},
                          "priority_reason": {"type": "string", "description": "Reducer priority reason emitted by listImpactFindings (packet.FindingResult.PriorityReason)."},
                          "priority_score": {"type": "integer", "minimum": 0, "maximum": 100, "description": "Reducer priority score emitted by listImpactFindings (packet.FindingResult.PriorityScore); triage metadata only."},
                          "priority_bucket": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"], "description": "Reducer triage priority bucket emitted by listImpactFindings (packet.FindingResult.PriorityBucket)."},
                          "priority_reason_codes": {"type": "array", "items": {"type": "string"}, "description": "Reducer priority reason codes emitted by listImpactFindings (packet.FindingResult.PriorityReasonCodes)."},
                          "priority_contributions": {"type": "array", "description": "Explainable scoring inputs emitted by listImpactFindings (packet.PriorityContribution). Priority is triage metadata and never proves affected truth.", "items": {"type": "object", "description": "One explainable scoring input. emitted by listImpactFindings (packet.PriorityContribution).", "properties": {"reason_code": {"type": "string", "description": "Contribution reason code emitted by listImpactFindings (packet.PriorityContribution.ReasonCode)."}, "input": {"type": "string", "description": "Scoring input emitted by listImpactFindings (packet.PriorityContribution.Input)."}, "value": {"type": "string", "description": "Scoring input value emitted by listImpactFindings (packet.PriorityContribution.Value)."}, "contribution": {"type": "integer", "description": "Score contribution emitted by listImpactFindings (packet.PriorityContribution.Contribution)."}}, "required": ["reason_code", "input", "contribution"]}},
                          "exploited_tier": {"type": "string", "enum": ["kev", "ssvc_active", "epss", "none"], "description": "Additive exploited rank for #9882 item 4.5 emitted by listImpactFindings (packet.FindingResult.ExploitedTier): kev for CISA KEV, ssvc_active for an SSVC active exploitation state (reserved until 4.1 feeds it), epss for an EPSS observation without an exploited listing, none otherwise. Triage metadata alongside priority_score; never proves affected truth. Omitted for rows written before the tier existed."},
                          "runtime_reachability": {"type": "string", "description": "Runtime reachability label emitted by listImpactFindings (packet.FindingResult.RuntimeReachability)."},
                          "reachability": {"type": "object", "description": "Cross-language reachability enrichment emitted by listImpactFindings (packet.ReachabilityResult). This is prioritization metadata and never downgrades or hides impact findings. missing_evidence and unavailable are not clean results.", "properties": {"state": {"type": "string", "enum": ["reachable", "not_called", "unknown", "unavailable", "missing_evidence"], "description": "Reachability verdict emitted by listImpactFindings (packet.ReachabilityResult.State)."}, "confidence": {"type": "string", "description": "Reachability confidence emitted by listImpactFindings (packet.ReachabilityResult.Confidence)."}, "source": {"type": "string", "description": "Reachability source emitted by listImpactFindings (packet.ReachabilityResult.Source)."}, "evidence": {"type": "string", "description": "Reachability evidence emitted by listImpactFindings (packet.ReachabilityResult.Evidence)."}, "reason": {"type": "string", "description": "Reachability reason emitted by listImpactFindings (packet.ReachabilityResult.Reason)."}, "language_maturity": {"type": "string", "enum": ["implemented", "partial", "unavailable", "unsupported"], "description": "Scanner language maturity emitted by listImpactFindings (packet.ReachabilityResult.LanguageMaturity)."}, "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Reachability evidence gaps emitted by listImpactFindings (packet.ReachabilityResult.MissingEvidence)."}, "scan_level": {"type": "string", "description": "Govulncheck scan depth behind a Go verdict (module, package, or symbol) emitted by listImpactFindings (packet.ReachabilityResult.ScanLevel). Present only when the scan reported it."}, "scan_mode": {"type": "string", "description": "How the scanned code was read (source or binary) emitted by listImpactFindings (packet.ReachabilityResult.ScanMode). Present only when the scan reported it."}, "scanner_version": {"type": "string", "description": "Govulncheck release that produced a Go verdict emitted by listImpactFindings (packet.ReachabilityResult.ScannerVersion). Present only when the scan reported it."}, "trace_count": {"type": "integer", "description": "Call traces the scanner found for the advisory and module emitted by listImpactFindings (packet.ReachabilityResult.TraceCount). Present only when the scan reported it."}}, "required": ["state"]},
                          "repository_id": {"type": "string", "description": "Repository the finding was reduced from, emitted by listImpactFindings (packet.FindingResult.RepositoryID)."},
                          "subject_digest": {"type": "string", "description": "Image subject digest the finding applies to, emitted by listImpactFindings (packet.FindingResult.SubjectDigest)."},
                          "image_ref": {"type": "string", "description": "Image reference the finding applies to, emitted by listImpactFindings (packet.FindingResult.ImageRef)."},
                          "dependency_scope": {"type": "string", "description": "Dependency scope emitted by listImpactFindings (packet.FindingResult.DependencyScope)."},
                          "workload_ids": {"type": "array", "items": {"type": "string"}, "description": "Baked workload ids emitted by listImpactFindings (packet.FindingResult.WorkloadIDs); stale values cannot satisfy the workload filter."},
                          "deployment_ids": {"type": "array", "items": {"type": "string"}, "description": "Deployment mapping anchors from repository-scoped reducer_platform_materialization evidence. These prove a repository has deployment-lane evidence but do not imply workload identity, environment, or runtime image proof."},
                          "service_ids": {"type": "array", "items": {"type": "string"}, "description": "Baked service ids emitted by listImpactFindings (packet.FindingResult.ServiceIDs); stale values cannot satisfy the service filter."},
                          "environments": {"type": "array", "items": {"type": "string"}, "description": "Baked environment names emitted by listImpactFindings (packet.FindingResult.Environments)."},
                          "environment_evidence": {"type": "object", "additionalProperties": {"type": "string", "enum": ["deploy_event", "declared"]}, "description": "Per-environment-name corroboration state for entries in environments[] (issue #5426). deploy_event means a ci.deployment_event observed at the deploying run's commit supplied that environment; declared means only the CI-declared workflow job gate did, with no deployment-event corroboration. deploy_event always wins a collision from multiple deployments for the same environment name. The two-value vocabulary is a producer guarantee: the reducer normalizes every value it writes, mapping anything it does not recognize to declared rather than inventing corroboration. Omitted entirely when a finding has no recorded environment evidence, including rows written before #5426 landed, rather than reporting a fabricated value. A declared-only deployment still contributes its environment and its cicd_run_correlation evidence hop; only promotion to runtime_reachability=deployed_image is withheld."},
                          "cloud_runtime_resource_refs": {"type": "array", "items": {"type": "string"}, "description": "ARNs of observed cloud compute resources (running ECS task / image-package Lambda) whose running image digest equals this finding's subject_digest — runtime-observed deployment evidence that drives deployment_truth_tier=runtime_confirmed, distinct from CI-declared correlation. Query-time enrichment (#5452); present only when a live cloud resource runs the affected image."},
                          "kubernetes_runtime_workload_refs": {"type": "array", "description": "Current, caller-authorized Kubernetes workloads observed through an exact digest RUNS_IMAGE edge whose source_digest equals this finding's subject_digest, emitted by listImpactFindings (packet.KubernetesRuntimeWorkloadRef). Each workload owner winner and edge scope/generation is independently current and authorized. Query-time enrichment (#5834); present only when exact live evidence drives deployment_truth_tier=runtime_confirmed. Unlike repository-level runtime_context.workload_ids, these refs are bound to this finding's digest.", "items": {"type": "object", "description": "One digest-bound Kubernetes workload ref. emitted by listImpactFindings (packet.KubernetesRuntimeWorkloadRef).", "properties": {"workload_uid": {"type": "string", "description": "Workload uid emitted by listImpactFindings (packet.KubernetesRuntimeWorkloadRef.UID)."}, "cluster_id": {"type": "string", "description": "Cluster id emitted by listImpactFindings (packet.KubernetesRuntimeWorkloadRef.ClusterID)."}, "namespace": {"type": "string", "description": "Workload namespace emitted by listImpactFindings (packet.KubernetesRuntimeWorkloadRef.Namespace)."}, "name": {"type": "string", "description": "Workload name emitted by listImpactFindings (packet.KubernetesRuntimeWorkloadRef.Name)."}}, "required": ["workload_uid"]}},
                          "kubernetes_runtime_probe": {"type": "object", "description": "Bounded per-digest Kubernetes RUNS_IMAGE probe metadata emitted by listImpactFindings (packet.KubernetesRuntimeProbeMetadata). candidate_limit is this finding digest's page-weighted authorized-ref budget: every non-empty digest occurrence receives at least one slot from the fixed 200-reference serialized-page cap, and repeated findings share the budget deterministically. workload_refs_truncated is null for scoped callers; for all-scopes callers true means authorized current refs exceeded that budget, false means the raw graph query exhausted within the budget, and null means the graph candidate sentinel was reached but authorization did not prove an authorized overflow.", "properties": {"candidate_limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Digest-local candidate budget emitted by listImpactFindings (packet.KubernetesRuntimeProbeMetadata.CandidateLimit)."}, "workload_refs_truncated": {"type": "boolean", "nullable": true, "description": "Whether authorized refs exceeded the budget, emitted by listImpactFindings (packet.KubernetesRuntimeProbeMetadata.WorkloadRefsTruncated); null when authorization hides the answer."}}, "required": ["candidate_limit", "workload_refs_truncated"]},
                          "ci_declared_artifact_digest": {"type": "string", "description": "The matched cicd_run_correlation deployment's OWN declared artifact digest (issue #5469), baked by the reducer only when that deployment matched through a STRONG identity branch (its own artifact_digest equalled subject_digest, or its own image_ref equalled image_ref) rather than the weak repository+environment+operational-anchor branch (#5426). This can genuinely differ from subject_digest: a deployment can match via image reference while its own declared digest contradicts the finding's subject digest (a moved tag), and that disagreement is exactly what version_resolution_corroboration discloses. A contradicting value here is never eligible to become version_resolution_tier's winner (review finding R1) -- it is disclosed as corroboration only. Never a copy of subject_digest — omitted, not borrowed, when no strong-branch match exists."},
                          "ci_declared_image_ref": {"type": "string", "description": "The matched cicd_run_correlation deployment's OWN declared image reference (issue #5469), baked under the same strong-branch-only rule as ci_declared_artifact_digest. Omitted when no strong-branch match exists."},
                          "runtime_context": ` + RuntimeContext + `,
                          "catalog_entity_refs": {"type": "array", "items": {"type": "string"}, "description": "Reducer-admitted service-catalog entity references attached to the finding evidence path. These are catalog anchors and do not become service_ids unless the reducer fact carries an explicit service_id."},
                          "catalog_owner_refs": {"type": "array", "items": {"type": "string"}, "description": "Reducer-admitted service-catalog owners attached to the finding evidence path. These preserve ownership context without inventing service or workload identity."},
                          "dependency_path": {"type": "array", "items": {"type": "string"}, "description": "Dependency path emitted by listImpactFindings (packet.FindingResult.DependencyPath)."},
                          "dependency_depth": {"type": "integer", "description": "Dependency depth emitted by listImpactFindings (packet.FindingResult.DependencyDepth)."},
                          "direct_dependency": {"type": "boolean", "description": "Whether the finding is a direct dependency, emitted by listImpactFindings (packet.FindingResult.DirectDependency)."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence gaps emitted by listImpactFindings (packet.FindingResult.MissingEvidence)."},
                          "evidence_path": {"type": "array", "items": {"type": "string"}, "description": "Evidence path hops emitted by listImpactFindings (packet.FindingResult.EvidencePath)."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids emitted by listImpactFindings (packet.FindingResult.EvidenceFactIDs)."},
                          "source_freshness": {"type": "string", "description": "Source freshness signal emitted by listImpactFindings (packet.FindingResult.SourceFreshness)."},
                          "source_confidence": {"type": "string", "description": "Source confidence signal emitted by listImpactFindings (packet.FindingResult.SourceConfidence)."},
                          "suppression": {
                            "type": "object",
                            "description": "Reducer VEX/operator-policy suppression decision for this finding, emitted by listImpactFindings (packet.SuppressionDecisionRow). Always populated; state=active means no suppression matched. Operator suppressions (not_affected, accepted_risk, false_positive, ignored) and a source retraction of the whole advisory (advisory_retracted, with the reason and any replacement in the finding's provenance.advisory_retraction block) hide the finding from the default view; expired, provider_dismissed, and scope_mismatch states keep the finding visible while preserving suppression provenance.",
                            "properties": {
                              "state": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch", "advisory_retracted"], "description": "Suppression state emitted by listImpactFindings (packet.SuppressionDecisionRow.State)."},
                              "suppression_id": {"type": "string", "description": "Suppression id emitted by listImpactFindings (packet.SuppressionDecisionRow.SuppressionID)."},
                              "source": {"type": "string", "enum": ["vex_statement", "eshu_policy", "provider_dismissal", "advisory_retraction"], "description": "Suppression source emitted by listImpactFindings (packet.SuppressionDecisionRow.Source)."},
                              "justification": {"type": "string", "enum": ["not_affected", "accepted_risk", "false_positive", "ignored", "provider_dismissed"], "description": "Operator justification emitted by listImpactFindings (packet.SuppressionDecisionRow.Justification)."},
                              "author": {"type": "string", "description": "Suppression author emitted by listImpactFindings (packet.SuppressionDecisionRow.Author)."},
                              "authored_at": {"type": "string", "description": "Suppression creation time emitted by listImpactFindings (packet.SuppressionDecisionRow.AuthoredAt)."},
                              "expires_at": {"type": "string", "description": "Suppression expiry emitted by listImpactFindings (packet.SuppressionDecisionRow.ExpiresAt)."},
                              "reason": {"type": "string", "description": "Suppression reason emitted by listImpactFindings (packet.SuppressionDecisionRow.Reason)."},
                              "evidence_ref": {"type": "string", "description": "Pointer to the originating evidence (fact ID, VEX URL, provider alert) emitted by listImpactFindings (packet.SuppressionDecisionRow.EvidenceRef)."},
                              "vex_document_id": {"type": "string", "description": "VEX document id emitted by listImpactFindings (packet.SuppressionDecisionRow.VEXDocumentID)."},
                              "vex_statement_id": {"type": "string", "description": "VEX statement id emitted by listImpactFindings (packet.SuppressionDecisionRow.VEXStatementID)."},
                              "matched_id": {"type": "string", "description": "Finding-side identity the winning suppression named: an advisory-group member id, or the finding's own cve_id/advisory_id."},
                              "matched_field": {"type": "string", "enum": ["cve_id", "advisory_id"], "description": "Suppression scope field that supplied matched_id."},
                              "matched_via": {"type": "string", "enum": ["direct", "group_member"], "description": "direct when the scope named the finding's own ids; group_member when it named a non-canonical alias-group member."}
                            },
                            "required": ["state"]
                          },
                          "remediation": {
                            "type": "object",
                            "description": "Advisory-only safe-upgrade recommendation for this finding (issue #595) emitted by listImpactFindings (packet.Remediation). The reducer never auto-opens pull requests. confidence is one of exact, partial, or unknown; reason is a closed enum that names the recommended action; manifest_allows_fix labels whether the manifest range already admits the first patched version. Older rows that predate remediation computation omit this block.",
                            "properties": {
                              "ecosystem": {"type": "string", "description": "Ecosystem the remediation was computed for, emitted by listImpactFindings (packet.Remediation.Ecosystem)."},
                              "current_version": {"type": "string", "description": "Installed version emitted by listImpactFindings (packet.Remediation.CurrentVersion)."},
                              "vulnerable_range": {"type": "string", "description": "Source-reported vulnerable range emitted by listImpactFindings (packet.Remediation.VulnerableRange)."},
                              "fixed_version_source": {"type": "string", "description": "Source of the fixed version emitted by listImpactFindings (packet.Remediation.FixedVersionSource)."},
                              "match_reason": {"type": "string", "description": "Version/range match reason emitted by listImpactFindings (packet.Remediation.MatchReason)."},
                              "first_patched_version": {"type": "string", "description": "First patched version Eshu can defend, emitted by listImpactFindings (packet.Remediation.FirstPatchedVersion)."},
                              "patched_version_branches": {
                                "type": "array",
                                "description": "Every published fixed-version branch emitted by listImpactFindings (packet.FixedVersionBranch).",
                                "items": {
                                  "type": "object", "description": "One patched version branch. emitted by listImpactFindings (packet.FixedVersionBranch).",
                                  "properties": {
                                    "version": {"type": "string", "description": "Fixed version emitted by listImpactFindings (packet.FixedVersionBranch.Version)."},
                                    "source": {"type": "string", "description": "Advisory source of the branch emitted by listImpactFindings (packet.FixedVersionBranch.Source)."}
                                  },
                                  "required": ["version", "source"]
                                }
                              },
                              "manifest_range": {"type": "string", "description": "Manifest range preserved from package consumption evidence, emitted by listImpactFindings (packet.Remediation.ManifestRange)."},
                              "manifest_allows_fix": {"type": "string", "enum": ["allowed", "blocked", "unknown"], "description": "Whether the manifest range already admits the first patched version, emitted by listImpactFindings (packet.Remediation.ManifestAllowsFix)."},
                              "direct": {"type": "boolean", "description": "Direct (true) or transitive (false) designation emitted by listImpactFindings (packet.Remediation.Direct)."},
                              "parent_package": {"type": "string", "description": "Parent package the caller would upgrade for transitive findings, emitted by listImpactFindings (packet.Remediation.ParentPackage)."},
                              "confidence": {"type": "string", "enum": ["exact", "partial", "unknown"], "description": "Confidence label emitted by listImpactFindings (packet.Remediation.Confidence)."},
                              "reason": {"type": "string", "enum": ["direct_upgrade_allowed", "direct_range_blocked", "transitive_parent_upgrade_required", "already_fixed", "no_patched_version", "multiple_patched_branches", "package_manager_unsupported", "manifest_range_missing", "manifest_range_malformed", "installed_version_missing", "installed_version_malformed"], "description": "Machine reason code naming the recommended action, emitted by listImpactFindings (packet.Remediation.Reason)."},
                              "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Structured missing-evidence reasons emitted by listImpactFindings (packet.Remediation.MissingEvidence)."}
                            }
                          },
                          "provenance": {
                            "type": "object",
                            "description": "Selected EPSS observation and per-source advisory provenance, emitted by listImpactFindings (packet.Provenance). Reducers preserve advisory source observations behind a finding so callers see which source supplied severity, fixed version, and vulnerable range plus alternates. Advisory selection uses documented per-ecosystem priority (vendor advisory for OS package classes, GHSA/GLAD/OSV/NVD for language ecosystems).",
                            "properties": {
                              "selected_severity_source": {"type": "string", "description": "Advisory source whose severity was selected, emitted by listImpactFindings (packet.Provenance.SelectedSeveritySource)."},
                              "selected_severity_score": {"type": "number", "description": "Selected severity score emitted by listImpactFindings (packet.Provenance.SelectedSeverityScore)."},
                              "selected_severity_score_source": {"type": "string", "enum": ["derived_from_vector"], "description": "Present and set to derived_from_vector when the reducer computed selected_severity_score from selected_severity_vector because the source published no score, emitted by listImpactFindings (packet.Provenance.SelectedSeverityScoreSource). Absent means the source reported the score."},
                              "selected_severity_vector": {"type": "string", "description": "Selected severity vector emitted by listImpactFindings (packet.Provenance.SelectedSeverityVector)."},
                              "selected_severity_label": {"type": "string", "description": "Selected severity label emitted by listImpactFindings (packet.Provenance.SelectedSeverityLabel)."},
                              "selected_fixed_version_source": {"type": "string", "description": "Advisory source whose fixed-version branch was selected, emitted by listImpactFindings (packet.Provenance.SelectedFixedVersionSource)."},
                              "selected_range_source": {"type": "string", "description": "Advisory source whose vulnerable-range expression was selected, emitted by listImpactFindings (packet.Provenance.SelectedRangeSource)."},
                              "selected_epss_score_date": {"type": "string", "format": "date", "description": "Score date of the selected EPSS observation, emitted by listImpactFindings (packet.Provenance.SelectedEPSSScoreDate). Omitted for undated legacy facts."},
                              "selected_epss_model_version": {"type": "string", "description": "Model version supplied by the selected EPSS observation, when available, emitted by listImpactFindings (packet.Provenance.SelectedEPSSModelVersion). The FIRST API does not supply this field."},
                              "alternate_severities": {
                                "type": "array",
                                "description": "Severities reported by other sources that were not selected, emitted by listImpactFindings (packet.AlternateSeverity).",
                                "items": {
                                  "type": "object", "description": "One alternate severity row. emitted by listImpactFindings (packet.AlternateSeverity).",
                                  "properties": {
                                    "source": {"type": "string", "description": "Non-selected source emitted by listImpactFindings (packet.AlternateSeverity.Source)."},
                                    "score": {"type": "number", "description": "Non-selected score emitted by listImpactFindings (packet.AlternateSeverity.Score)."},
                                    "score_source": {"type": "string", "enum": ["derived_from_vector"], "description": "Present when score was computed from vector rather than reported by the source, emitted by listImpactFindings (packet.AlternateSeverity.ScoreSource)."},
                                    "vector": {"type": "string", "description": "Non-selected vector emitted by listImpactFindings (packet.AlternateSeverity.Vector)."},
                                    "label": {"type": "string", "description": "Non-selected label emitted by listImpactFindings (packet.AlternateSeverity.Label)."}
                                  },
                                  "required": ["source"]
                                }
                              },
                              "fixed_version_branches": {
                                "type": "array",
                                "description": "Every source-reported fixed-version branch, with the originating advisory source preserved, emitted by listImpactFindings (packet.FixedVersionBranch).",
                                "items": {
                                  "type": "object", "description": "One fixed version branch. emitted by listImpactFindings (packet.FixedVersionBranch).",
                                  "properties": {
                                    "version": {"type": "string", "description": "Fixed version emitted by listImpactFindings (packet.FixedVersionBranch.Version)."},
                                    "source": {"type": "string", "description": "Advisory source of the branch emitted by listImpactFindings (packet.FixedVersionBranch.Source)."}
                                  },
                                  "required": ["version", "source"]
                                }
                              },
                              "advisory_sources": {
                                "type": "array",
                                "description": "Every advisory source observation, including source-reported update timestamp, withdrawal timestamp, lifecycle status, and any stated replacement, emitted by listImpactFindings (packet.AdvisorySource).",
                                "items": {
                                  "type": "object", "description": "One advisory source row. emitted by listImpactFindings (packet.AdvisorySource).",
                                  "properties": {
                                    "source": {"type": "string", "description": "Advisory source emitted by listImpactFindings (packet.AdvisorySource.Source)."},
                                    "advisory_id": {"type": "string", "description": "Source-native advisory id emitted by listImpactFindings (packet.AdvisorySource.AdvisoryID)."},
                                    "source_updated_at": {"type": "string", "description": "Source update time emitted by listImpactFindings (packet.AdvisorySource.SourceUpdatedAt)."},
                                    "withdrawn_at": {"type": "string", "description": "Source withdrawal time emitted by listImpactFindings (packet.AdvisorySource.WithdrawnAt)."},
                                    "status": {"type": "string", "description": "Source-reported lifecycle status, verbatim (for example Rejected or withdrawn), emitted by listImpactFindings (packet.AdvisorySource.Status)."},
                                    "replaced_by": {"type": "array", "items": {"type": "string"}, "description": "CVE ids the source states replace this advisory, emitted by listImpactFindings (packet.AdvisorySource.ReplacedBy). Present only when the source states a replacement."}
                                  },
                                  "required": ["source"]
                                }
                              },
                              "advisory_retraction": {
                                "type": "object",
                                "description": "Present only when state=advisory_retracted: every source record of the CVE was rejected or withdrawn, emitted by listImpactFindings (packet.AdvisoryRetraction). Names why the finding is hidden and any replacement a source stated.",
                                "properties": {
                                  "reason_code": {"type": "string", "enum": ["rejected", "withdrawn", "rejected_and_withdrawn"], "description": "Retraction reason emitted by listImpactFindings (packet.AdvisoryRetraction.ReasonCode)."},
                                  "reason": {"type": "string", "description": "Retraction reason text emitted by listImpactFindings (packet.AdvisoryRetraction.Reason)."},
                                  "replaced_by": {"type": "array", "items": {"type": "string"}, "description": "Sorted union of the replacement CVE ids the retracting sources stated, emitted by listImpactFindings (packet.AdvisoryRetraction.ReplacedBy). Never inferred."},
                                  "sources": {
                                    "type": "array",
                                    "description": "Source records that retracted the advisory, emitted by listImpactFindings (packet.RetractionSource).",
                                    "items": {
                                      "type": "object", "description": "One advisory source evidence row. emitted by listImpactFindings (packet.RetractionSource).",
                                      "properties": {
                                        "source": {"type": "string", "description": "Retracting source emitted by listImpactFindings (packet.RetractionSource.Source)."},
                                        "advisory_id": {"type": "string", "description": "Retracted advisory id emitted by listImpactFindings (packet.RetractionSource.AdvisoryID)."},
                                        "reason_code": {"type": "string", "enum": ["rejected", "withdrawn"], "description": "Retraction reason emitted by listImpactFindings (packet.RetractionSource.ReasonCode)."},
                                        "status": {"type": "string", "description": "Source-reported status emitted by listImpactFindings (packet.RetractionSource.Status)."},
                                        "withdrawn_at": {"type": "string", "description": "Withdrawal time emitted by listImpactFindings (packet.RetractionSource.WithdrawnAt)."},
                                        "replaced_by": {"type": "array", "items": {"type": "string"}, "description": "Replacement CVE ids stated by this source, emitted by listImpactFindings (packet.RetractionSource.ReplacedBy)."}
                                      },
                                      "required": ["source", "reason_code"]
                                    }
                                  }
                                },
                                "required": ["reason_code", "reason"]
                              }
                            }
                          },
                          "deployment_truth_tier": {"type": "string", "description": "Strongest deployment evidence tier for this finding's repository or workload context, emitted by listImpactFindings (packet.FindingResult.DeploymentTruthTier); omitted when the finding has no deployment anchor."},
                          "version_resolution_tier": {"type": "string", "enum": ["runtime_confirmed", "provenance_ci_declared", "declared_ref", "config_only"], "description": "Which deployment-truth tier the judged version/digest for this finding (subject_digest/image_ref/observed_version) was resolved from (issue #5469), emitted by listImpactFindings (packet.FindingResult.VersionResolutionTier), reusing the truth.DeploymentTruthTier vocabulary verbatim -- the same tier strings as deployment_truth_tier. Can differ from deployment_truth_tier: deployment_truth_tier reports the strongest tier with ANY deployment evidence, while version_resolution_tier requires that tier's evidence to also carry a concrete, ELIGIBLE version/digest claim. provenance_ci_declared requires ci_declared_artifact_digest or ci_declared_image_ref to be non-empty (a strong-branch CI/CD match); a CI-declared hop that matched only via repository+environment+operational anchor (#5426's weak branch) bakes neither field, so it drops through to config_only here (or is omitted entirely if config_only itself has no eligible claim) even though it still holds deployment_truth_tier at provenance_ci_declared. A provenance_ci_declared claim whose ci_declared_artifact_digest CONTRADICTS subject_digest is never eligible to win either -- it still appears in version_resolution_corroboration, but resolution falls through to the next tier with an eligible claim about the finding's own identity (typically config_only). declared_ref never appears: #5393 (DEPLOYS_REF) has no evidence producer yet. Omitted when the finding has no eligible version/digest evidence at all."},
                          "version_resolution_corroboration": {
                            "type": "array",
                            "description": "Every deployment-truth tier other than the winner that ALSO makes a version/digest claim for this finding (issue #5469), emitted by listImpactFindings (packet.VersionResolutionCorroboration), including a tier that was ineligible to win (a CI-declared digest contradicting subject_digest, review finding R1) or whose claim disagrees with the winner -- disagreement is disclosed rather than the entry being dropped, so an operator can see two evidence sources disagree instead of only ever seeing confirmation. A provenance_ci_declared entry's digest_or_version is always the CI-declared deployment's OWN identity (ci_declared_artifact_digest/ci_declared_image_ref), never borrowed from subject_digest, so a genuine same-axis digest disagreement between runtime-observed/config-owned and CI-declared evidence is expressible here, not only a cross-axis digest-vs-version mismatch against config_only. Omitted when no other tier makes a claim.",
                            "items": {
                              "type": "object", "description": "One version-resolution corroboration entry. emitted by listImpactFindings (packet.VersionResolutionCorroboration).",
                              "properties": {
                                "tier": {"type": "string", "enum": ["runtime_confirmed", "provenance_ci_declared", "declared_ref", "config_only"], "description": "Claiming tier emitted by listImpactFindings (packet.VersionResolutionCorroboration.Tier)."},
                                "digest_or_version": {"type": "string", "description": "The digest or version this tier's own evidence asserts, emitted by listImpactFindings (packet.VersionResolutionCorroboration.DigestOrVersion)."},
                                "evidence_kind": {"type": "string", "enum": ["cloud_runtime_probe", "cicd_run_correlation", "config_materialization"], "description": "The evidence source backing this tier's claim, emitted by listImpactFindings (packet.VersionResolutionCorroboration.EvidenceKind)."},
                                "agreement": {"type": "string", "enum": ["agrees", "disagrees", "not_comparable"], "description": "Whether digest_or_version textually equals version_resolution_tier's judged value. Only ever agrees/disagrees when this entry's identity axis (digest, image_ref, or version) matches the winner's axis -- a cross-axis pair, such as a config-materialized version compared against a digest-based winner, is always not_comparable, since a dpkg version string (for example) can never meaningfully equal a sha256 digest (review finding R6)."}
                              },
                              "required": ["tier", "agreement"]
                            }
                          }
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listImpactFindings."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listImpactFindings."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listImpactFindings."},
                    "next_cursor": {"type": "object", "description": "Keyset cursor emitted by listImpactFindings only when truncated carries the last finding id forward; absent otherwise.", "properties": {
                      "after_finding_id": {"type": "string", "description": "Finding id of the last row, emitted by listImpactFindings for the next page cursor."}
                    }},
                    "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Echo of the detection profile the caller requested, emitted by listImpactFindings; precise is returned when no profile parameter was supplied."},
                    ` + supplyChainReadinessEnvelope + `
                  }
                }
              }
            }
          }
        }
      }
    },
`
