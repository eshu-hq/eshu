// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

// This file holds the typed-contracts-seam extraction functions for the
// vulnerability_intelligence family's identity-critical kinds (cve,
// affected_package, affected_product, os_package). It is split out of
// supply_chain_impact_match.go (the matching/scoring logic that consumes
// these rows) to keep both files under the repo's 500-line cap.

// supplyChainCVEFromEnvelope decodes one vulnerability.cve envelope through
// the contracts seam and projects it into the reducer's internal
// supplychainmodel.ImpactCVE row. A decode error (missing required advisory_id, or
// any other malformed/unsupported-major payload) is returned so the caller
// routes it through partitionDecodeFailures rather than silently producing a
// blank-identity row.
func supplyChainCVEFromEnvelope(envelope facts.Envelope) (supplychainmodel.ImpactCVE, error) {
	cve, err := decodeVulnerabilityCVE(envelope)
	if err != nil {
		return supplychainmodel.ImpactCVE{}, err
	}
	return supplychainmodel.ImpactCVE{
		FactID:          envelope.FactID,
		CVEID:           payloadcore.FirstNonBlank(derefString(cve.CVEID), cve.AdvisoryID),
		AdvisoryID:      cve.AdvisoryID,
		Source:          derefString(cve.Source),
		CVSSScore:       derefFloat64(cve.CVSSScore),
		CVSSVector:      derefString(cve.CVSSVector),
		SeverityLabel:   derefString(cve.SeverityLabel),
		PublishedAt:     derefString(cve.PublishedAt),
		SourceUpdatedAt: derefString(cve.ModifiedAt),
		WithdrawnAt:     derefString(cve.WithdrawnAt),
	}, nil
}

// supplyChainAffectedPackageFromEnvelope decodes one
// vulnerability.affected_package envelope through the contracts seam and
// projects it into the reducer's internal supplychainmodel.AffectedPackage row. A
// decode error (missing required advisory_id, or any other malformed/
// unsupported-major payload) is returned so the caller routes it through
// partitionDecodeFailures rather than silently producing a blank-identity row.
func supplyChainAffectedPackageFromEnvelope(envelope facts.Envelope) (supplychainmodel.AffectedPackage, error) {
	pkg, err := decodeVulnerabilityAffectedPackage(envelope)
	if err != nil {
		return supplychainmodel.AffectedPackage{}, err
	}
	purl := derefString(pkg.PURL)
	return supplychainmodel.AffectedPackage{
		FactID:           envelope.FactID,
		CVEID:            payloadcore.FirstNonBlank(derefString(pkg.CVEID), pkg.AdvisoryID),
		Source:           derefString(pkg.Source),
		AdvisoryID:       pkg.AdvisoryID,
		PackageID:        canonicalSupplyChainAffectedPackageID(derefString(pkg.PackageID), purl),
		Ecosystem:        strings.ToLower(derefString(pkg.Ecosystem)),
		Name:             derefString(pkg.PackageName),
		PURL:             purl,
		AffectedVersions: pkg.AffectedVersions,
		AffectedRanges:   supplyChainAffectedRangesFromTyped(pkg.AffectedRanges),
		AffectedRangeRaw: derefString(pkg.AffectedRangeRaw),
		FixedVersions:    pkg.FixedVersions,
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
// projects it into the reducer's internal supplychainmodel.AffectedProduct row.
// This kind's typed struct declares zero required fields (Criteria and
// MatchCriteriaID are an either-or identity), so the only decode error this
// can return is a genuine malformed-payload or unsupported-major condition,
// never a missing-field input_invalid classification.
func supplyChainAffectedProductFromEnvelope(envelope facts.Envelope) (supplychainmodel.AffectedProduct, error) {
	product, err := decodeVulnerabilityAffectedProduct(envelope)
	if err != nil {
		return supplychainmodel.AffectedProduct{}, err
	}
	return supplychainmodel.AffectedProduct{
		FactID:          envelope.FactID,
		CVEID:           derefString(product.CVEID),
		Criteria:        derefString(product.Criteria),
		MatchCriteriaID: derefString(product.MatchCriteriaID),
		Vulnerable:      product.Vulnerable != nil && *product.Vulnerable,
	}, nil
}

// supplyChainAffectedRangesFromTyped converts the typed
// vulnerabilityv1.AffectedRange slice into the reducer's internal
// supplychainmodel.AffectedRange rows, mirroring the pre-typing
// supplyChainAffectedRangesFromPayload's own drop rules: a range with no type
// or no events is dropped rather than emitted empty.
func supplyChainAffectedRangesFromTyped(ranges []vulnerabilityv1.AffectedRange) []supplychainmodel.AffectedRange {
	if len(ranges) == 0 {
		return nil
	}
	out := make([]supplychainmodel.AffectedRange, 0, len(ranges))
	for _, r := range ranges {
		kind := derefString(r.Type)
		events := supplyChainAffectedRangeEventsFromTyped(r.Events)
		if kind == "" || len(events) == 0 {
			continue
		}
		out = append(out, supplychainmodel.AffectedRange{Kind: kind, Events: events})
	}
	return out
}

// supplyChainAffectedRangeEventsFromTyped converts the typed
// vulnerabilityv1.AffectedRangeEvent slice into the reducer's internal
// supplychainmodel.AffectedRangeEvent rows.
func supplyChainAffectedRangeEventsFromTyped(events []vulnerabilityv1.AffectedRangeEvent) []supplychainmodel.AffectedRangeEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]supplychainmodel.AffectedRangeEvent, 0, len(events))
	for _, e := range events {
		out = append(out, supplychainmodel.AffectedRangeEvent{
			Introduced:   derefString(e.Introduced),
			Fixed:        derefString(e.Fixed),
			LastAffected: derefString(e.LastAffected),
			Limit:        derefString(e.Limit),
		})
	}
	return out
}

// supplyChainOSPackageFromEnvelope decodes one vulnerability.os_package
// envelope through the contracts seam and projects it into the reducer's
// internal supplychainmodel.OSPackage row. A decode error (missing a required
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
func supplyChainOSPackageFromEnvelope(envelope facts.Envelope) (supplychainmodel.OSPackage, error) {
	pkg, err := decodeVulnerabilityOSPackage(envelope)
	if err != nil {
		return supplychainmodel.OSPackage{}, err
	}
	purl := derefString(pkg.PURL)
	return supplychainmodel.OSPackage{
		FactID:               envelope.FactID,
		ScopeID:              envelope.ScopeID,
		GenerationID:         envelope.GenerationID,
		PackageID:            packageIDFromPURL(purl),
		PURL:                 purl,
		Distro:               strings.ToLower(pkg.Distro),
		DistroVersion:        pkg.DistroVersion,
		PackageManager:       strings.ToLower(pkg.PackageManager),
		Name:                 pkg.Name,
		Arch:                 pkg.Arch,
		InstalledVersion:     pkg.InstalledVersion,
		RepositoryClass:      strings.ToLower(derefString(pkg.RepositoryClass)),
		VendorAdvisorySource: strings.ToLower(derefString(pkg.VendorAdvisorySource)),
	}, nil
}

// supplyChainScannerAnalysisFromEnvelope decodes one scanner_worker.analysis
// envelope through the contracts seam and projects it into the reducer's
// internal supplychainmodel.ScannerAnalysis row: the sibling fact
// classifySupplyChainImpactPackage joins an os_package to (by
// ScopeID+GenerationID) so the finding's SubjectDigest anchors on the
// analyzer-observed ImageDigest instead of the os_package's own opaque
// ScopeID. A decode error (missing a required field such as analyzer,
// target_kind, image_reference, or image_digest, or any other malformed/
// unsupported-major payload) is returned so the caller routes it through
// partitionDecodeFailures rather than silently producing a blank-identity row.
func supplyChainScannerAnalysisFromEnvelope(envelope facts.Envelope) (supplychainmodel.ScannerAnalysis, error) {
	analysis, err := decodeScannerWorkerAnalysis(envelope)
	if err != nil {
		return supplychainmodel.ScannerAnalysis{}, err
	}
	return supplychainmodel.ScannerAnalysis{
		FactID:         envelope.FactID,
		ScopeID:        envelope.ScopeID,
		GenerationID:   envelope.GenerationID,
		ImageDigest:    strings.TrimSpace(analysis.ImageDigest),
		ImageReference: strings.TrimSpace(analysis.ImageReference),
	}, nil
}
