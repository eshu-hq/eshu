// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
	VulnerableRange string
	MatchReason     string
	Status          SupplyChainImpactStatus
	Confidence      string
	CVSSScore       float64
	SeveritySource  string
	// SeverityScoreSource says where CVSSScore came from. It is
	// cvss.ScoreSourceDerivedFromVector when the reducer computed the score from
	// SeverityVector, and empty when the source reported the score (or there is
	// none). A derived score must never be shown as source-reported (#9693).
	SeverityScoreSource   string
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
	EPSSScoreDate         string
	EPSSModelVersion      string
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
	// ScanTargetID is the immutable authorization boundary for a derived
	// image scan. It is empty for legacy repository and image findings.
	ScanTargetID string
	// ScanGenerationID and AuthorityDependencies are protected writer inputs.
	// They are never encoded into the public reducer-derived fact payload.
	ScanGenerationID      string
	AuthorityDependencies []AdvisoryAuthorityDependency
	ImageRef              string
	DependencyScope       string
	WorkloadIDs           []string
	// ServiceWorkloadPairs is genuine (ServiceID, WorkloadID) co-occurrence
	// for tuple-aware suppression matching (#5466 F-2); see
	// runtime.go and scope.go.
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
	// AdvisoryRetraction is non-nil when every source observation of the CVE
	// group rejected or withdrew its advisory (#9695). It is the input to the
	// advisory_retracted suppression state and the source of the reason and
	// replaced_by the finding carries. The finding keeps its identity: a
	// restored advisory re-derives the same finding id.
	AdvisoryRetraction *AdvisoryRetraction
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
	deployments []supplychainmodel.DeploymentContext,
) {
	var firstImageRefMatch supplychainmodel.DeploymentContext
	hasImageRefMatch := false
	for _, deployment := range deployments {
		strongDigestMatch := finding.SubjectDigest != "" && deployment.ArtifactDigest != "" &&
			deployment.ArtifactDigest == finding.SubjectDigest
		if strongDigestMatch {
			finding.CIDeclaredArtifactDigest = deployment.ArtifactDigest
			finding.CIDeclaredImageRef = deployment.ImageRef
			return
		}
		if !hasImageRefMatch &&
			finding.ImageRef != "" &&
			deployment.ImageRef != "" &&
			deployment.ImageRef == finding.ImageRef {
			firstImageRefMatch = deployment
			hasImageRefMatch = true
		}
	}
	if hasImageRefMatch {
		finding.CIDeclaredArtifactDigest = firstImageRefMatch.ArtifactDigest
		finding.CIDeclaredImageRef = firstImageRefMatch.ImageRef
	}
}

// Severity outcomes counted by eshu_dp_supply_chain_severity_decisions_total
// (#9693). The set is closed so the metric label stays bounded.
const (
	severityOutcomeSourceReported = "source_reported"
	severityOutcomeDerived        = "derived_from_vector"
	severityOutcomeLabelOnly      = "label_only"
	severityOutcomeNone           = "none"
)

// supplyChainSeverityOutcome classifies where a finding's severity came from.
func supplyChainSeverityOutcome(finding SupplyChainImpactFinding) string {
	switch {
	case finding.CVSSScore > 0 && finding.SeverityScoreSource == cvss.ScoreSourceDerivedFromVector:
		return severityOutcomeDerived
	case finding.CVSSScore > 0:
		return severityOutcomeSourceReported
	case cvss.LabelBucket(finding.SeverityLabel) != cvss.BucketNone:
		return severityOutcomeLabelOnly
	default:
		return severityOutcomeNone
	}
}

// emitSeverityDecisions records one severity outcome per finding. It runs once
// per reducer pass after the write, beside the other per-finding counters, and
// adds at most four series.
func (h SupplyChainImpactHandler) emitSeverityDecisions(ctx context.Context, findings []SupplyChainImpactFinding) {
	if h.Instruments == nil || h.Instruments.SupplyChainSeverityDecisions == nil {
		return
	}
	counts := make(map[string]int64, 4)
	for _, finding := range findings {
		counts[supplyChainSeverityOutcome(finding)]++
	}
	for outcome, count := range counts {
		h.Instruments.SupplyChainSeverityDecisions.Add(ctx, count, metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
			telemetry.AttrOutcome(outcome),
		))
	}
}
