// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
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
// remains the deterministic tail of that ranking. Withdrawn observations are
// only returned when every observation in the group is withdrawn, so callers
// still get a row with `WithdrawnAt` set rather than the zero value. Only the
// withdrawn timestamp excludes a record here, exactly as before the status
// field existed: the representative names the finding's advisory id, and a
// rejected status must not move a finding's identity (#9695).
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

// supplyChainCVEGroupKey is the one definition of the key a vulnerability.cve
// fact groups under: its trimmed cve_id, or its advisory_id when it has none. The
// decode that fills ImpactCVE.CVEID and the unreadable-record guard both call it,
// so a quarantined record is recorded under the key its group would have used.
func supplyChainCVEGroupKey(cveID, advisoryID string) string {
	return payloadcore.FirstNonBlank(cveID, advisoryID)
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
	ecosystem := packageidentity.LowerASCII(strings.TrimSpace(packages[0].Ecosystem))
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
		severity := cvss.DeriveSeverity(cve.CVSSScore, cve.CVSSVector, cve.SeverityLabel)
		matched := matchAffectedForCVE(cve, packages, matchedAffected)
		source := classifyAdvisorySource(cve.Source, cve.AdvisoryID)
		if packageSource := classifyAffectedPackageAdvisorySource(matched); packageSource != "" {
			source = packageSource
		}
		observations = append(observations, AdvisoryProvenanceObservation{
			Source:              source,
			AdvisoryID:          payloadcore.FirstNonBlank(cve.AdvisoryID, cve.CVEID),
			SourceUpdatedAt:     strings.TrimSpace(cve.SourceUpdatedAt),
			SeverityScore:       severity.Score,
			SeverityScoreSource: derivedScoreSource(severity),
			SeverityVector:      strings.TrimSpace(cve.CVSSVector),
			SeverityLabel:       strings.TrimSpace(cve.SeverityLabel),
			FixedVersions:       append([]string(nil), matched.FixedVersions...),
			AffectedRange:       supplyChainAffectedRangeSummary(matched),
			WithdrawnAt:         strings.TrimSpace(cve.WithdrawnAt),
			Status:              strings.TrimSpace(cve.Status),
			ReplacedBy:          cleanReplacementIDs(cve.ReplacedBy),
			CVEFactID:           cve.FactID,
			AffectedFactID:      matched.FactID,
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
	// SeverityScore is the CVSS base score for this observation: the
	// source-reported score, or the score computed from SeverityVector when the
	// source published only a vector (#9693). Zero means neither exists.
	SeverityScore float64
	// SeverityScoreSource is cvss.ScoreSourceDerivedFromVector when
	// SeverityScore was computed from SeverityVector, and empty when the source
	// reported it.
	SeverityScoreSource string
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
	// Status is the source-reported lifecycle status, verbatim. A "rejected" or
	// "withdrawn" status retracts the observation exactly as WithdrawnAt does.
	Status string
	// ReplacedBy lists the CVE ids the source names as the replacement.
	ReplacedBy []string
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
	// ScoreSource is cvss.ScoreSourceDerivedFromVector when Score was computed
	// from Vector rather than reported by Source, and empty otherwise.
	ScoreSource string
	Vector      string
	Label       string
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
	// Status and ReplacedBy repeat what the source reported, so a group that
	// only one source retracted still shows that rejection (#9695).
	Status     string
	ReplacedBy []string
}

// advisoryProvenanceSelection is the consolidated result of applying
// ecosystem-aware source priority to a per-(cve_id, package_id) set of
// observations.
type advisoryProvenanceSelection struct {
	SeveritySource string
	// IdentitySource is the source whose advisory id names the finding. It is
	// set only by a source-reported score, the one rule that has always chosen
	// the advisory identity. A derived score or a label sets SeveritySource but
	// never IdentitySource, so deriving severity cannot move advisory_id,
	// finding_id, or advisory-scoped suppression matching (#9693).
	IdentitySource       string
	SeverityScore        float64
	SeverityScoreSource  string
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

func supplyChainImpactProvenancePayload(finding SupplyChainImpactFinding) map[string]any {
	out := map[string]any{}
	if finding.EPSSScoreDate != "" {
		out["selected_epss_score_date"] = finding.EPSSScoreDate
	}
	if finding.EPSSModelVersion != "" {
		out["selected_epss_model_version"] = finding.EPSSModelVersion
	}
	if finding.SeveritySource != "" {
		out["selected_severity_source"] = finding.SeveritySource
	}
	if finding.SeverityVector != "" {
		out["selected_severity_vector"] = finding.SeverityVector
	}
	if finding.SeverityLabel != "" {
		out["selected_severity_label"] = finding.SeverityLabel
	}
	if finding.CVSSScore != 0 {
		out["selected_severity_score"] = finding.CVSSScore
	}
	if finding.SeverityScoreSource != "" {
		out["selected_severity_score_source"] = finding.SeverityScoreSource
	}
	if finding.FixedVersionSource != "" {
		out["selected_fixed_version_source"] = finding.FixedVersionSource
	}
	if finding.RangeSource != "" {
		out["selected_range_source"] = finding.RangeSource
	}
	if alternates := serializeAlternateSeverities(finding.AlternateSeverities); len(alternates) > 0 {
		out["alternate_severities"] = alternates
	}
	if branches := serializeFixedVersionBranches(finding.FixedVersionBranches); len(branches) > 0 {
		out["fixed_version_branches"] = branches
	}
	if advisories := serializeAdvisorySources(finding.AdvisorySources); len(advisories) > 0 {
		out["advisory_sources"] = advisories
	}
	if retraction := serializeAdvisoryRetraction(finding.AdvisoryRetraction); retraction != nil {
		out["advisory_retraction"] = retraction
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func serializeAlternateSeverities(values []AlternateSeverity) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row := map[string]any{"source": value.Source}
		if value.Score != 0 {
			row["score"] = value.Score
		}
		if value.ScoreSource != "" {
			row["score_source"] = value.ScoreSource
		}
		if value.Vector != "" {
			row["vector"] = value.Vector
		}
		if value.Label != "" {
			row["label"] = value.Label
		}
		out = append(out, row)
	}
	return out
}
