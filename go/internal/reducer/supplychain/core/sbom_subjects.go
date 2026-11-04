// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

type supplyChainSBOMPath struct {
	component  supplychainmodel.SBOMComponent
	attachment supplychainmodel.Attachment
}

// appendSupplyChainImpactPackageFindings classifies each verified image subject
// independently and leaves unassociated manifest and OS evidence on its own path.
func appendSupplyChainImpactPackageFindings(
	findings []SupplyChainImpactFinding,
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) []SupplyChainImpactFinding {
	pkg := representativeAffectedPackage(pkgs)
	paths := supplyChainSBOMPaths(pkg, index)
	if len(paths) == 0 {
		return appendSupplyChainImpactFleetOSFindings(findings, cves, pkgs, index)
	}
	if len(paths) == 1 && firstConsumption(pkg.PackageID, index.consumption).FactID == "" && !hasMatchingOSPackage(pkg, index) {
		return appendSupplyChainImpactFinding(findings, classifySupplyChainImpactPackage(cves, pkgs, index))
	}
	pathDigests := make(map[string]bool, len(paths))
	for _, path := range paths {
		pathDigests[path.attachment.SubjectDigest] = true
	}
	osByDigest := map[string]map[string][]supplychainmodel.OSPackage{}
	baseOS := map[string][]supplychainmodel.OSPackage{}
	vendorSource := classifyAffectedPackageAdvisorySource(pkg)
	for _, key := range affectedOSPackageLookupKeys(pkg) {
		for _, installed := range index.osPackages[key] {
			if !osPackageMatchesAffectedPackage(installed, pkg, vendorSource) {
				continue
			}
			analysis := index.scannerAnalyses[supplychainmodel.ScopeGenerationKey(installed.ScopeID, installed.GenerationID)]
			if pathDigests[analysis.ImageDigest] && analysis.ImageDigest != "" {
				if osByDigest[analysis.ImageDigest] == nil {
					osByDigest[analysis.ImageDigest] = map[string][]supplychainmodel.OSPackage{}
				}
				osByDigest[analysis.ImageDigest][key] = append(osByDigest[analysis.ImageDigest][key], installed)
			} else {
				baseOS[key] = append(baseOS[key], installed)
			}
		}
	}
	for _, path := range paths {
		scoped := index
		scoped.components = []supplychainmodel.SBOMComponent{path.component}
		scoped.attachments = map[string]supplychainmodel.Attachment{path.component.DocumentID: path.attachment}
		// A package manifest does not identify which image contains its package.
		// Repository coincidence alone cannot establish this association.
		scoped.consumption = nil
		scoped.osPackages = osByDigest[path.attachment.SubjectDigest]
		findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactPackage(cves, pkgs, scoped))
	}
	// Keep independently owned manifest and scan evidence visible. A scanner
	// digest without a matching SBOM is still a valid OS-package path, and these
	// rows split the way the no-SBOM fleet does, so a row here cannot decide the
	// finding for another image or release line by arriving first (#9984).
	base := index
	base.components = nil
	base.osPackages = baseOS
	if len(consumptionRepositoriesForPackage(pkg.PackageID, base.consumption)) > 0 || hasMatchingOSPackage(pkg, base) {
		findings = appendSupplyChainImpactFleetOSFindings(findings, cves, pkgs, base)
	}
	return findings
}

// appendSupplyChainImpactManifestFindings classifies one finding per consuming
// repository instead of one representative consumption per package (issue
// #9725). A single firstConsumption winner silently drops every other
// consumer — every non-lockfile manifest row loses to the preferred lockfile
// row — so each distinct repository classifies over its own rows, with
// firstConsumption still picking the representative row within a repository.
// Repositories sort for deterministic output; the writer and coalesce keys
// already carry repository_id, so per-repo findings persist side by side.
// With no consumption rows the single unscoped classification stands (OS-only
// evidence, or the owned-anchor drop when nothing anchors the package).
func appendSupplyChainImpactManifestFindings(
	findings []SupplyChainImpactFinding,
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) []SupplyChainImpactFinding {
	pkg := representativeAffectedPackage(pkgs)
	repositories := consumptionRepositoriesForPackage(pkg.PackageID, index.consumption)
	if len(repositories) == 0 {
		return appendSupplyChainImpactFinding(findings, classifySupplyChainImpactPackage(cves, pkgs, index))
	}
	for _, repositoryID := range repositories {
		scoped := index
		scoped.consumption = map[string][]supplychainmodel.PackageConsumption{
			pkg.PackageID: consumptionRowsForRepository(index.consumption[pkg.PackageID], repositoryID),
		}
		findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactPackage(cves, pkgs, scoped))
	}
	return findings
}

// appendSupplyChainImpactFleetOSFindings classifies each digest's installed
// rows independently (issue #9858). Without SBOM subjects the fleet-wide pass
// kept the first matching installed row, so a fixed image's row silenced a
// vulnerable image (and vice versa) by input order. Each digest classifies
// over its own scoped index — the osByDigest scoping the SBOM path above —
// through the same manifest fanout, so per-repository composition is
// unchanged: a scope holding a single digest classifies exactly as before.
// Rows no sibling analysis resolves to a digest classify once per (release
// line, installed version) through the same fanout (#9984), since the first of
// them used to decide one finding for every host. With no matching row at all
// the whole index classifies once, which is every non-OS advisory.
func appendSupplyChainImpactFleetOSFindings(
	findings []SupplyChainImpactFinding,
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) []SupplyChainImpactFinding {
	pkg := representativeAffectedPackage(pkgs)
	osByDigest, baseOS := groupOSPackagesByDigest(pkg, index)
	if len(osByDigest) == 0 && len(baseOS) == 0 {
		return appendSupplyChainImpactManifestFindings(findings, cves, pkgs, index)
	}
	digests := make([]string, 0, len(osByDigest))
	for digest := range osByDigest {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	for _, digest := range digests {
		scoped := index
		scoped.osPackages = osByDigest[digest]
		findings = appendSupplyChainImpactManifestFindings(findings, cves, pkgs, scoped)
	}
	for _, shard := range splitOSPackagesByReleaseLine(baseOS) {
		scoped := index
		scoped.osPackages = shard
		findings = appendSupplyChainImpactManifestFindings(findings, cves, pkgs, scoped)
	}
	return findings
}

// groupOSPackagesByDigest partitions the advisory's matching installed rows
// by the image digest their sibling scanner analysis resolves, mirroring the
// osByDigest partition in appendSupplyChainImpactPackageFindings. Rows with
// no digest-bearing sibling stay in base for splitOSPackagesByReleaseLine.
func groupOSPackagesByDigest(
	pkg supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) (map[string]map[string][]supplychainmodel.OSPackage, map[string][]supplychainmodel.OSPackage) {
	byDigest := map[string]map[string][]supplychainmodel.OSPackage{}
	base := map[string][]supplychainmodel.OSPackage{}
	vendorSource := classifyAffectedPackageAdvisorySource(pkg)
	for _, key := range affectedOSPackageLookupKeys(pkg) {
		for _, installed := range index.osPackages[key] {
			if !osPackageMatchesAffectedPackage(installed, pkg, vendorSource) {
				continue
			}
			analysis := index.scannerAnalyses[supplychainmodel.ScopeGenerationKey(installed.ScopeID, installed.GenerationID)]
			if analysis.ImageDigest != "" {
				if byDigest[analysis.ImageDigest] == nil {
					byDigest[analysis.ImageDigest] = map[string][]supplychainmodel.OSPackage{}
				}
				byDigest[analysis.ImageDigest][key] = append(byDigest[analysis.ImageDigest][key], installed)
			} else {
				base[key] = append(base[key], installed)
			}
		}
	}
	return byDigest, base
}

// consumptionRepositoriesForPackage returns the sorted distinct non-blank
// repository IDs with consumption rows for a package. Blank repository rows
// cannot anchor a finding, matching the firstConsumption skip.
func consumptionRepositoriesForPackage(
	packageID string,
	consumption map[string][]supplychainmodel.PackageConsumption,
) []string {
	var repositories []string
	seen := make(map[string]bool)
	for _, row := range consumption[packageID] {
		if strings.TrimSpace(row.RepositoryID) == "" || seen[row.RepositoryID] {
			continue
		}
		seen[row.RepositoryID] = true
		repositories = append(repositories, row.RepositoryID)
	}
	sort.Strings(repositories)
	return repositories
}

// consumptionRowsForRepository returns the rows of one repository in index
// order so firstConsumption keeps its within-repository preference.
func consumptionRowsForRepository(
	rows []supplychainmodel.PackageConsumption,
	repositoryID string,
) []supplychainmodel.PackageConsumption {
	var out []supplychainmodel.PackageConsumption
	for _, row := range rows {
		if row.RepositoryID == repositoryID {
			out = append(out, row)
		}
	}
	return out
}

func hasMatchingOSPackage(pkg supplychainmodel.AffectedPackage, index supplyChainImpactIndex) bool {
	_, ok := firstOSPackageImpactPath(pkg, index)
	return ok
}

func supplyChainSBOMPaths(pkg supplychainmodel.AffectedPackage, index supplyChainImpactIndex) []supplyChainSBOMPath {
	var paths []supplyChainSBOMPath
	for _, component := range index.components {
		if !componentMatchesAffectedPackage(component, pkg) {
			continue
		}
		attachment := index.attachments[component.DocumentID]
		if attachment.SubjectDigest == "" || attachment.Status == "subject_mismatch" || attachment.Status == "unknown_subject" {
			continue
		}
		image := index.images[attachment.SubjectDigest]
		if image.digest == "" || unusableSupplyChainImageIdentityReason(image) != "" {
			continue
		}
		paths = append(paths, supplyChainSBOMPath{component, attachment})
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := paths[i], paths[j]
		if a.attachment.SubjectDigest != b.attachment.SubjectDigest {
			return a.attachment.SubjectDigest < b.attachment.SubjectDigest
		}
		if a.component.Version != b.component.Version {
			return a.component.Version < b.component.Version
		}
		if a.component.PURL != b.component.PURL {
			return a.component.PURL < b.component.PURL
		}
		return a.component.FactID < b.component.FactID
	})
	return paths
}

func coalesceAndSortSupplyChainImpactFindings(findings []SupplyChainImpactFinding) []SupplyChainImpactFinding {
	if len(findings) < 2 {
		return findings
	}
	sort.Slice(findings, func(i, j int) bool {
		left, right := supplyChainImpactStableKey(findings[i]), supplyChainImpactStableKey(findings[j])
		if left != right {
			return left < right
		}
		if findings[i].MatchReason != findings[j].MatchReason {
			return findings[i].MatchReason < findings[j].MatchReason
		}
		return findings[i].ImageRef < findings[j].ImageRef
	})
	out := findings[:0]
	var groupKey string
	for _, finding := range findings {
		key := supplyChainImpactStableKey(finding)
		if len(out) == 0 || key != groupKey {
			out = append(out, finding)
			groupKey = key
			continue
		}
		previous := &out[len(out)-1]
		conflict := previous.MatchReason != finding.MatchReason || previous.Confidence != finding.Confidence
		previous.EvidenceFactIDs = payloadcore.UniqueSortedStrings(append(previous.EvidenceFactIDs, finding.EvidenceFactIDs...))
		previous.EvidencePath = payloadcore.UniqueSortedStrings(append(previous.EvidencePath, finding.EvidencePath...))
		previous.MissingEvidence = payloadcore.UniqueSortedStrings(append(previous.MissingEvidence, finding.MissingEvidence...))
		if conflict {
			previous.Status = SupplyChainImpactPossiblyAffected
			previous.Confidence = "partial"
			previous.MatchReason = supplyChainVersionReasonSBOMVersionConflict
			previous.RuntimeReachability = "image_sbom"
			previous.MissingEvidence = payloadcore.UniqueSortedStrings(append(previous.MissingEvidence, supplyChainMissingSBOMVersionConflict))
			*previous = appendSupplyChainImpactFinding(nil, *previous)[0]
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return supplyChainImpactStableKey(out[i]) < supplyChainImpactStableKey(out[j])
	})
	return out
}

func supplyChainImpactStableKey(finding SupplyChainImpactFinding) string {
	identity, err := json.Marshal(supplyChainImpactLogicalIdentity(finding))
	if err != nil {
		panic(err)
	}
	return string(identity)
}
