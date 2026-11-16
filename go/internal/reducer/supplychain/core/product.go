// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func classifySupplyChainImpactProduct(
	cve supplychainmodel.ImpactCVE,
	severityObservation supplychainmodel.ImpactCVE,
	product supplychainmodel.AffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactProductFinding(cve, severityObservation, product, index)
	component, attachment, image, hasComponentPath, imagePathMissing := firstSBOMProductImpactPath(product, index)
	if hasComponentPath {
		finding.ObservedVersion = payloadcore.FirstNonBlank(component.Version, versionFromCPE23Criteria(product.Criteria))
		finding.SubjectDigest = attachment.SubjectDigest
		finding.ImageRef = image.imageRef
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.FactID, attachment.FactID, image.factID)
		finding.EvidencePath = append(finding.EvidencePath, chain.SBOMComponentFactKind, reducercontract.SBOMAttestationAttachmentFactKind, reducercontract.ContainerImageIdentityFactKind)
		if image.repositoryID != "" {
			finding.RepositoryID = image.repositoryID
		}
		finding.Status = SupplyChainImpactAffectedDerived
		finding.Confidence = "derived_product"
		finding.RuntimeReachability = "image_sbom"
		finding.CanonicalWrites = 1
		finalizeSupplyChainImpactFinding(&finding, index, imagePathMissing, aliasGroupMissingEvidence(finding.CVEID, index))
		return finding
	}
	finding.ObservedVersion = versionFromCPE23Criteria(product.Criteria)
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "weak_product"
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	finalizeSupplyChainImpactFinding(&finding, index, imagePathMissing, aliasGroupMissingEvidence(finding.CVEID, index))
	return finding
}

// baseSupplyChainImpactProductFinding names the finding from cve, the ranked
// representative, and reports severity from severityObservation, the group's
// best-scored row (#10717). They are the same record unless the ranked row is
// the less informative one.
func baseSupplyChainImpactProductFinding(
	cve supplychainmodel.ImpactCVE,
	severityObservation supplychainmodel.ImpactCVE,
	product supplychainmodel.AffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	severity := cvss.DeriveSeverity(severityObservation.CVSSScore, severityObservation.CVSSVector, severityObservation.SeverityLabel)
	finding := SupplyChainImpactFinding{
		CVEID:               cve.CVEID,
		AdvisoryID:          payloadcore.FirstNonBlank(cve.AdvisoryID, cve.CVEID),
		ProductCriteria:     product.Criteria,
		MatchCriteriaID:     product.MatchCriteriaID,
		CVSSScore:           severity.Score,
		SeverityScoreSource: derivedScoreSource(severity),
		SeverityVector:      strings.TrimSpace(severityObservation.CVSSVector),
		SeverityLabel:       strings.TrimSpace(severityObservation.SeverityLabel),
		AdvisoryPublishedAt: cve.PublishedAt,
		AdvisoryUpdatedAt:   cve.SourceUpdatedAt,
		EvidencePath:        []string{chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedProductFactKind},
		EvidenceFactIDs:     []string{cve.FactID, severityObservation.FactID, product.FactID},
	}
	applyRiskSignals(&finding, index.riskSignals[cve.CVEID])
	applyAliasGroupOutcome(&finding, index)
	return finding
}

// applyAliasGroupOutcome carries the alias-connected grouping pass outcome
// onto a finding: the sorted member-id set and enrichment provenance on
// grouped keys. Ungrouped keys read no entry and keep per-record keys with
// no marker. Quarantine markers ride finalizeSupplyChainImpactFinding's
// extras (see aliasGroupMissingEvidence): finalize recomputes MissingEvidence
// from scratch, so a marker set here would not survive.
func applyAliasGroupOutcome(finding *SupplyChainImpactFinding, index supplyChainImpactIndex) {
	group, ok := index.aliasGroups[finding.CVEID]
	if !ok || !group.grouped {
		return
	}
	finding.AdvisoryGroupMembers = append([]string(nil), group.members...)
	finding.AdvisoryGroupEPSSSource = group.epssSource
	finding.AdvisoryGroupKEVSource = group.kevSource
}

// aliasGroupMissingEvidence returns the quarantine marker for the finding's
// group key as a finalizeSupplyChainImpactFinding extra, or nil. A fallback
// finding keeps its per-record key with the marker instead of a
// partial-component group id (#10407).
func aliasGroupMissingEvidence(cveID string, index supplyChainImpactIndex) []string {
	if group, ok := index.aliasGroups[cveID]; ok && group.missingEvidence != "" {
		return []string{group.missingEvidence}
	}
	return nil
}

func baseSupplyChainImpactFinding(
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
	holders []osHoldingFact,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	// Newest copy first (#10042): version matching, remediation, and
	// reachability read the same reduced group the provenance builder
	// below reads, so a superseded copy decides nothing anywhere on the
	// finding. The builder re-applies the same idempotent reduction for
	// its direct CPE-path caller (retraction.go).
	cves.observations, pkgs = newestCopyAdvisoryInputs(cves.observations, pkgs)
	pkg := representativeAffectedPackage(pkgs)
	observations := buildAdvisoryProvenanceObservations(cves.observations, pkgs)
	markOSHoldingObservations(observations, holders)
	provenance := selectAdvisoryProvenance(pkg.Ecosystem, observations)
	advisoryID := provenanceAdvisoryID(provenance, cves)

	finding := SupplyChainImpactFinding{
		CVEID:                cves.cveID,
		AdvisoryID:           advisoryID,
		PackageID:            pkg.PackageID,
		Ecosystem:            pkg.Ecosystem,
		PackageName:          pkg.Name,
		PURL:                 pkg.PURL,
		FixedVersion:         provenance.FixedVersion,
		CVSSScore:            provenance.SeverityScore,
		SeverityScoreSource:  provenance.SeverityScoreSource,
		SeveritySource:       provenance.SeveritySource,
		SeverityVector:       provenance.SeverityVector,
		SeverityLabel:        provenance.SeverityLabel,
		AdvisoryPublishedAt:  cves.representative().PublishedAt,
		AdvisoryUpdatedAt:    cves.representative().SourceUpdatedAt,
		AlternateSeverities:  provenance.AlternateSeverities,
		FixedVersionSource:   provenance.FixedVersionSource,
		FixedVersionBranches: provenance.FixedVersionBranches,
		RangeSource:          provenance.RangeSource,
		VulnerableRange:      provenance.VulnerableRange,
		AdvisorySources:      provenance.AdvisorySources,
		EvidencePath:         []string{chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind},
		EvidenceFactIDs:      append(provenance.EvidenceFactIDs, cves.retiredAffectedFactIDs...),
	}
	retractionSources := observations
	if osPackageClassEcosystem(pkg.Ecosystem) {
		retractionSources = recordsListingThePackage(observations)
	}
	finding.AdvisoryRetraction = index.retractionFor(cves.cveID, retractionSources)
	applyRiskSignals(&finding, index.riskSignals[cves.cveID])
	applyAliasGroupOutcome(&finding, index)
	return finding
}

// provenanceAdvisoryID picks the advisory identifier surfaced on the finding
// row. The selected severity source's advisory identifier wins so the row
// matches the source the operator sees as the primary signal; falling back
// to the first advisory observation keeps the row populated when no source
// reported a CVSS score. Only a source-reported score (IdentitySource) picks
// the identity: a score derived from a vector or a label selects the severity
// source but must not move advisory_id, which feeds finding_id and
// advisory-scoped suppression matching (#9693). An OS-class finding is named by
// its deciding record instead and never consults IdentitySource (#9914): its
// severity may come from an NVD or GHSA record in the group, but the name must not
// depend on whether this pass loaded one.
func provenanceAdvisoryID(provenance advisoryProvenanceSelection, cves supplyChainCVEGroup) string {
	if provenance.DecidingAdvisoryID != "" {
		return provenance.DecidingAdvisoryID
	}
	if provenance.IdentitySource != "" {
		for _, advisory := range provenance.AdvisorySources {
			if advisory.Source == provenance.IdentitySource {
				return advisory.AdvisoryID
			}
		}
	}
	if len(provenance.AdvisorySources) > 0 {
		return provenance.AdvisorySources[0].AdvisoryID
	}
	rep := cves.representative()
	return payloadcore.FirstNonBlank(rep.AdvisoryID, rep.CVEID)
}

func applyRiskSignals(finding *SupplyChainImpactFinding, signals supplychainmodel.RiskSignals) {
	finding.EPSSProbability = signals.EPSSProbability
	finding.EPSSPercentile = signals.EPSSPercentile
	finding.EPSSScoreDate = signals.EPSSScoreDate
	finding.EPSSModelVersion = signals.EPSSModelVersion
	finding.KnownExploited = signals.KnownExploited
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, signals.EPSSFactID, signals.KEVFactID)
	var reasons []string
	if finding.CVSSScore > 0 {
		reasons = append(reasons, fmt.Sprintf("cvss=%.1f", finding.CVSSScore))
	}
	if signals.EPSSProbability != "" {
		reasons = append(reasons, "epss="+signals.EPSSProbability)
	}
	if signals.KnownExploited {
		reasons = append(reasons, "kev=true")
	}
	if len(reasons) > 0 {
		finding.PriorityReason = strings.Join(reasons, "; ") + "; signals do not prove reachability"
	}
	finding.EvidenceFactIDs = payloadcore.UniqueSortedStrings(finding.EvidenceFactIDs)
}

// severityObservation returns the observation whose severity the product path
// reports, given the representative that names the finding. The product path
// reads one row, and the representative is chosen by source rank alone; the CVE
// List ranks above NVD, so an unscored cve_list row would otherwise hide NVD's
// score (eshu-hq/eshu#10717). This mirrors the package path's severity
// fallback: a source-reported score beats a vector-derived score, which beats a
// label, and source rank orders observations within one tier. A withdrawn or
// rejected record lends no severity, so a group whose every record is retracted
// returns its representative with the score, vector, and label blanked (a
// record's superseded copies are reduced away first, so a stale live copy never
// outvotes a newer retracted one, #10764): the
// finding keeps its identity and evidence ids but reports no severity, the same
// outcome the package path reaches. Identity never reads this row: the
// finding's advisory id, dates, and key stay the representative's. With no
// better-tiered observation the representative itself is returned, so a group
// whose ranked row already carries the best severity is unchanged.
func (g supplyChainCVEGroup) severityObservation(representative supplychainmodel.ImpactCVE) supplychainmodel.ImpactCVE {
	// Newest copy first, as baseSupplyChainImpactFinding does for the package
	// path and the retraction read does for this group: a superseded live copy
	// of a record whose newest copy is withdrawn or Rejected must not lend its
	// severity. The reduction is idempotent and allocation-free when no record
	// has two copies and the group holds four records or fewer, which is
	// every typical group (one record per source); a larger group pays one
	// key set to find out.
	observations, _ := newestCopyAdvisoryInputs(g.observations, nil)
	best := representative
	bestTier := observationSeverityTier(representative)
	bestRank := observationSourceRank(representative)
	if !slices.ContainsFunc(observations, func(o supplychainmodel.ImpactCVE) bool {
		return o.FactID == representative.FactID
	}) {
		// The ranked representative is a superseded copy: it still names the
		// finding, but it earns no severity or tier of its own, exactly as a
		// retracted representative earns none.
		best.CVSSScore, best.CVSSVector, best.SeverityLabel = 0, "", ""
		bestTier = 4
	}
	for _, observation := range observations {
		if advisoryRetractionCode(observation.Status, observation.WithdrawnAt) != "" {
			continue
		}
		tier := observationSeverityTier(observation)
		if tier > bestTier {
			continue
		}
		rank := observationSourceRank(observation)
		if tier < bestTier || rank < bestRank || (rank == bestRank && observation.FactID < best.FactID) {
			best, bestTier, bestRank = observation, tier, rank
		}
	}
	if advisoryRetractionCode(best.Status, best.WithdrawnAt) != "" {
		// Every observation is retracted, so the representative still names
		// the finding but lends no severity: a rejected record's CVSS must not
		// score (eshu-hq/eshu#10764). FactID and every identity field stay, so
		// the finding id and evidence fact ids do not move.
		best.CVSSScore, best.CVSSVector, best.SeverityLabel = 0, "", ""
	}
	return best
}

// observationSeverityTier orders what an observation says about severity, the
// lowest tier winning: a reported score, a score derived from its vector, a
// recognized label, then nothing. It is the tiering of selectFallbackSeverity,
// read through the same cvss.DeriveSeverity the finding uses.
func observationSeverityTier(observation supplychainmodel.ImpactCVE) int {
	// A retracted representative still names the finding, but it earns no tier
	// of its own, so any live observation with a severity replaces it.
	if advisoryRetractionCode(observation.Status, observation.WithdrawnAt) != "" {
		return 4
	}
	severity := cvss.DeriveSeverity(observation.CVSSScore, observation.CVSSVector, observation.SeverityLabel)
	switch {
	case severity.Score > 0 && severity.ScoreSource == cvss.ScoreSourceReported:
		return 0
	case severity.Score > 0:
		return 1
	case severity.Bucket != cvss.BucketNone:
		return 2
	default:
		return 3
	}
}

func observationSourceRank(observation supplychainmodel.ImpactCVE) int {
	return advisorySourcePriority("", classifyAdvisorySource(observation.Source, observation.AdvisoryID))
}
