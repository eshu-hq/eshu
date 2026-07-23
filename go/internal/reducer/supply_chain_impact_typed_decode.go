// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

// This file holds the typed-contracts-seam extraction functions for the
// vulnerability_intelligence family's identity-critical kinds (cve,
// affected_package, affected_product, os_package). It is split out of
// supply_chain_impact_match.go (the matching/scoring logic that consumes
// these rows) to keep both files under the repo's 500-line cap.

// supplyChainCVEFromEnvelope decodes one vulnerability.cve envelope through
// the contracts seam and projects it into the reducer's internal
// supplyChainImpactCVE row. A decode error (missing required advisory_id, or
// any other malformed/unsupported-major payload) is returned so the caller
// routes it through partitionDecodeFailures rather than silently producing a
// blank-identity row.
func supplyChainCVEFromEnvelope(envelope facts.Envelope) (supplyChainImpactCVE, error) {
	cve, err := decodeVulnerabilityCVE(envelope)
	if err != nil {
		return supplyChainImpactCVE{}, err
	}
	return supplyChainImpactCVE{
		factID:          envelope.FactID,
		cveID:           firstNonBlank(derefString(cve.CVEID), cve.AdvisoryID),
		advisoryID:      cve.AdvisoryID,
		source:          derefString(cve.Source),
		cvssScore:       derefFloat64(cve.CVSSScore),
		cvssVector:      derefString(cve.CVSSVector),
		severityLabel:   derefString(cve.SeverityLabel),
		publishedAt:     derefString(cve.PublishedAt),
		sourceUpdatedAt: derefString(cve.ModifiedAt),
		withdrawnAt:     derefString(cve.WithdrawnAt),
	}, nil
}

// supplyChainAffectedPackageFromEnvelope decodes one
// vulnerability.affected_package envelope through the contracts seam and
// projects it into the reducer's internal supplyChainAffectedPackage row. A
// decode error (missing required advisory_id, or any other malformed/
// unsupported-major payload) is returned so the caller routes it through
// partitionDecodeFailures rather than silently producing a blank-identity row.
func supplyChainAffectedPackageFromEnvelope(envelope facts.Envelope) (supplyChainAffectedPackage, error) {
	pkg, err := decodeVulnerabilityAffectedPackage(envelope)
	if err != nil {
		return supplyChainAffectedPackage{}, err
	}
	purl := derefString(pkg.PURL)
	return supplyChainAffectedPackage{
		factID:           envelope.FactID,
		cveID:            firstNonBlank(derefString(pkg.CVEID), pkg.AdvisoryID),
		source:           derefString(pkg.Source),
		advisoryID:       pkg.AdvisoryID,
		packageID:        canonicalSupplyChainAffectedPackageID(derefString(pkg.PackageID), purl),
		ecosystem:        strings.ToLower(derefString(pkg.Ecosystem)),
		name:             derefString(pkg.PackageName),
		purl:             purl,
		affectedVersions: pkg.AffectedVersions,
		affectedRanges:   supplyChainAffectedRangesFromTyped(pkg.AffectedRanges),
		affectedRangeRaw: derefString(pkg.AffectedRangeRaw),
		fixedVersions:    pkg.FixedVersions,
	}, nil
}

// canonicalSupplyChainAffectedPackageID prefers a source-reported PackageID,
// falling back to a value derived from purl when the source omitted it.
func canonicalSupplyChainAffectedPackageID(packageID string, purl string) string {
	if packageID != "" {
		return packageID
	}
	derived, err := packageidentity.PackageIDFromPURL(purl)
	if err != nil {
		return ""
	}
	return derived
}

// supplyChainAffectedProductFromEnvelope decodes one
// vulnerability.affected_product envelope through the contracts seam and
// projects it into the reducer's internal supplyChainAffectedProduct row.
// This kind's typed struct declares zero required fields (Criteria and
// MatchCriteriaID are an either-or identity), so the only decode error this
// can return is a genuine malformed-payload or unsupported-major condition,
// never a missing-field input_invalid classification.
func supplyChainAffectedProductFromEnvelope(envelope facts.Envelope) (supplyChainAffectedProduct, error) {
	product, err := decodeVulnerabilityAffectedProduct(envelope)
	if err != nil {
		return supplyChainAffectedProduct{}, err
	}
	return supplyChainAffectedProduct{
		factID:          envelope.FactID,
		cveID:           derefString(product.CVEID),
		criteria:        derefString(product.Criteria),
		matchCriteriaID: derefString(product.MatchCriteriaID),
		vulnerable:      product.Vulnerable != nil && *product.Vulnerable,
	}, nil
}

// supplyChainAffectedRangesFromTyped converts the typed
// vulnerabilityv1.AffectedRange slice into the reducer's internal
// supplyChainAffectedRange rows, mirroring the pre-typing
// supplyChainAffectedRangesFromPayload's own drop rules: a range with no type
// or no events is dropped rather than emitted empty.
func supplyChainAffectedRangesFromTyped(ranges []vulnerabilityv1.AffectedRange) []supplyChainAffectedRange {
	if len(ranges) == 0 {
		return nil
	}
	out := make([]supplyChainAffectedRange, 0, len(ranges))
	for _, r := range ranges {
		kind := derefString(r.Type)
		events := supplyChainAffectedRangeEventsFromTyped(r.Events)
		if kind == "" || len(events) == 0 {
			continue
		}
		out = append(out, supplyChainAffectedRange{kind: kind, events: events})
	}
	return out
}

// supplyChainAffectedRangeEventsFromTyped converts the typed
// vulnerabilityv1.AffectedRangeEvent slice into the reducer's internal
// supplyChainAffectedRangeEvent rows.
func supplyChainAffectedRangeEventsFromTyped(events []vulnerabilityv1.AffectedRangeEvent) []supplyChainAffectedRangeEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]supplyChainAffectedRangeEvent, 0, len(events))
	for _, e := range events {
		out = append(out, supplyChainAffectedRangeEvent{
			introduced:   derefString(e.Introduced),
			fixed:        derefString(e.Fixed),
			lastAffected: derefString(e.LastAffected),
			limit:        derefString(e.Limit),
		})
	}
	return out
}

// supplyChainOSPackageFromEnvelope decodes one vulnerability.os_package
// envelope through the contracts seam and projects it into the reducer's
// internal supplyChainOSPackage row. A decode error (missing a required
// identity field: distro, distro_version, package_manager, name, arch,
// installed_version_raw) is returned so the caller routes it through
// partitionDecodeFailures rather than silently producing a row keyed on a
// blank identity segment.
//
// InstalledVersion decodes verbatim from installed_version_raw — reducers
// MUST NOT compare it against an upstream advisory's fixed version; impact is
// decided by RepositoryClass=="vendor" plus a VendorAdvisorySource string
// match (osPackageMatchesAffectedPackage, supply_chain_impact_match.go), never
// by version comparison. RepositoryClass and VendorAdvisorySource are optional
// on the typed struct, so a present-but-empty value here (a legitimate "no
// vendor evidence" observation) decodes to "" exactly as the pre-typing
// payloadStr lookup did.
func supplyChainOSPackageFromEnvelope(envelope facts.Envelope) (supplyChainOSPackage, error) {
	pkg, err := decodeVulnerabilityOSPackage(envelope)
	if err != nil {
		return supplyChainOSPackage{}, err
	}
	purl := derefString(pkg.PURL)
	return supplyChainOSPackage{
		factID:               envelope.FactID,
		scopeID:              envelope.ScopeID,
		generationID:         envelope.GenerationID,
		packageID:            packageIDFromPURL(purl),
		purl:                 purl,
		distro:               strings.ToLower(pkg.Distro),
		distroVersion:        pkg.DistroVersion,
		packageManager:       strings.ToLower(pkg.PackageManager),
		name:                 pkg.Name,
		arch:                 pkg.Arch,
		installedVersion:     pkg.InstalledVersion,
		repositoryClass:      strings.ToLower(derefString(pkg.RepositoryClass)),
		vendorAdvisorySource: strings.ToLower(derefString(pkg.VendorAdvisorySource)),
	}, nil
}

// supplyChainScannerAnalysisFromEnvelope decodes one scanner_worker.analysis
// envelope through the contracts seam and projects it into the reducer's
// internal supplyChainScannerAnalysis row: the sibling fact
// classifySupplyChainImpactPackage joins an os_package to (by
// ScopeID+GenerationID) so the finding's SubjectDigest anchors on the
// analyzer-observed ImageDigest instead of the os_package's own opaque
// ScopeID. A decode error (missing a required field such as analyzer,
// target_kind, image_reference, or image_digest, or any other malformed/
// unsupported-major payload) is returned so the caller routes it through
// partitionDecodeFailures rather than silently producing a blank-identity row.
func supplyChainScannerAnalysisFromEnvelope(envelope facts.Envelope) (supplyChainScannerAnalysis, error) {
	analysis, err := decodeScannerWorkerAnalysis(envelope)
	if err != nil {
		return supplyChainScannerAnalysis{}, err
	}
	return supplyChainScannerAnalysis{
		factID:         envelope.FactID,
		scopeID:        envelope.ScopeID,
		generationID:   envelope.GenerationID,
		imageDigest:    strings.TrimSpace(analysis.ImageDigest),
		imageReference: strings.TrimSpace(analysis.ImageReference),
	}, nil
}
