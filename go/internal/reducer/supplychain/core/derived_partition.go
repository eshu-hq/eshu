// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// AdvisoryAuthorityDependency identifies the server-owned grant that made an
// advisory generation available to one scan target. Readers must recheck it
// before selecting or returning a persisted finding.
type AdvisoryAuthorityDependency struct {
	Kind                 string
	ScanTargetID         string
	ID                   string
	RevisionID           string
	AuthorityEpoch       int64
	AdvisoryScopeEpoch   int64
	AdvisoryScopeID      string
	AdvisoryGenerationID string
	ConfigFingerprint    string
	ProviderID           string
	TrustedSourceClass   string
	ContentProfile       string
}

// AuthorizedScanImpactEvidence is one complete, loader-authorized target
// snapshot. Advisories are selected by server authority before this classifier
// runs; their payload fields never grant public or target-private access.
type AuthorizedScanImpactEvidence struct {
	ScanTargetID          string
	ScanGenerationID      string
	OSPackages            []facts.Envelope
	Analyses              []facts.Envelope
	Advisories            []facts.Envelope
	AuthorityDependencies []AdvisoryAuthorityDependency
	Complete              bool
}

// BuildAuthorizedScanImpactFindings returns one union of already-classified
// legacy findings and boundary-local scan findings for a single writer pass.
// The caller must authorize and classify legacy facts separately; raw advisory
// envelopes cannot enter this boundary. Incomplete target authority or evidence
// fails the pass rather than retracting or publishing clean status.
func BuildAuthorizedScanImpactFindings(
	legacyFindings []SupplyChainImpactFinding,
	targets []AuthorizedScanImpactEvidence,
) ([]SupplyChainImpactFinding, error) {
	findings, _, err := buildAuthorizedScanImpactFindingsWithQuarantine(legacyFindings, targets)
	return findings, err
}

func buildAuthorizedScanImpactFindingsWithQuarantine(
	legacyFindings []SupplyChainImpactFinding,
	targets []AuthorizedScanImpactEvidence,
) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, error) {
	for _, finding := range legacyFindings {
		if finding.ScanTargetID != "" || finding.ScanGenerationID != "" || len(finding.AuthorityDependencies) != 0 {
			return nil, nil, fmt.Errorf("derived finding passed as classified legacy input")
		}
	}
	seenTargets := make(map[string]struct{}, len(targets))
	authorityClassByScope := make(map[string]string)
	authorityWitnessByScope := make(map[string]AdvisoryAuthorityDependency)
	privateOwnerByScope := make(map[string]string)
	for _, target := range targets {
		if err := validateAuthorizedScanImpactEvidence(target); err != nil {
			return nil, nil, err
		}
		if _, duplicate := seenTargets[target.ScanTargetID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate scan target %q", target.ScanTargetID)
		}
		seenTargets[target.ScanTargetID] = struct{}{}
		for _, dependency := range target.AuthorityDependencies {
			key := dependency.AdvisoryScopeID + "\x00" + dependency.AdvisoryGenerationID
			if prior, exists := authorityWitnessByScope[key]; exists && prior != dependency {
				return nil, nil, fmt.Errorf("advisory generation has conflicting authority revisions")
			}
			authorityWitnessByScope[key] = dependency
			if prior := authorityClassByScope[key]; prior != "" && prior != dependency.Kind {
				return nil, nil, fmt.Errorf("advisory generation has conflicting public and private authority")
			}
			authorityClassByScope[key] = dependency.Kind
			if dependency.Kind == "private_binding" {
				if prior := privateOwnerByScope[key]; prior != "" && prior != target.ScanTargetID {
					return nil, nil, fmt.Errorf("private advisory generation shared across scan targets")
				}
				privateOwnerByScope[key] = target.ScanTargetID
			}
		}
	}
	findings := slices.Clone(legacyFindings)
	var quarantined []factdecode.QuarantinedFact
	if len(targets) == 0 {
		return findings, quarantined, nil
	}
	advisoryIndexes := make(map[string]supplyChainImpactIndex, len(targets))
	for _, target := range targets {
		advisories, err := deduplicateScanImpactEnvelopes(target.Advisories)
		if err != nil {
			return nil, nil, err
		}
		sort.Slice(advisories, func(i, j int) bool {
			if advisories[i].ScopeID != advisories[j].ScopeID {
				return advisories[i].ScopeID < advisories[j].ScopeID
			}
			return advisories[i].FactID < advisories[j].FactID
		})
		cacheKey, err := advisoryIndexKey(advisories)
		if err != nil {
			return nil, nil, err
		}
		shared, exists := advisoryIndexes[cacheKey]
		if !exists {
			var advisoryQuarantined []factdecode.QuarantinedFact
			shared, advisoryQuarantined, err = buildSupplyChainImpactIndexWithQuarantine(advisories)
			if err != nil {
				return nil, nil, err
			}
			if len(advisoryQuarantined) > 0 {
				return nil, append(quarantined, advisoryQuarantined...), fmt.Errorf("authorized advisory %q has invalid %s", advisoryQuarantined[0].FactID, advisoryQuarantined[0].Field)
			}
			advisoryIndexes[cacheKey] = shared
		}
		index := shared
		index.osPackages = make(map[string][]supplychainmodel.OSPackage)
		index.scannerAnalyses = make(map[string]supplychainmodel.ScannerAnalysis)
		index.scannerAnalysisConflicts = nil
		scanFacts := append(slices.Clone(target.OSPackages), target.Analyses...)
		scanFacts, err = deduplicateScanImpactEnvelopes(scanFacts)
		if err != nil {
			return nil, nil, err
		}
		for _, envelope := range scanFacts {
			q, isQuarantine, fatal := addSupplyChainImpactIndexEntry(&index, envelope, nil)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				return nil, append(quarantined, q), fmt.Errorf("authorized scan fact %q has invalid %s", q.FactID, q.Field)
			}
		}
		targetFindings := coalesceAndSortSupplyChainImpactFindings(appendSupplyChainImpactIndexFindings(nil, index))
		markAuthorizedFixedVersionConflicts(targetFindings, advisories)
		for _, finding := range targetFindings {
			if !findingHasAnyScanFact(finding, target.OSPackages) {
				continue
			}
			finding.ScanTargetID = target.ScanTargetID
			finding.ScanGenerationID = target.ScanGenerationID
			finding.AuthorityDependencies = findingAdvisoryDependencies(finding, advisories, target.AuthorityDependencies)
			if len(finding.AuthorityDependencies) == 0 {
				return nil, nil, fmt.Errorf("derived finding for %q has no authorized advisory dependency", target.ScanTargetID)
			}
			findings = append(findings, finding)
		}
	}
	return sortAuthorizedScanImpactFindings(findings), quarantined, nil
}

func validateAuthorizedScanImpactEvidence(target AuthorizedScanImpactEvidence) error {
	const prefix = "image://derived/"
	if !strings.HasPrefix(target.ScanTargetID, prefix) || len(target.ScanTargetID) != len(prefix)+64 {
		return fmt.Errorf("invalid derived scan target %q", target.ScanTargetID)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(target.ScanTargetID, prefix)); err != nil {
		return fmt.Errorf("invalid derived scan target digest: %w", err)
	}
	if target.ScanGenerationID == "" || !target.Complete || len(target.AuthorityDependencies) == 0 {
		return fmt.Errorf("incomplete authorized scan evidence for %q", target.ScanTargetID)
	}
	for _, envelope := range target.OSPackages {
		if envelope.FactKind != chain.VulnerabilityOSPackageFactKind || envelope.ScopeID != target.ScanTargetID || envelope.GenerationID != target.ScanGenerationID {
			return fmt.Errorf("os package outside scan target %q", target.ScanTargetID)
		}
	}
	if len(target.Analyses) == 0 {
		return fmt.Errorf("scan target %q has no completed analysis", target.ScanTargetID)
	}
	for _, envelope := range target.Analyses {
		if envelope.FactKind != facts.ScannerWorkerAnalysisFactKind || envelope.ScopeID != target.ScanTargetID || envelope.GenerationID != target.ScanGenerationID {
			return fmt.Errorf("analysis outside scan target %q", target.ScanTargetID)
		}
		analysis, err := schemadecode.DecodeScannerWorkerAnalysis(envelope)
		if err != nil || analysis.AnalysisStatus != "completed" || analysis.CoverageStatus != "scanned" {
			return fmt.Errorf("scan target %q has incomplete analysis coverage", target.ScanTargetID)
		}
	}
	for _, dependency := range target.AuthorityDependencies {
		if !supportedTrustedAdvisorySourceClass(dependency.TrustedSourceClass) {
			return fmt.Errorf("unsupported trusted advisory source class %q", dependency.TrustedSourceClass)
		}
		if dependency.ContentProfile != "supply_chain_impact_v1" {
			return fmt.Errorf("unsupported advisory content profile %q", dependency.ContentProfile)
		}
		if !validAdvisoryDependency(dependency) ||
			(dependency.Kind == "private_binding" && dependency.ScanTargetID != target.ScanTargetID) {
			return fmt.Errorf("advisory authority outside scan target %q", target.ScanTargetID)
		}
	}
	for _, envelope := range target.Advisories {
		if !authorizedImpactAdvisoryKind(envelope.FactKind) || !slices.ContainsFunc(target.AuthorityDependencies, func(dependency AdvisoryAuthorityDependency) bool {
			return validAdvisoryDependency(dependency) && dependency.AdvisoryScopeID == envelope.ScopeID && dependency.AdvisoryGenerationID == envelope.GenerationID && advisoryMatchesTrustedSource(envelope, dependency)
		}) {
			return fmt.Errorf("advisory %q lacks target authority", envelope.FactID)
		}
	}
	return nil
}

func authorizedImpactAdvisoryKind(kind string) bool {
	switch kind {
	case chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind,
		chain.VulnerabilityAffectedProductFactKind, chain.VulnerabilityEPSSScoreFactKind,
		chain.VulnerabilityKnownExploitedFactKind:
		return true
	default:
		return false
	}
}

func deduplicateScanImpactEnvelopes(envelopes []facts.Envelope) ([]facts.Envelope, error) {
	seen := make(map[string]facts.Envelope, len(envelopes))
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.FactID == "" {
			return nil, fmt.Errorf("authorized evidence has no fact ID")
		}
		if existing, exists := seen[envelope.FactID]; exists {
			if !reflect.DeepEqual(existing, envelope) {
				return nil, fmt.Errorf("conflicting duplicate fact %q", envelope.FactID)
			}
			continue
		}
		seen[envelope.FactID] = envelope
		out = append(out, envelope)
	}
	return out, nil
}

func advisoryIndexKey(envelopes []facts.Envelope) (string, error) {
	digest := sha256.New()
	for _, envelope := range envelopes {
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return "", fmt.Errorf("encode advisory cache key: %w", err)
		}
		_, _ = digest.Write(encoded)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func findingHasAnyScanFact(finding SupplyChainImpactFinding, packages []facts.Envelope) bool {
	for _, envelope := range packages {
		if slices.Contains(finding.EvidenceFactIDs, envelope.FactID) {
			return true
		}
	}
	return false
}

func validAdvisoryDependency(dependency AdvisoryAuthorityDependency) bool {
	if dependency.Kind != "public_registration" && dependency.Kind != "private_binding" {
		return false
	}
	if dependency.Kind == "public_registration" && dependency.ScanTargetID != "" {
		return false
	}
	return dependency.ID != "" && dependency.RevisionID != "" &&
		dependency.AdvisoryScopeID != "" && dependency.AdvisoryGenerationID != "" &&
		dependency.ConfigFingerprint != "" && dependency.ProviderID != "" &&
		dependency.TrustedSourceClass != ""
}

func advisoryMatchesTrustedSource(envelope facts.Envelope, dependency AdvisoryAuthorityDependency) bool {
	sourceClass := strings.ToLower(strings.TrimSpace(dependency.TrustedSourceClass))
	switch envelope.FactKind {
	case chain.VulnerabilityKnownExploitedFactKind:
		// Kind-exact: only the CISA KEV collector emits this kind, so a
		// well-formed fact is cisa_kev by construction. The decode keeps
		// the old fail-closed verdict on malformed payloads.
		if sourceClass != "cisa_kev" {
			return false
		}
		_, err := schemadecode.DecodeVulnerabilityKnownExploited(envelope)
		return err == nil
	case chain.VulnerabilityEPSSScoreFactKind:
		// Kind-exact: only First EPSS emits this kind; see above.
		if sourceClass != "first_epss" {
			return false
		}
		_, err := schemadecode.DecodeVulnerabilityEPSSScore(envelope)
		return err == nil
	default:
		if sourceClass == "cisa_kev" || sourceClass == "first_epss" {
			return false
		}
	}
	source, ok := advisorySourceClass(envelope)
	return ok && strings.EqualFold(strings.TrimSpace(source), sourceClass)
}

// advisorySourceClass reports the trusted source class an advisory fact
// envelope speaks for, without reading its payload map directly. The
// affected_product kind is NVD-only; the kinds shared across sources (cve,
// affected_package) decode through the contracts seam and report their
// declared source field. Anything else reports ok=false so the caller fails
// closed, preserving the old raw-read verdicts without the raw read —
// except malformed shared-kind payloads, which now fail closed instead of
// matching on a present source string.
func advisorySourceClass(envelope facts.Envelope) (string, bool) {
	switch envelope.FactKind {
	case chain.VulnerabilityAffectedProductFactKind:
		if _, err := schemadecode.DecodeVulnerabilityAffectedProduct(envelope); err != nil {
			return "", false
		}
		return "nvd", true
	case chain.VulnerabilityCVEFactKind:
		cve, err := schemadecode.DecodeVulnerabilityCVE(envelope)
		if err != nil || cve.Source == nil {
			return "", false
		}
		return *cve.Source, true
	case chain.VulnerabilityAffectedPackageFactKind:
		pkg, err := schemadecode.DecodeVulnerabilityAffectedPackage(envelope)
		if err != nil || pkg.Source == nil {
			return "", false
		}
		return *pkg.Source, true
	default:
		return "", false
	}
}

// sortAuthorizedScanImpactFindings orders the already-coalesced legacy and
// target-local sets by their logical identity fields. Their identity domains
// cannot intersect: target rows carry ScanTargetID, while legacy rows do not.
func sortAuthorizedScanImpactFindings(findings []SupplyChainImpactFinding) []SupplyChainImpactFinding {
	sort.Slice(findings, func(i, j int) bool {
		a, z := findings[i], findings[j]
		return cmp.Or(
			cmp.Compare(a.AdvisoryID, z.AdvisoryID),
			cmp.Compare(a.CVEID, z.CVEID),
			cmp.Compare(string(a.Status), string(z.Status)),
			cmp.Compare(a.MatchCriteriaID, z.MatchCriteriaID),
			cmp.Compare(a.ObservedVersion, z.ObservedVersion),
			cmp.Compare(a.PackageID, z.PackageID),
			cmp.Compare(a.ProductCriteria, z.ProductCriteria),
			cmp.Compare(a.PURL, z.PURL),
			cmp.Compare(a.RepositoryID, z.RepositoryID),
			cmp.Compare(a.RequestedRange, z.RequestedRange),
			cmp.Compare(a.ScanTargetID, z.ScanTargetID),
			cmp.Compare(a.SubjectDigest, z.SubjectDigest),
		) < 0
	})
	return findings
}

func findingAdvisoryDependencies(
	finding SupplyChainImpactFinding,
	advisories []facts.Envelope,
	dependencies []AdvisoryAuthorityDependency,
) []AdvisoryAuthorityDependency {
	used := make(map[string]struct{})
	for _, envelope := range advisories {
		if slices.Contains(finding.EvidenceFactIDs, envelope.FactID) {
			used[envelope.ScopeID+"\x00"+envelope.GenerationID] = struct{}{}
		}
	}
	out := make([]AdvisoryAuthorityDependency, 0, len(dependencies))
	seen := make(map[AdvisoryAuthorityDependency]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		if _, ok := used[dependency.AdvisoryScopeID+"\x00"+dependency.AdvisoryGenerationID]; !ok {
			continue
		}
		if _, duplicate := seen[dependency]; duplicate {
			continue
		}
		seen[dependency] = struct{}{}
		out = append(out, dependency)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func supportedTrustedAdvisorySourceClass(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	if isOSVendorAdvisorySource(source) {
		return true
	}
	switch source {
	case "nvd", "ghsa", "glad", "osv", "pysec", "osv-go", "rustsec", "osv-malicious", "cisa_kev", "first_epss":
		return true
	default:
		return false
	}
}
