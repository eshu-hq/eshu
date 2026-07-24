// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

// allowedEvidenceFamilies is the closed set of family identifiers the
// envelope is allowed to surface. Anything emitted by the readiness store
// outside this set is dropped to prevent silent contract drift between the
// SQL family literals and the Go classifier.
var allowedEvidenceFamilies = map[string]struct{}{
	EvidenceFamilyVulnerabilityAdvisory:       {},
	EvidenceFamilyVulnerabilityExploitability: {},
	EvidenceFamilyPackageConsumption:          {},
	EvidenceFamilyPackageRegistry:             {},
	EvidenceFamilySBOMComponent:               {},
	EvidenceFamilySBOMAttestation:             {},
	EvidenceFamilyContainerImageIdentity:      {},
	EvidenceFamilyVulnerabilityOSPackage:      {},
	EvidenceFamilyScannerWorkerAnalysis:       {},
}

func normalizeEvidenceSources(sources []SupplyChainImpactEvidenceFamily) []SupplyChainImpactEvidenceFamily {
	if len(sources) == 0 {
		return []SupplyChainImpactEvidenceFamily{}
	}
	cloned := make([]SupplyChainImpactEvidenceFamily, 0, len(sources))
	for _, family := range sources {
		name := strings.TrimSpace(family.Family)
		if name == "" {
			continue
		}
		if _, ok := allowedEvidenceFamilies[name]; !ok {
			continue
		}
		cloned = append(cloned, family)
	}
	sort.SliceStable(cloned, func(i, j int) bool {
		return cloned[i].Family < cloned[j].Family
	})
	return cloned
}

func normalizeSourceSnapshots(snapshots []SupplyChainImpactSourceSnapshot) []SupplyChainImpactSourceSnapshot {
	if len(snapshots) == 0 {
		return nil
	}
	out := make([]SupplyChainImpactSourceSnapshot, 0, len(snapshots))
	seen := map[string]struct{}{}
	for _, snapshot := range snapshots {
		snapshot.Source = strings.TrimSpace(snapshot.Source)
		if snapshot.Source == "" {
			continue
		}
		snapshot.Ecosystem = strings.TrimSpace(snapshot.Ecosystem)
		snapshot.CacheArtifactVersion = strings.TrimSpace(snapshot.CacheArtifactVersion)
		snapshot.SnapshotDigest = strings.TrimSpace(snapshot.SnapshotDigest)
		snapshot.LastUpdatedAt = strings.TrimSpace(snapshot.LastUpdatedAt)
		snapshot.Freshness = strings.TrimSpace(snapshot.Freshness)
		snapshot.WarningCode = strings.TrimSpace(snapshot.WarningCode)
		snapshot.WarningMessage = strings.TrimSpace(snapshot.WarningMessage)
		key := snapshot.Source + "\x00" + snapshot.Ecosystem + "\x00" + snapshot.SnapshotDigest
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, snapshot)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		return out[i].SnapshotDigest < out[j].SnapshotDigest
	})
	return out
}

func uniqueSortedReadinessStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	sort.Strings(unique)
	return unique
}

func readinessMissingContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
