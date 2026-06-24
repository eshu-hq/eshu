// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
)

// supplyChainCVEGroup is the consolidated per-(cve_id) view of every
// source-attributed CVE observation. Reducers walk one group per CVE so
// finding admission emits one row per advisory identity instead of one row
// per source overwriting earlier rows.
type supplyChainCVEGroup struct {
	cveID        string
	observations []supplyChainImpactCVE
}

// representative returns the highest-priority non-withdrawn observation for
// the group so callers that still need a single CVE row (legacy product-only
// path) get a deterministic choice without losing the rest of the provenance
// list. Group-level callers do not know the package ecosystem, so the
// language-class priority table supplies the ranking; for the product-only
// CPE path the only source emitting `affected_product` facts is NVD, which
// remains the deterministic tail of that ranking. Withdrawn observations
// are only returned when every observation in the group is withdrawn, so
// callers still get a row with `WithdrawnAt` set rather than the zero value.
func (g supplyChainCVEGroup) representative() supplyChainImpactCVE {
	if len(g.observations) == 0 {
		return supplyChainImpactCVE{cveID: g.cveID}
	}
	best := -1
	bestRank := 0
	for i, observation := range g.observations {
		if strings.TrimSpace(observation.withdrawnAt) != "" {
			continue
		}
		rank := advisorySourcePriority("", classifyAdvisorySource(observation.source, observation.advisoryID))
		if best < 0 || rank < bestRank ||
			(rank == bestRank && observation.factID < g.observations[best].factID) {
			best = i
			bestRank = rank
		}
	}
	if best < 0 {
		return g.observations[0]
	}
	return g.observations[best]
}

func groupSupplyChainCVEsByID(observations []supplyChainImpactCVE) map[string]supplyChainCVEGroup {
	groups := make(map[string]supplyChainCVEGroup, len(observations))
	for _, observation := range observations {
		group := groups[observation.cveID]
		group.cveID = observation.cveID
		group.observations = append(group.observations, observation)
		groups[observation.cveID] = group
	}
	return groups
}

func sortedCVEKeys(groups map[string]supplyChainCVEGroup) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func groupSupplyChainAffectedByPackage(observations []supplyChainAffectedPackage) map[string][]supplyChainAffectedPackage {
	groups := make(map[string][]supplyChainAffectedPackage, len(observations))
	for _, observation := range observations {
		groups[observation.packageID] = append(groups[observation.packageID], observation)
	}
	return groups
}

func sortedPackageKeys(groups map[string][]supplyChainAffectedPackage) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// representativeAffectedPackage picks the highest-priority source's affected
// package as the row used for shape fields (ecosystem, package name, purl).
// Per-source provenance still flows through the AdvisoryProvenanceObservation
// list so callers do not lose vendor or upstream rows.
func representativeAffectedPackage(packages []supplyChainAffectedPackage) supplyChainAffectedPackage {
	if len(packages) == 0 {
		return supplyChainAffectedPackage{}
	}
	if len(packages) == 1 {
		return packages[0]
	}
	ecosystem := strings.ToLower(strings.TrimSpace(packages[0].ecosystem))
	sorted := append([]supplyChainAffectedPackage(nil), packages...)
	sort.SliceStable(sorted, func(i, j int) bool {
		si := classifyAdvisorySource(sorted[i].source, sorted[i].advisoryID)
		sj := classifyAdvisorySource(sorted[j].source, sorted[j].advisoryID)
		ri := advisorySourcePriority(ecosystem, si)
		rj := advisorySourcePriority(ecosystem, sj)
		if ri != rj {
			return ri < rj
		}
		if sorted[i].source != sorted[j].source {
			return sorted[i].source < sorted[j].source
		}
		return sorted[i].factID < sorted[j].factID
	})
	return sorted[0]
}

// buildAdvisoryProvenanceObservations consolidates per-source CVE and
// affected_package observations into the shape the priority selector needs.
// Each CVE observation contributes severity and withdrawal timestamp; the
// matching affected_package observation contributes the source's
// fixed-version branches and vulnerable-range text. Observations without a
// matching counterpart are still preserved so callers see the full source
// list.
func buildAdvisoryProvenanceObservations(
	cves []supplyChainImpactCVE,
	packages []supplyChainAffectedPackage,
) []AdvisoryProvenanceObservation {
	observations := make([]AdvisoryProvenanceObservation, 0, len(cves)+len(packages))
	matchedAffected := make(map[string]bool, len(packages))
	for _, cve := range cves {
		matched := matchAffectedForCVE(cve, packages, matchedAffected)
		source := classifyAdvisorySource(cve.source, cve.advisoryID)
		if packageSource := classifyAffectedPackageAdvisorySource(matched); packageSource != "" {
			source = packageSource
		}
		observations = append(observations, AdvisoryProvenanceObservation{
			Source:          source,
			AdvisoryID:      firstNonBlank(cve.advisoryID, cve.cveID),
			SourceUpdatedAt: strings.TrimSpace(cve.sourceUpdatedAt),
			SeverityScore:   cve.cvssScore,
			SeverityVector:  strings.TrimSpace(cve.cvssVector),
			SeverityLabel:   strings.TrimSpace(cve.severityLabel),
			FixedVersions:   append([]string(nil), matched.fixedVersions...),
			AffectedRange:   supplyChainAffectedRangeSummary(matched),
			WithdrawnAt:     strings.TrimSpace(cve.withdrawnAt),
			CVEFactID:       cve.factID,
			AffectedFactID:  matched.factID,
		})
	}
	for _, pkg := range packages {
		if matchedAffected[pkg.factID] {
			continue
		}
		observations = append(observations, AdvisoryProvenanceObservation{
			Source:         classifyAffectedPackageAdvisorySource(pkg),
			AdvisoryID:     firstNonBlank(pkg.advisoryID, pkg.cveID),
			FixedVersions:  append([]string(nil), pkg.fixedVersions...),
			AffectedRange:  supplyChainAffectedRangeSummary(pkg),
			AffectedFactID: pkg.factID,
		})
	}
	return observations
}

func matchAffectedForCVE(
	cve supplyChainImpactCVE,
	packages []supplyChainAffectedPackage,
	matched map[string]bool,
) supplyChainAffectedPackage {
	for _, pkg := range packages {
		if matched[pkg.factID] {
			continue
		}
		if classifyAdvisorySource(pkg.source, pkg.advisoryID) != classifyAdvisorySource(cve.source, cve.advisoryID) {
			continue
		}
		if pkg.advisoryID != "" && cve.advisoryID != "" && pkg.advisoryID != cve.advisoryID {
			continue
		}
		matched[pkg.factID] = true
		return pkg
	}
	return supplyChainAffectedPackage{}
}

// AdvisoryProvenanceObservation captures one source-attributed advisory
// observation for a CVE+package pair so reducer admission can preserve where
// severity, fixed-version, and vulnerable-range truth came from.
type AdvisoryProvenanceObservation struct {
	// Source is the logical advisory source name (for example "ghsa", "nvd",
	// "osv", "glad", or a vendor security source). It is derived from the
	// source-fact payload and the advisory identifier prefix so a GHSA
	// observation collected via OSV is still reported as a GHSA observation.
	Source string
	// AdvisoryID is the source-reported advisory identifier (CVE-..., GHSA-...,
	// RHSA-..., GLSA-..., etc.).
	AdvisoryID string
	// SourceUpdatedAt records when the source last modified its advisory.
	SourceUpdatedAt string
	// SeverityScore is the source-reported CVSS base score. Zero means the
	// source did not publish a score for this advisory.
	SeverityScore float64
	// SeverityVector is the source-reported CVSS vector string.
	SeverityVector string
	// SeverityLabel is the source-reported severity label (CRITICAL, HIGH, ...).
	SeverityLabel string
	// FixedVersions lists every source-reported fixed version branch.
	FixedVersions []string
	// AffectedRange is the source-reported textual vulnerable range when present.
	AffectedRange string
	// WithdrawnAt records when the source withdrew the advisory; non-empty
	// observations must not be selected for severity, fixed-version, or range.
	WithdrawnAt string
	// CVEFactID is the source-fact id of the cve envelope that contributed this
	// observation, used to wire EvidenceFactIDs in the resulting finding.
	CVEFactID string
	// AffectedFactID is the source-fact id of the affected_package envelope
	// that contributed this observation, when present.
	AffectedFactID string
}

// AlternateSeverity is one source-attributed severity that was not selected
// for the finding but is preserved so callers can see vendor/source
// disagreement.
type AlternateSeverity struct {
	Source string
	Score  float64
	Vector string
	Label  string
}

// FixedVersionBranch records one source-attributed fixed-version branch.
type FixedVersionBranch struct {
	Version string
	Source  string
}

// AdvisorySourceObservation is the bounded provenance row surfaced through
// the finding payload. It carries source identity, advisory identifier,
// update timestamp, and withdrawal timestamp so API/MCP callers can explain
// why one severity was selected over alternates without re-reading raw
// source facts.
type AdvisorySourceObservation struct {
	Source          string
	AdvisoryID      string
	SourceUpdatedAt string
	WithdrawnAt     string
}

// advisoryProvenanceSelection is the consolidated result of applying
// ecosystem-aware source priority to a per-(cve_id, package_id) set of
// observations.
type advisoryProvenanceSelection struct {
	SeveritySource       string
	SeverityScore        float64
	SeverityVector       string
	SeverityLabel        string
	AlternateSeverities  []AlternateSeverity
	FixedVersionSource   string
	FixedVersion         string
	FixedVersionBranches []FixedVersionBranch
	RangeSource          string
	// VulnerableRange is the raw vulnerable-range expression Eshu copied
	// from the selected source observation so list-route callers see the
	// same expression as the explain route.
	VulnerableRange string
	AdvisorySources []AdvisorySourceObservation
	EvidenceFactIDs []string
}

// classifyAdvisorySource maps the source-fact payload's collector source name
// and the advisory identifier prefix to a logical advisory source name. OSV
// transports advisories from many upstream feeds (GHSA, PYSEC, MAL, RUSTSEC,
// GO, etc.), so the prefix decides which logical source we attribute to.
