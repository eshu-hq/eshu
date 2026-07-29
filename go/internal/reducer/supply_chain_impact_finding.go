// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// SupplyChainImpactFinding is one reducer-owned vulnerability impact finding.
//
// Severity, fixed-version, and vulnerable-range fields carry per-source
// provenance so admission preserves which advisory source supplied each
// selected value and what alternates other sources reported. Reducers select
// one value per field using documented ecosystem-aware source priority.
type SupplyChainImpactFinding struct {
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
	// VulnerableRange is the source-reported affected range expression for
	// the advisory the provenance selector picked. The reducer persists the
	// expression on the canonical finding payload so list-route callers see
	// the same vulnerable range as the explain route without re-loading raw
	// advisory facts.
	VulnerableRange       string
	MatchReason           string
	Status                SupplyChainImpactStatus
	Confidence            string
	CVSSScore             float64
	SeveritySource        string
	SeverityVector        string
	SeverityLabel         string
	AdvisoryPublishedAt   string
	AdvisoryUpdatedAt     string
	AlternateSeverities   []AlternateSeverity
	FixedVersionSource    string
	FixedVersionBranches  []FixedVersionBranch
	RangeSource           string
	AdvisorySources       []AdvisorySourceObservation
	EPSSProbability       string
	EPSSPercentile        string
	KnownExploited        bool
	PriorityReason        string
	PriorityScore         int
	PriorityBucket        string
	PriorityReasonCodes   []string
	PriorityContributions []SupplyChainImpactPriorityContribution
	RuntimeReachability   string
	Reachability          *SupplyChainReachability
	RepositoryID          string
	SubjectDigest         string
	ImageRef              string
	DependencyScope       string
	WorkloadIDs           []string
	// ServiceWorkloadPairs is genuine (ServiceID, WorkloadID) co-occurrence
	// for tuple-aware suppression matching (#5466 F-2); see
	// supply_chain_impact_runtime.go and supply_chain_suppression_scope_match.go.
	ServiceWorkloadPairs []SupplyChainServiceWorkloadPair
	DeploymentIDs        []string
	ServiceIDs           []string
	Environments         []string
	// EnvironmentEvidence records, per environment name in Environments,
	// whether the strongest deployment evidence observed for that
	// environment was "deploy_event" (a ci.deployment_event observed at the
	// deploying run's commit, #5425) or "declared" (the CI-declared
	// workflow job gate alone, with no deployment-event corroboration).
	// deploy_event always wins a collision. Environments itself is
	// unchanged for wire compatibility; this is a sibling structure.
	EnvironmentEvidence map[string]string
	// CIDeclaredArtifactDigest and CIDeclaredImageRef store the complete
	// artifact identity from one strongly matched CI deployment. Weak
	// operational matches leave both fields blank; see
	// bakeSupplyChainCIDeclaredArtifactIdentity.
	CIDeclaredArtifactDigest string
	CIDeclaredImageRef       string
	CatalogEntityRefs        []string
	CatalogOwnerRefs         []string
	DependencyPath           []string
	DependencyDepth          int
	DirectDependency         *bool
	MissingEvidence          []string
	EvidencePath             []string
	EvidenceFactIDs          []string
	CanonicalWrites          int
	// DetectionProfile records which tier this finding meets: precise for
	// exact installed-version anchors, comprehensive for range-only,
	// SBOM-derived, product-derived, malformed, or missing-version evidence.
	// Always set before the writer persists the row. Unsupported matcher
	// ecosystems are withheld from impact findings and surfaced through
	// readiness coverage gaps instead.
	DetectionProfile DetectionProfile
	// Suppression carries the VEX or operator-policy decision evaluated for
	// this finding. State is always populated; it is "active" when no
	// suppression applies. The writer persists the decision on the finding
	// payload so API and MCP callers can include or exclude suppressed
	// findings and explain the rationale.
	Suppression SupplyChainSuppressionDecision
	// Remediation is the advisory-only safe-upgrade recommendation Eshu
	// computes for this finding (issue #595). The reducer never auto-opens
	// pull requests; this block exists so API and MCP callers can explain
	// the upgrade path without re-reading raw advisory or lockfile facts.
	Remediation SupplyChainImpactRemediation
}

// bakeSupplyChainCIDeclaredArtifactIdentity persists the declared artifact
// identity from the strongest matching deployment. An exact subject-digest
// match outranks every image-reference match; fact order breaks ties within
// each strength. Repository, environment, and operational-anchor matches do
// not make an artifact identity claim, so they leave both fields blank.
//
// The selected deployment contributes both fields as one atomic pair, even
// when one field is empty. No other deployment can fill that field, because
// combining deployments could associate a digest and image reference that no
// source declared together. A deployment selected by image reference may carry
// a digest that differs from the finding's subject digest; preserving that
// disagreement lets query-time version resolution report it.
func bakeSupplyChainCIDeclaredArtifactIdentity(
	finding *SupplyChainImpactFinding,
	deployments []supplyChainDeploymentContext,
) {
	var firstImageRefMatch supplyChainDeploymentContext
	hasImageRefMatch := false
	for _, deployment := range deployments {
		strongDigestMatch := finding.SubjectDigest != "" && deployment.artifactDigest != "" &&
			deployment.artifactDigest == finding.SubjectDigest
		if strongDigestMatch {
			finding.CIDeclaredArtifactDigest = deployment.artifactDigest
			finding.CIDeclaredImageRef = deployment.imageRef
			return
		}
		if !hasImageRefMatch &&
			finding.ImageRef != "" &&
			deployment.imageRef != "" &&
			deployment.imageRef == finding.ImageRef {
			firstImageRefMatch = deployment
			hasImageRefMatch = true
		}
	}
	if hasImageRefMatch {
		finding.CIDeclaredArtifactDigest = firstImageRefMatch.artifactDigest
		finding.CIDeclaredImageRef = firstImageRefMatch.imageRef
	}
}
