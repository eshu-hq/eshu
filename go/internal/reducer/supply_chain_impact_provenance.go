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
		observations = append(observations, AdvisoryProvenanceObservation{
			Source:          classifyAdvisorySource(cve.source, cve.advisoryID),
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
			Source:         classifyAdvisorySource(pkg.source, pkg.advisoryID),
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
func classifyAdvisorySource(payloadSource string, advisoryID string) string {
	source := strings.ToLower(strings.TrimSpace(payloadSource))
	id := strings.ToUpper(strings.TrimSpace(advisoryID))
	switch source {
	case "osv":
		switch {
		case strings.HasPrefix(id, "GHSA-"):
			return "ghsa"
		case strings.HasPrefix(id, "PYSEC-"):
			return "pysec"
		case strings.HasPrefix(id, "GO-"):
			return "osv-go"
		case strings.HasPrefix(id, "RUSTSEC-"):
			return "rustsec"
		case strings.HasPrefix(id, "MAL-"):
			return "osv-malicious"
		default:
			return "osv"
		}
	case "":
		// Default to nvd for the legacy test envelopes that did not stamp a
		// source field. The reducer must still classify them rather than
		// emitting an empty provenance row.
		return "nvd"
	default:
		return source
	}
}

// advisorySourcePriority returns the rank for one logical advisory source in
// one ecosystem. Lower numbers beat higher numbers; sources without a
// configured rank fall back to a stable terminal rank so selection remains
// deterministic. The priority table follows the principle that
// ecosystem-native or vendor-curated advisories outrank generic NVD records:
//
//   - For OS package classes (rpm, deb, apk, alpine, debian, ubuntu, rhel,
//     redhat, ...) the matching vendor advisory beats GLAD, GHSA, and NVD.
//   - For language ecosystems (npm, pypi, go, maven, ...) GHSA beats GLAD,
//     OSV, and NVD.
func advisorySourcePriority(ecosystem string, source string) int {
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	source = strings.ToLower(strings.TrimSpace(source))
	if vendor, ok := vendorForOSPackageClass(ecosystem); ok {
		return vendorClassPriorityRank(vendor, source)
	}
	return languageClassPriorityRank(source)
}

// languageClassPriorityRank returns the rank for one advisory source under
// the language ecosystem priority table (npm, pypi, maven, ...). Inlined as a
// switch so sort comparators do not allocate a fresh map per comparison.
func languageClassPriorityRank(source string) int {
	switch source {
	case "ghsa":
		return 1
	case "glad":
		return 2
	case "osv", "pysec", "osv-go", "rustsec":
		return 3
	case "osv-malicious":
		return 4
	case "nvd":
		return 5
	default:
		// Unknown source: rank after every known source but before missing
		// observations so the selector still produces a deterministic answer.
		return 999
	}
}

// vendorClassPriorityRank returns the rank for one advisory source under an
// OS-vendor ecosystem priority table, where the matching vendor advisory
// outranks GLAD, GHSA, OSV, and NVD because vendor backports change
// applicability. Inlined as a switch so sort comparators stay allocation-free.
func vendorClassPriorityRank(vendor string, source string) int {
	if source == vendor {
		return 1
	}
	switch source {
	case "glad":
		return 2
	case "ghsa":
		return 3
	case "osv", "pysec", "osv-go", "rustsec":
		return 4
	case "nvd":
		return 5
	default:
		return 999
	}
}

// vendorForOSPackageClass returns the vendor advisory source name expected
// for one OS package class, when the ecosystem maps to a known vendor.
func vendorForOSPackageClass(ecosystem string) (string, bool) {
	switch ecosystem {
	case "rpm", "rhel", "redhat", "centos", "rockylinux", "rocky":
		return "redhat", true
	case "deb", "debian":
		return "debian", true
	case "ubuntu":
		return "ubuntu", true
	case "alpine", "apk":
		return "alpine", true
	case "amazonlinux", "amazon":
		return "amazonlinux", true
	case "suse", "opensuse":
		return "suse", true
	case "wolfi":
		return "wolfi", true
	case "chainguard":
		return "chainguard", true
	case "oracle", "oraclelinux":
		return "oracle", true
	default:
		return "", false
	}
}

// selectAdvisoryProvenance applies ecosystem-aware source priority to a set
// of per-source observations and returns the consolidated selection along
// with alternate severities, fixed-version branches, and advisory-source
// rows for downstream serialization.
func selectAdvisoryProvenance(
	ecosystem string,
	observations []AdvisoryProvenanceObservation,
) advisoryProvenanceSelection {
	if len(observations) == 0 {
		return advisoryProvenanceSelection{}
	}

	sorted := append([]AdvisoryProvenanceObservation(nil), observations...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri := advisorySourcePriority(ecosystem, sorted[i].Source)
		rj := advisorySourcePriority(ecosystem, sorted[j].Source)
		if ri != rj {
			return ri < rj
		}
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		return sorted[i].AdvisoryID < sorted[j].AdvisoryID
	})

	selection := advisoryProvenanceSelection{}
	for _, observation := range sorted {
		if observation.WithdrawnAt != "" {
			continue
		}
		if selection.SeveritySource == "" && observation.SeverityScore > 0 {
			selection.SeveritySource = observation.Source
			selection.SeverityScore = observation.SeverityScore
			selection.SeverityVector = observation.SeverityVector
			selection.SeverityLabel = observation.SeverityLabel
		}
		if selection.FixedVersionSource == "" && len(observation.FixedVersions) > 0 {
			selection.FixedVersionSource = observation.Source
			selection.FixedVersion = strings.TrimSpace(observation.FixedVersions[0])
		}
		if selection.RangeSource == "" && strings.TrimSpace(observation.AffectedRange) != "" {
			selection.RangeSource = observation.Source
			selection.VulnerableRange = strings.TrimSpace(observation.AffectedRange)
		}
	}

	for _, observation := range sorted {
		if observation.WithdrawnAt == "" && observation.Source == selection.SeveritySource {
			continue
		}
		if observation.SeverityScore == 0 && strings.TrimSpace(observation.SeverityVector) == "" && strings.TrimSpace(observation.SeverityLabel) == "" {
			continue
		}
		selection.AlternateSeverities = append(selection.AlternateSeverities, AlternateSeverity{
			Source: observation.Source,
			Score:  observation.SeverityScore,
			Vector: observation.SeverityVector,
			Label:  observation.SeverityLabel,
		})
	}

	branches := make([]FixedVersionBranch, 0)
	seenBranch := make(map[string]struct{})
	for _, observation := range sorted {
		if observation.WithdrawnAt != "" {
			continue
		}
		for _, version := range observation.FixedVersions {
			version = strings.TrimSpace(version)
			if version == "" {
				continue
			}
			key := observation.Source + "|" + version
			if _, ok := seenBranch[key]; ok {
				continue
			}
			seenBranch[key] = struct{}{}
			branches = append(branches, FixedVersionBranch{Version: version, Source: observation.Source})
		}
	}
	if len(branches) > 0 {
		selection.FixedVersionBranches = branches
	}

	advisories := make([]AdvisorySourceObservation, 0, len(sorted))
	seenAdvisory := make(map[string]struct{}, len(sorted))
	for _, observation := range sorted {
		key := observation.Source + "|" + observation.AdvisoryID
		if _, ok := seenAdvisory[key]; ok {
			continue
		}
		seenAdvisory[key] = struct{}{}
		advisories = append(advisories, AdvisorySourceObservation{
			Source:          observation.Source,
			AdvisoryID:      observation.AdvisoryID,
			SourceUpdatedAt: observation.SourceUpdatedAt,
			WithdrawnAt:     observation.WithdrawnAt,
		})
	}
	selection.AdvisorySources = advisories

	for _, observation := range sorted {
		if observation.CVEFactID != "" {
			selection.EvidenceFactIDs = append(selection.EvidenceFactIDs, observation.CVEFactID)
		}
		if observation.AffectedFactID != "" {
			selection.EvidenceFactIDs = append(selection.EvidenceFactIDs, observation.AffectedFactID)
		}
	}
	selection.EvidenceFactIDs = uniqueSortedStrings(selection.EvidenceFactIDs)
	return selection
}
