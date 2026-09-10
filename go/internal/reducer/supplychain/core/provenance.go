// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// supplyChainCVEGroup is the consolidated per-(cve_id) view of every
// source-attributed CVE observation. Reducers walk one group per CVE so
// finding admission emits one row per advisory identity instead of one row
// per source overwriting earlier rows.
type supplyChainCVEGroup struct {
	cveID        string
	observations []supplychainmodel.ImpactCVE
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
func (g supplyChainCVEGroup) representative() supplychainmodel.ImpactCVE {
	if len(g.observations) == 0 {
		return supplychainmodel.ImpactCVE{CVEID: g.cveID}
	}
	best := -1
	bestRank := 0
	for i, observation := range g.observations {
		if strings.TrimSpace(observation.WithdrawnAt) != "" {
			continue
		}
		rank := advisorySourcePriority("", classifyAdvisorySource(observation.Source, observation.AdvisoryID))
		if best < 0 || rank < bestRank ||
			(rank == bestRank && observation.FactID < g.observations[best].FactID) {
			best = i
			bestRank = rank
		}
	}
	if best < 0 {
		return g.observations[0]
	}
	return g.observations[best]
}

func groupSupplyChainCVEsByID(observations []supplychainmodel.ImpactCVE) map[string]supplyChainCVEGroup {
	groups := make(map[string]supplyChainCVEGroup, len(observations))
	for _, observation := range observations {
		group := groups[observation.CVEID]
		group.cveID = observation.CVEID
		group.observations = append(group.observations, observation)
		groups[observation.CVEID] = group
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

func groupSupplyChainAffectedByPackage(observations []supplychainmodel.AffectedPackage) map[string][]supplychainmodel.AffectedPackage {
	groups := make(map[string][]supplychainmodel.AffectedPackage, len(observations))
	for _, observation := range observations {
		groups[observation.PackageID] = append(groups[observation.PackageID], observation)
	}
	return groups
}

func sortedPackageKeys(groups map[string][]supplychainmodel.AffectedPackage) []string {
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
func representativeAffectedPackage(packages []supplychainmodel.AffectedPackage) supplychainmodel.AffectedPackage {
	if len(packages) == 0 {
		return supplychainmodel.AffectedPackage{}
	}
	if len(packages) == 1 {
		return packages[0]
	}
	ecosystem := strings.ToLower(strings.TrimSpace(packages[0].Ecosystem))
	sorted := append([]supplychainmodel.AffectedPackage(nil), packages...)
	sort.SliceStable(sorted, func(i, j int) bool {
		si := classifyAdvisorySource(sorted[i].Source, sorted[i].AdvisoryID)
		sj := classifyAdvisorySource(sorted[j].Source, sorted[j].AdvisoryID)
		ri := advisorySourcePriority(ecosystem, si)
		rj := advisorySourcePriority(ecosystem, sj)
		if ri != rj {
			return ri < rj
		}
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		return sorted[i].FactID < sorted[j].FactID
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
	cves []supplychainmodel.ImpactCVE,
	packages []supplychainmodel.AffectedPackage,
) []AdvisoryProvenanceObservation {
	observations := make([]AdvisoryProvenanceObservation, 0, len(cves)+len(packages))
	matchedAffected := make(map[string]bool, len(packages))
	for _, cve := range cves {
		matched := matchAffectedForCVE(cve, packages, matchedAffected)
		source := classifyAdvisorySource(cve.Source, cve.AdvisoryID)
		if packageSource := classifyAffectedPackageAdvisorySource(matched); packageSource != "" {
			source = packageSource
		}
		observations = append(observations, AdvisoryProvenanceObservation{
			Source:          source,
			AdvisoryID:      payloadcore.FirstNonBlank(cve.AdvisoryID, cve.CVEID),
			SourceUpdatedAt: strings.TrimSpace(cve.SourceUpdatedAt),
			SeverityScore:   cve.CVSSScore,
			SeverityVector:  strings.TrimSpace(cve.CVSSVector),
			SeverityLabel:   strings.TrimSpace(cve.SeverityLabel),
			FixedVersions:   append([]string(nil), matched.FixedVersions...),
			AffectedRange:   supplyChainAffectedRangeSummary(matched),
			WithdrawnAt:     strings.TrimSpace(cve.WithdrawnAt),
			CVEFactID:       cve.FactID,
			AffectedFactID:  matched.FactID,
		})
	}
	for _, pkg := range packages {
		if matchedAffected[pkg.FactID] {
			continue
		}
		observations = append(observations, AdvisoryProvenanceObservation{
			Source:         classifyAffectedPackageAdvisorySource(pkg),
			AdvisoryID:     payloadcore.FirstNonBlank(pkg.AdvisoryID, pkg.CVEID),
			FixedVersions:  append([]string(nil), pkg.FixedVersions...),
			AffectedRange:  supplyChainAffectedRangeSummary(pkg),
			AffectedFactID: pkg.FactID,
		})
	}
	return observations
}

func matchAffectedForCVE(
	cve supplychainmodel.ImpactCVE,
	packages []supplychainmodel.AffectedPackage,
	matched map[string]bool,
) supplychainmodel.AffectedPackage {
	for _, pkg := range packages {
		if matched[pkg.FactID] {
			continue
		}
		if classifyAdvisorySource(pkg.Source, pkg.AdvisoryID) != classifyAdvisorySource(cve.Source, cve.AdvisoryID) {
			continue
		}
		if pkg.AdvisoryID != "" && cve.AdvisoryID != "" && pkg.AdvisoryID != cve.AdvisoryID {
			continue
		}
		matched[pkg.FactID] = true
		return pkg
	}
	return supplychainmodel.AffectedPackage{}
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
