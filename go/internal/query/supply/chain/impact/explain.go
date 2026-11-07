// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// ExplanationStore reads one reducer-owned vulnerability
// impact finding and the bounded evidence facts referenced by that finding.
type ExplanationStore interface {
	ExplainSupplyChainImpact(
		context.Context,
		packet.ExplanationFilter,
	) (ExplanationRow, error)
}

// ExplanationRow contains the durable impact finding and only
// the source or reducer facts referenced by that finding's evidence ids.
type ExplanationRow struct {
	Finding       FindingRow
	EvidenceFacts []EvidenceFact
}

// EvidenceFact is a bounded fact preview used to explain one
// reducer-owned vulnerability impact finding without returning raw graph paths
// or whole advisory bodies.
type EvidenceFact struct {
	FactID           string
	FactKind         string
	SourceSystem     string
	SourceConfidence string
	ObservedAt       time.Time
	// SchemaVersion is the fact row's persisted schema_version, threaded into
	// the typed factschema decode seam so a non-1.x (future/unsupported major)
	// evidence fact dead-letters instead of being decoded as v1. An empty
	// value normalizes to decode.DefaultSchemaMajorVersion in
	// supplyChainSchemaEnvelope, matching the version-less legacy default.
	SchemaVersion string
	Payload       map[string]any
}

// BuildExplanation shapes a durable impact finding and its
// referenced evidence facts into one bounded explain response.
func BuildExplanation(
	filter packet.ExplanationFilter,
	row ExplanationRow,
	readiness packet.ReadinessEnvelope,
) packet.ExplanationResult {
	finding := BuildFindingResult(&row.Finding)
	advisory := buildSupplyChainAdvisoryExplanation(row)
	component := buildSupplyChainComponentExplanation(row)
	version := buildSupplyChainVersionExplanation(row, advisory, component)
	anchors := buildSupplyChainExplanationAnchors(row)
	dependencyChain := buildSupplyChainDependencyChain(row.Finding, row.EvidenceFacts)
	missing := explanationMissingEvidence(row.Finding, readiness, advisory, component, version, dependencyChain, anchors)
	impactPath := buildSupplyChainImpactPath(row, supplyChainImpactPathMissingEvidence(normalizedSupplyChainImpactMissingEvidence(&row.Finding)))
	remediation := buildSupplyChainRemediationExplanation(row, advisory, version, component, dependencyChain)
	return packet.ExplanationResult{
		Outcome:              "finding_explained",
		EvidencePacketHandle: supplyChainImpactEvidencePacketHandle(filter, row.Finding.FindingID),
		Input:                filter,
		Finding:              &finding,
		Advisory:             advisory,
		Component:            component,
		Version:              version,
		DependencyChain:      dependencyChain,
		Anchors:              anchors,
		Path:                 impactPath,
		Evidence:             summarizeSupplyChainEvidenceFacts(row.EvidenceFacts),
		Readiness:            readiness,
		MissingEvidence:      missing,
		Freshness:            supplyChainExplanationFreshness(row.EvidenceFacts, readiness.Freshness),
		Remediation:          remediation,
	}
}

// BuildNoEvidenceExplanation returns a bounded explanation for
// a valid scope where no reducer-owned impact finding currently exists.
func BuildNoEvidenceExplanation(
	filter packet.ExplanationFilter,
	readiness packet.ReadinessEnvelope,
) packet.ExplanationResult {
	missing := explanationUniqueStrings(append([]string{"impact_finding"}, readiness.MissingEvidence...))
	return packet.ExplanationResult{
		Outcome:              "no_finding",
		EvidencePacketHandle: supplyChainImpactEvidencePacketHandle(filter, ""),
		Input:                filter,
		Advisory:             packet.AdvisoryExplanation{CVEID: filter.CVEID, AdvisoryID: filter.AdvisoryID},
		Component:            packet.ComponentExplanation{PackageID: filter.PackageID},
		Version:              packet.VersionExplanation{VersionEvidence: "missing"},
		Anchors: packet.ExplanationAnchors{
			RepositoryID:  filter.RepositoryID,
			SubjectDigest: filter.SubjectDigest,
			ImageRefs:     compactStrings([]string{filter.ImageRef}),
			Workloads:     compactStrings([]string{filter.WorkloadID}),
			Services:      compactStrings([]string{filter.ServiceID}),
		},
		Evidence:        []packet.EvidenceFactSummary{},
		Readiness:       readiness,
		MissingEvidence: missing,
		Freshness:       packet.ExplanationFreshness{State: explanationFreshnessState(readiness.Freshness)},
	}
}

// BuildAmbiguousExplanation returns a bounded refusal envelope
// for a valid scope that matches multiple reducer-owned impact findings.
func BuildAmbiguousExplanation(
	filter packet.ExplanationFilter,
	readiness packet.ReadinessEnvelope,
	candidateCount int,
) packet.ExplanationResult {
	readiness = supplyChainImpactAmbiguousReadiness(readiness, candidateCount)
	body := buildSupplyChainImpactRefusalExplanation(filter, readiness)
	body.Outcome = "ambiguous_scope"
	body.MissingEvidence = explanationUniqueStrings(append([]string{"ambiguous_scope"}, readiness.MissingEvidence...))
	return body
}

func supplyChainImpactAmbiguousReadiness(
	readiness packet.ReadinessEnvelope,
	candidateCount int,
) packet.ReadinessEnvelope {
	if readiness.State != packet.ReadinessStateReadinessUnavailable {
		readiness.State = packet.ReadinessStateAmbiguousScope
	}
	if candidateCount < 2 {
		candidateCount = 2
	}
	if readiness.Counts.FindingsReturned < candidateCount {
		readiness.Counts.FindingsReturned = candidateCount
	}
	readiness.MissingEvidence = explanationUniqueStrings(append(readiness.MissingEvidence, "ambiguous_scope"))
	return readiness
}

func buildSupplyChainImpactRefusalExplanation(
	filter packet.ExplanationFilter,
	readiness packet.ReadinessEnvelope,
) packet.ExplanationResult {
	return packet.ExplanationResult{
		EvidencePacketHandle: supplyChainImpactEvidencePacketHandle(filter, ""),
		Input:                filter,
		Advisory:             packet.AdvisoryExplanation{CVEID: filter.CVEID, AdvisoryID: filter.AdvisoryID},
		Component:            packet.ComponentExplanation{PackageID: filter.PackageID},
		Version:              packet.VersionExplanation{VersionEvidence: "missing"},
		Anchors: packet.ExplanationAnchors{
			RepositoryID:  filter.RepositoryID,
			SubjectDigest: filter.SubjectDigest,
			ImageRefs:     compactStrings([]string{filter.ImageRef}),
			Workloads:     compactStrings([]string{filter.WorkloadID}),
			Services:      compactStrings([]string{filter.ServiceID}),
		},
		Evidence:  []packet.EvidenceFactSummary{},
		Readiness: readiness,
		Freshness: packet.ExplanationFreshness{State: explanationFreshnessState(readiness.Freshness)},
	}
}

func supplyChainImpactEvidencePacketHandle(filter packet.ExplanationFilter, findingID string) string {
	if findingID = strings.TrimSpace(findingID); findingID != "" {
		return "supply-chain-impact-explanation:finding:" + findingID
	}
	scope := strings.Join([]string{
		"finding_id=" + strings.TrimSpace(filter.FindingID),
		"advisory_id=" + strings.TrimSpace(filter.AdvisoryID),
		"cve_id=" + strings.TrimSpace(filter.CVEID),
		"package_id=" + strings.TrimSpace(filter.PackageID),
		"repository_id=" + strings.TrimSpace(filter.RepositoryID),
		"subject_digest=" + strings.TrimSpace(filter.SubjectDigest),
		"image_ref=" + strings.TrimSpace(filter.ImageRef),
		"workload_id=" + strings.TrimSpace(filter.WorkloadID),
		"service_id=" + strings.TrimSpace(filter.ServiceID),
	}, "\x00")
	sum := sha256.Sum256([]byte(scope))
	return "supply-chain-impact-explanation:scope:" + hex.EncodeToString(sum[:])
}

// FindingReadinessScope builds the readiness target scope for row, starting
// from fallback's ReadinessScope and filling in any field fallback left empty
// from row's own anchors.
func FindingReadinessScope(row FindingRow, fallback packet.ExplanationFilter) packet.TargetScope {
	scope := fallback.ReadinessScope()
	if scope.CVEID == "" {
		scope.CVEID = row.CVEID
	}
	if scope.AdvisoryID == "" {
		scope.AdvisoryID = row.AdvisoryID
	}
	if scope.PackageID == "" {
		scope.PackageID = row.PackageID
	}
	if scope.RepositoryID == "" {
		scope.RepositoryID = row.RepositoryID
	}
	if scope.SubjectDigest == "" {
		scope.SubjectDigest = row.SubjectDigest
	}
	if scope.ImageRef == "" {
		scope.ImageRef = row.ImageRef
	}
	if scope.WorkloadID == "" && len(row.WorkloadIDs) == 1 {
		scope.WorkloadID = row.WorkloadIDs[0]
	}
	if scope.ServiceID == "" && len(row.ServiceIDs) == 1 {
		scope.ServiceID = row.ServiceIDs[0]
	}
	return scope
}
