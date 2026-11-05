// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
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
	cves, pkgs = liveAdvisoryPackages(cves, pkgs, index)
	pkg := representativeAffectedPackage(pkgs)
	paths := supplyChainSBOMPaths(pkg, index)
	reselected := false
	if len(paths) == 0 {
		// No SBOM path: let a fact that joins the installed row name the finding
		// when source order picked one with no anchor at all (#9990).
		if joining, ok := withJoinableOSRepresentative(pkgs, pkg, index); ok {
			pkgs, reselected = joining, true
			pkg = representativeAffectedPackage(pkgs)
			paths = supplyChainSBOMPaths(pkg, index)
		}
	}
	index.recordOSRepresentativeOutcome(pkgs, pkg, reselected, len(paths) > 0)
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
// over its own scoped index, the osByDigest scoping of the SBOM path above,
// through the same manifest fanout, so per-repository composition is
// unchanged.
//
// A digest classifies once per (release line, installed version) it holds
// (#10057). The first row used to decide the digest's one finding, so a digest
// holding a version below the fix could read known fixed by row order. A digest
// that holds one version on one release line still yields one finding.
//
// Rows no sibling analysis resolves to a digest classify the same way (#9984):
// the first of them used to decide one finding for every host. With no
// matching row at all the whole index classifies once. That covers every
// non-OS advisory and an OS advisory that joins no installed row.
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
		// One digest can hold more than one installed version under a package
		// key, and the classifier reads one row per index, so the first row
		// would decide the digest's only finding (#10057). Each (release line,
		// version) shard classifies on its own, as the digest-less rows do.
		for _, shard := range splitOSPackagesByReleaseLine(osByDigest[digest]) {
			scoped := index
			scoped.osPackages = shard
			findings = appendSupplyChainImpactManifestFindings(findings, cves, pkgs, scoped)
		}
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
	keys := sortSupplyChainImpactFindings(findings, true)
	out := findings[:0]
	var groupKey string
	for i, finding := range findings {
		key := keys[i]
		if len(out) == 0 || key != groupKey {
			out = append(out, finding)
			groupKey = key
			continue
		}
		previous := &out[len(out)-1]
		conflict := previous.MatchReason != finding.MatchReason || previous.Confidence != finding.Confidence
		// Two findings share a writer identity without sharing an SBOM when the
		// identity cannot tell digest-less release lines apart (a purl with no
		// distro qualifier, #9984). Their disagreement is not a conflict between
		// SBOM components, so it must not wear that label.
		fromSBOM := findingHasSBOMComponentPath(*previous) || findingHasSBOMComponentPath(finding)
		previous.EvidenceFactIDs = payloadcore.UniqueSortedStrings(append(previous.EvidenceFactIDs, finding.EvidenceFactIDs...))
		previous.EvidencePath = payloadcore.UniqueSortedStrings(append(previous.EvidencePath, finding.EvidencePath...))
		previous.MissingEvidence = payloadcore.UniqueSortedStrings(append(previous.MissingEvidence, finding.MissingEvidence...))
		if conflict && !fromSBOM {
			// The sort keeps the lesser reason in previous, so the reason does not
			// depend on arrival order. Differing confidence settles on the weaker.
			if previous.Confidence != finding.Confidence {
				previous.Confidence = "partial"
			}
			*previous = appendSupplyChainImpactFinding(nil, *previous)[0]
		} else if conflict {
			previous.Status = SupplyChainImpactPossiblyAffected
			previous.Confidence = "partial"
			previous.MatchReason = supplyChainVersionReasonSBOMVersionConflict
			previous.RuntimeReachability = "image_sbom"
			previous.MissingEvidence = payloadcore.UniqueSortedStrings(append(previous.MissingEvidence, supplyChainMissingSBOMVersionConflict))
			*previous = appendSupplyChainImpactFinding(nil, *previous)[0]
		}
	}
	sortSupplyChainImpactFindings(out, false)
	return out
}

// keyedSupplyChainImpactFindings sorts findings by a stable key computed once
// per finding. The key is a JSON marshal of the logical identity, so computing
// it inside a comparator cost two marshals per comparison and grew with
// n log n; a digest holding several versions multiplies n (#10057).
type keyedSupplyChainImpactFindings struct {
	findings []SupplyChainImpactFinding
	keys     []string
	tieBreak bool
}

func (k keyedSupplyChainImpactFindings) Len() int { return len(k.findings) }

func (k keyedSupplyChainImpactFindings) Swap(i, j int) {
	k.findings[i], k.findings[j] = k.findings[j], k.findings[i]
	k.keys[i], k.keys[j] = k.keys[j], k.keys[i]
}

func (k keyedSupplyChainImpactFindings) Less(i, j int) bool {
	if k.keys[i] != k.keys[j] {
		return k.keys[i] < k.keys[j]
	}
	if !k.tieBreak {
		return false
	}
	if k.findings[i].MatchReason != k.findings[j].MatchReason {
		return k.findings[i].MatchReason < k.findings[j].MatchReason
	}
	return k.findings[i].ImageRef < k.findings[j].ImageRef
}

// sortSupplyChainImpactFindings orders findings by stable key, then, when
// tieBreak is set, by match reason and image ref. It marshals each key once and
// swaps keys with their findings, so the order is identical to comparing
// supplyChainImpactStableKey directly, ties included: sort.Sort and sort.Slice
// run the same pdqsort over the same sequence of comparisons. It returns the
// keys in sorted order, aligned index for index with findings.
func sortSupplyChainImpactFindings(findings []SupplyChainImpactFinding, tieBreak bool) []string {
	keys := make([]string, len(findings))
	for i := range findings {
		keys[i] = supplyChainImpactStableKey(findings[i])
	}
	sort.Sort(keyedSupplyChainImpactFindings{findings: findings, keys: keys, tieBreak: tieBreak})
	return keys
}

// findingHasSBOMComponentPath reports whether classification read an SBOM
// component for the finding, which is the only evidence the SBOM-conflict label
// speaks about.
func findingHasSBOMComponentPath(finding SupplyChainImpactFinding) bool {
	return slices.Contains(finding.EvidencePath, chain.SBOMComponentFactKind)
}

func supplyChainImpactStableKey(finding SupplyChainImpactFinding) string {
	identity, err := json.Marshal(supplyChainImpactLogicalIdentity(finding))
	if err != nil {
		panic(err)
	}
	return string(identity)
}
