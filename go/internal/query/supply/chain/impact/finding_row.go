// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// FindingRow is one durable impact finding decoded from
// reducer-owned facts.
type FindingRow struct {
	FindingID       string
	CVEID           string
	AdvisoryID      string
	PackageID       string
	Ecosystem       string
	PackageName     string
	PURL            string
	ProductCriteria string
	MatchCriteriaID string
	ObservedVersion string
	RequestedRange  string
	FixedVersion    string
	// VulnerableRange is the source-reported affected range expression
	// copied from the advisory the reducer's provenance selector picked.
	// Persisted on the canonical finding payload so list responses expose
	// the same value as the explain route. Older rows that predate the
	// reducer capturing the range may return blank.
	VulnerableRange       string
	MatchReason           string
	Status                string
	Confidence            string
	CVSSScore             float64
	AdvisoryPublishedAt   string
	AdvisoryUpdatedAt     string
	EPSSProbability       string
	EPSSPercentile        string
	KnownExploited        bool
	PriorityReason        string
	PriorityScore         int
	PriorityBucket        string
	PriorityReasonCodes   []string
	PriorityContributions []PriorityContribution
	// ExploitedTier is the additive #9882 item-4.5 exploited rank (kev,
	// ssvc_active, epss, none), decoded from the persisted finding payload.
	// Empty for rows written before the tier existed; the response omits it
	// then rather than inventing a rank. Kept in the same field position as
	// FindingResult so the FindingResult(row) conversion stays valid.
	ExploitedTier       string
	RuntimeReachability string
	Reachability        *ReachabilityResult
	RepositoryID        string
	SubjectDigest       string
	ImageRef            string
	DependencyScope     string
	WorkloadIDs         []string
	DeploymentIDs       []string
	ServiceIDs          []string
	Environments        []string
	// EnvironmentEvidence records, per environment name in Environments,
	// whether the strongest deployment evidence for that environment was
	// "deploy_event" (a ci.deployment_event observed at the deploying run's
	// commit, #5425) or "declared" (the CI-declared workflow job gate
	// alone). Older rows written before #5426 landed decode this as a nil
	// map, never as a fabricated value, and the response omits the field
	// entirely for them. Kept in the same field
	// position as FindingResult so the
	// FindingResult(row) conversion stays valid.
	EnvironmentEvidence map[string]string
	// CIDeclaredArtifactDigest and CIDeclaredImageRef carry the matched
	// cicd_run_correlation deployment's OWN declared artifact identity
	// (issue #5469), decoded from the persisted finding payload. The reducer
	// bakes these only when that deployment matched through a strong
	// artifact-identity branch (digest or image-ref equality), never the
	// weak repository+environment+operational-anchor branch -- so a
	// non-empty value here is a genuine CI-declared version/digest claim,
	// not one inferred from the finding's own SubjectDigest/ImageRef. Empty
	// for older rows written before #5469 landed, and for any row whose only
	// CI/CD evidence matched through the weak branch. Kept in the same field
	// position as FindingResult so the
	// FindingResult(row) conversion stays valid.
	CIDeclaredArtifactDigest string
	CIDeclaredImageRef       string
	// CloudRuntimeResourceRefs names the observed cloud compute resources
	// (running ECS task / image-package Lambda ARNs) whose running image digest
	// matches this finding's subject digest — runtime-observed deployment
	// evidence distinct from the CI-declared deployment anchors (#5452). It is
	// NOT decoded from the persisted finding payload: the findings handler
	// populates it at read time via applySupplyChainCloudRuntimeEvidence (a
	// bounded CloudResource graph probe), mirroring how trace_deployment_chain
	// derives its runtime_confirmed tier from live graph evidence. Kept in the
	// same field position as FindingResult so the
	// FindingResult(row) conversion stays valid.
	CloudRuntimeResourceRefs []string
	// KubernetesRuntimeWorkloadRefs names current, authorized workloads observed
	// running this finding's exact subject digest. It is query-time evidence and
	// is intentionally distinct from repository-level RuntimeContext.WorkloadIDs.
	KubernetesRuntimeWorkloadRefs []KubernetesRuntimeWorkloadRef
	// KubernetesRuntimeProbe reports the digest-local candidate budget and only
	// discloses truncation when the all-scopes authorization contract permits it.
	KubernetesRuntimeProbe *KubernetesRuntimeProbeMetadata
	// RuntimeContext carries the read-time-resolved runtime context
	// (workloads, services, deployments, environments, catalog refs) resolved
	// from this finding's repository_id at query time (issue #5746). Like
	// CloudRuntimeResourceRefs it is NOT decoded from the persisted payload:
	// the findings handler populates it at read time via
	// applySupplyChainRuntimeContext. Kept in the same field position as
	// FindingResult so the
	// FindingResult(row) conversion stays valid.
	RuntimeContext    *RuntimeContextResult
	CatalogEntityRefs []string
	CatalogOwnerRefs  []string
	DependencyPath    []string
	DependencyDepth   int
	DirectDependency  *bool
	MissingEvidence   []string
	EvidencePath      []string
	EvidenceFactIDs   []string
	SourceFreshness   string
	SourceConfidence  string
	Provenance        *Provenance
	// Suppression carries the reducer VEX/operator-policy decision; it is
	// always populated (state=active when no suppression matched) so callers
	// can audit suppression provenance even when the finding is hidden from
	// the default view.
	Suppression *SuppressionDecisionRow
	// DetectionProfile records which evidence tier produced the row:
	// precise for exact installed-version anchors, comprehensive for
	// range-only, SBOM-derived, CPE-derived, malformed, or missing-version
	// evidence. Unsupported matcher ecosystems are readiness gaps, not
	// findings.
	// Older rows written before profile tagging may return blank.
	DetectionProfile string
	// Remediation is the reducer-owned advisory-only safe-upgrade
	// recommendation for this finding. Older rows written before #595
	// landed will leave this nil; callers must treat that as "no
	// remediation computed" rather than "no fix available".
	Remediation *Remediation
	// DeploymentTruthTier classifies the strongest deployment evidence
	// available for this finding's repository or workload context (#5471).
	DeploymentTruthTier string
	// VersionResolutionTier and VersionResolutionCorroboration are NOT
	// decoded from the persisted finding payload. They are computed at read
	// time by supplyChainVersionResolution (#5469) from this row's own
	// fields, mirroring how DeploymentTruthTier is derived rather than
	// stored. Kept in the same field position as
	// FindingResult so the
	// FindingResult(row) conversion stays valid.
	VersionResolutionTier          string
	VersionResolutionCorroboration []VersionResolutionCorroboration
}
