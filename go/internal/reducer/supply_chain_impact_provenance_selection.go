// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
)

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
	if ecosystem == "os" && isOSVendorAdvisorySource(source) {
		return 1
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

func isOSVendorAdvisorySource(source string) bool {
	switch source {
	case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux",
		"debian", "ubuntu", "alpine", "suse", "wolfi", "chainguard", "oracle":
		return true
	default:
		return false
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
