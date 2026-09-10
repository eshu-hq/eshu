// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// The typed-contracts-seam extraction functions for vulnerability.cve,
// .affected_package, .affected_product, and .os_package
// (supplyChainCVEFromEnvelope, supplyChainAffectedPackageFromEnvelope,
// canonicalSupplyChainAffectedPackageID, supplyChainAffectedProductFromEnvelope,
// supplyChainAffectedRangesFromTyped, supplyChainAffectedRangeEventsFromTyped,
// supplyChainOSPackageFromEnvelope) live in supply_chain_impact_typed_decode.go
// (split out to keep this file under the repo's 500-line cap).

func supplyChainConsumptionFromEnvelope(envelope facts.Envelope) (supplychainmodel.PackageConsumption, error) {
	consumption, err := schemadecode.DecodeReducerPackageConsumptionCorrelation(envelope)
	if err != nil {
		return supplychainmodel.PackageConsumption{}, err
	}
	return supplychainmodel.PackageConsumption{
		FactID:                    envelope.FactID,
		EvidenceKind:              correlation.PackageConsumptionFactKind,
		PackageID:                 strings.TrimSpace(consumption.PackageID),
		RepositoryID:              strings.TrimSpace(payloadcore.DerefString(consumption.RepositoryID)),
		DependencyRange:           strings.TrimSpace(payloadcore.DerefString(consumption.DependencyRange)),
		ObservedVersion:           payloadcore.FirstNonBlank(payloadcore.DerefString(consumption.ObservedVersion), payloadcore.DerefString(consumption.ResolvedVersion)),
		RequestedRange:            strings.TrimSpace(payloadcore.DerefString(consumption.RequestedRange)),
		InstalledVersion:          strings.TrimSpace(payloadcore.DerefString(consumption.InstalledVersion)),
		DependencyPath:            orderedStrings(consumption.DependencyPath),
		DependencyDepth:           payloadcore.DerefInt(consumption.DependencyDepth),
		DirectDependency:          consumption.DirectDependency,
		DependencyScope:           supplyChainDependencyScopeFromCorrelation(consumption.DependencyScope, consumption.ManifestSection),
		VersionEvidence:           strings.TrimSpace(payloadcore.DerefString(consumption.VersionEvidence)),
		UnresolvedMSBuildProperty: strings.TrimSpace(payloadcore.DerefString(consumption.UnresolvedMSBuildProperty)),
		AmbiguousMSBuildProperty:  strings.TrimSpace(payloadcore.DerefString(consumption.AmbiguousMSBuildProperty)),
		PartialEvidence:           payloadcore.DerefBool(consumption.PartialEvidence),
		Lockfile:                  payloadcore.DerefBool(consumption.Lockfile),
	}, nil
}

// supplyChainSBOMComponentFromEnvelope reads sbom.component raw
// (payloadcore.PayloadStr), NOT through the sdk/go/factschema typed decode seam
// (schemadecode.DecodeSBOMComponent, used by the sbom_attestation family in
// internal/reducer/sbomattest): this is the supply_chain_impact domain's own
// read of the sbom_attestation family's wire kind, and that domain carries
// zero existing quarantine plumbing
// across ANY of its many vulnerability/OS-package/deployment-context kinds
// today. Converting only this one field-read in isolation would be a hollow,
// half-typed contract rather than a real accuracy fix; it is deferred to the
// supply_chain_impact family's own future migration (Contract System v1
// #4566), matching how the GCP wave deferred its shared cross-provider
// consumers to their own conversion. The WIRED sbom.component consumer that
// IS typed this wave is sbom_attestation_attachment_index.go's
// buildSBOMAttachmentIndex.
func supplyChainSBOMComponentFromEnvelope(envelope facts.Envelope) supplychainmodel.SBOMComponent {
	purl := payloadcore.PayloadStr(envelope.Payload, "purl")
	return supplychainmodel.SBOMComponent{
		FactID:     envelope.FactID,
		DocumentID: payloadcore.PayloadStr(envelope.Payload, "document_id"),
		PURL:       purl,
		CPE:        payloadcore.PayloadStr(envelope.Payload, "cpe"),
		// Prefer the canonical package_id the collector now emits so the
		// component joins vulnerability facts on the same identity every
		// other package fact uses; fall back to the version-stripped purl for
		// components ingested before the collector carried package_id.
		PackageID: payloadcore.FirstNonBlank(payloadcore.PayloadStr(envelope.Payload, "package_id"), packageIDFromPURL(purl)),
		Version:   payloadcore.FirstNonBlank(payloadcore.PayloadStr(envelope.Payload, "version"), versionFromPURL(purl)),
	}
}

func supplyChainAttachmentFromEnvelope(envelope facts.Envelope) supplychainmodel.Attachment {
	return supplychainmodel.Attachment{
		FactID:        envelope.FactID,
		DocumentID:    payloadcore.PayloadStr(envelope.Payload, "document_id"),
		SubjectDigest: payloadcore.PayloadStr(envelope.Payload, "subject_digest"),
		Status:        payloadcore.PayloadStr(envelope.Payload, "attachment_status"),
	}
}

// supplyChainImageIdentityFromEnvelope, singleSupplyChainImageSourceRepositoryID,
// and singleSupplyChainRepositoryID live in supply_chain_impact_anchor_tier.go
// (split out to keep this file under the repo's 500-line cap).

func supplyChainWorkloadContextsFromEnvelope(envelope facts.Envelope) []supplychainmodel.WorkloadContext {
	repositoryID := supplyChainWorkloadRepositoryID(envelope)
	workloadIDs := supplyChainWorkloadIDsFromPayload(envelope.Payload)
	if repositoryID == "" || len(workloadIDs) == 0 {
		return nil
	}
	out := make([]supplychainmodel.WorkloadContext, 0, len(workloadIDs))
	for _, workloadID := range workloadIDs {
		out = append(out, supplychainmodel.WorkloadContext{
			FactID:       envelope.FactID,
			RepositoryID: repositoryID,
			WorkloadID:   workloadID,
		})
	}
	return out
}

func supplyChainWorkloadRepositoryID(envelope facts.Envelope) string {
	direct := payloadcore.FirstNonBlank(
		payloadcore.PayloadStr(envelope.Payload, "repository_id"),
		payloadcore.PayloadStr(envelope.Payload, "repo_id"),
	)
	if direct != "" {
		return direct
	}
	scoped := payloadcore.FirstNonBlank(
		payloadcore.PayloadStr(envelope.Payload, "scope_id"),
		envelope.ScopeID,
	)
	if repositoryID := repositoryIDFromReducerScope(scoped); repositoryID != "" {
		return repositoryID
	}
	for _, scopeID := range payloadcore.PayloadOrderedStrings(envelope.Payload, "related_scope_ids") {
		if repositoryID := repositoryIDFromReducerScope(scopeID); repositoryID != "" {
			return repositoryID
		}
	}
	return strings.TrimSpace(scoped)
}

// repositoryIDFromReducerScope forwards to [payloadcore.RepositoryIDFromReducerScope].
func repositoryIDFromReducerScope(scopeID string) string {
	return payloadcore.RepositoryIDFromReducerScope(scopeID)
}

// supplyChainWorkloadIDsFromPayload forwards to [payloadcore.SupplyChainWorkloadIDsFromPayload].
func supplyChainWorkloadIDsFromPayload(payload map[string]any) []string {
	return payloadcore.SupplyChainWorkloadIDsFromPayload(payload)
}

func supplyChainServiceContextFromEnvelope(envelope facts.Envelope) supplychainmodel.ServiceContext {
	return supplychainmodel.ServiceContext{
		FactID:         envelope.FactID,
		RepositoryID:   supplyChainServiceRepositoryID(envelope),
		ServiceID:      payloadcore.PayloadStr(envelope.Payload, "service_id"),
		WorkloadID:     payloadcore.PayloadStr(envelope.Payload, "workload_id"),
		EntityRef:      payloadcore.PayloadStr(envelope.Payload, "entity_ref"),
		OwnerRef:       payloadcore.PayloadStr(envelope.Payload, "owner_ref"),
		Outcome:        payloadcore.PayloadStr(envelope.Payload, "outcome"),
		DriftStatus:    payloadcore.PayloadStr(envelope.Payload, "drift_status"),
		ProvenanceOnly: payloadBool(envelope.Payload, "provenance_only"),
	}
}

func supplyChainServiceRepositoryID(envelope facts.Envelope) string {
	if repositoryID := payloadcore.FirstNonBlank(
		payloadcore.PayloadStr(envelope.Payload, "repository_id"),
		payloadcore.PayloadStr(envelope.Payload, "repo_id"),
	); repositoryID != "" {
		return repositoryID
	}
	return supplyChainWorkloadRepositoryID(envelope)
}

func supplyChainDependencyScopeFromCorrelation(dependencyScope, manifestSection *string) string {
	if scope := strings.TrimSpace(payloadcore.DerefString(dependencyScope)); scope != "" {
		return scope
	}
	return strings.TrimSpace(payloadcore.DerefString(manifestSection))
}

func firstConsumption(
	packageID string,
	consumption map[string][]supplychainmodel.PackageConsumption,
) supplychainmodel.PackageConsumption {
	var fallback supplychainmodel.PackageConsumption
	for _, row := range consumption[packageID] {
		if row.RepositoryID == "" {
			continue
		}
		if strings.TrimSpace(row.InstalledVersion) != "" || row.Lockfile {
			return row
		}
		if fallback.RepositoryID == "" {
			fallback = row
		}
	}
	return fallback
}

func firstSBOMImpactPath(
	pkg supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) (supplychainmodel.SBOMComponent, supplychainmodel.Attachment, supplyChainImageIdentity, bool, []string) {
	var missing []string
	for _, component := range index.components {
		if !componentMatchesAffectedPackage(component, pkg) {
			continue
		}
		attachment := index.attachments[component.DocumentID]
		if attachment.SubjectDigest == "" || attachment.Status == "subject_mismatch" || attachment.Status == "unknown_subject" {
			continue
		}
		image := index.images[attachment.SubjectDigest]
		if image.digest == "" {
			missing = append(missing, "image identity evidence missing")
			continue
		}
		if reason := unusableSupplyChainImageIdentityReason(image); reason != "" {
			missing = append(missing, reason)
			continue
		}
		return component, attachment, image, true, nil
	}
	return supplychainmodel.SBOMComponent{}, supplychainmodel.Attachment{}, supplyChainImageIdentity{}, false, payloadcore.UniqueSortedStrings(missing)
}

func firstOSPackageImpactPath(
	pkg supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) (supplychainmodel.OSPackage, bool) {
	vendorSource := classifyAffectedPackageAdvisorySource(pkg)
	if vendorSource == "" {
		return supplychainmodel.OSPackage{}, false
	}
	for _, key := range affectedOSPackageLookupKeys(pkg) {
		for _, installed := range index.osPackages[key] {
			if !osPackageMatchesAffectedPackage(installed, pkg, vendorSource) {
				continue
			}
			return installed, true
		}
	}
	return supplychainmodel.OSPackage{}, false
}

func osPackageMatchesAffectedPackage(
	installed supplychainmodel.OSPackage,
	pkg supplychainmodel.AffectedPackage,
	vendorSource string,
) bool {
	if !supportedOSPackageImpactManager(installed.PackageManager) ||
		installed.DistroVersion == "" || installed.Arch == "" {
		return false
	}
	if installed.RepositoryClass != "vendor" || installed.VendorAdvisorySource == "" {
		return false
	}
	if installed.VendorAdvisorySource != vendorSource {
		return false
	}
	if !osPackageEcosystemMatchesVendor(pkg.Ecosystem, installed.PackageManager, installed.VendorAdvisorySource, installed.Distro) {
		return false
	}
	if pkg.PackageID != "" && pkg.PackageID == installed.PackageID {
		return true
	}
	return pkg.PURL != "" && packageIDFromPURL(pkg.PURL) == installed.PackageID
}

func supportedOSPackageImpactManager(manager string) bool {
	switch strings.ToLower(strings.TrimSpace(manager)) {
	case "rpm", "dpkg", "apk":
		return true
	default:
		return false
	}
}

func osPackageEcosystemMatchesVendor(
	ecosystem string,
	packageManager string,
	vendorSource string,
	distro string,
) bool {
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	if ecosystem == "" {
		return true
	}
	packageManager = strings.ToLower(strings.TrimSpace(packageManager))
	vendorSource = strings.ToLower(strings.TrimSpace(vendorSource))
	distro = strings.ToLower(strings.TrimSpace(distro))
	switch ecosystem {
	case packageManager, vendorSource, distro:
		return true
	case string(packageidentity.EcosystemOS):
		return osPackageFamilyFromPackageManager(packageManager) != "" &&
			osPackageVendorMatchesFamily(vendorSource, packageManager, distro)
	case "deb":
		return packageManager == "dpkg" || vendorSource == "debian" || distro == "debian" || distro == "ubuntu"
	case "debian":
		return vendorSource == "debian" || distro == "debian"
	case "ubuntu":
		return vendorSource == "ubuntu" || distro == "ubuntu"
	case "apk", "alpine":
		return packageManager == "apk" || vendorSource == "alpine" || distro == "alpine"
	case "rpm":
		return packageManager == "rpm"
	case "redhat", "rhel", "fedora", "centos", "rocky", "rockylinux", "alma", "amazon", "amazonlinux":
		return packageManager == "rpm" && (vendorSource == ecosystem || distro == ecosystem)
	default:
		return false
	}
}

func firstSBOMProductImpactPath(
	product supplychainmodel.AffectedProduct,
	index supplyChainImpactIndex,
) (supplychainmodel.SBOMComponent, supplychainmodel.Attachment, supplyChainImageIdentity, bool, []string) {
	var missing []string
	for _, component := range index.components {
		if !componentMatchesAffectedProduct(component, product) {
			continue
		}
		attachment := index.attachments[component.DocumentID]
		if attachment.SubjectDigest == "" || attachment.Status == "subject_mismatch" || attachment.Status == "unknown_subject" {
			continue
		}
		image := index.images[attachment.SubjectDigest]
		if image.digest == "" {
			missing = append(missing, "image identity evidence missing")
			continue
		}
		if reason := unusableSupplyChainImageIdentityReason(image); reason != "" {
			missing = append(missing, reason)
			continue
		}
		return component, attachment, image, true, nil
	}
	return supplychainmodel.SBOMComponent{}, supplychainmodel.Attachment{}, supplyChainImageIdentity{}, false, payloadcore.UniqueSortedStrings(missing)
}

func unusableSupplyChainImageIdentityReason(image supplyChainImageIdentity) string {
	switch image.outcome {
	case "", string(reducercontract.ContainerImageIdentityExactDigest), string(reducercontract.ContainerImageIdentityTagResolved):
		if image.outcome == "" || image.canonicalWrites > 0 {
			return ""
		}
		return "image identity evidence missing"
	case string(reducercontract.ContainerImageIdentityAmbiguousTag):
		return "image identity evidence ambiguous"
	case string(reducercontract.ContainerImageIdentityStaleTag):
		return "image identity evidence stale"
	case string(reducercontract.ContainerImageIdentityUnresolved):
		return "image identity evidence unresolved"
	default:
		return "image identity evidence unsupported"
	}
}

// payloadBool forwards to [payloadcore.PayloadBool].
func payloadBool(payload map[string]any, key string) bool {
	return payloadcore.PayloadBool(payload, key)
}

// supplyChainInt forwards to [payloadcore.PayloadInt].
func supplyChainInt(payload map[string]any, key string) int {
	return payloadcore.PayloadInt(payload, key)
}

// derefFloat64 returns the pointed-to float64, or 0 for a nil pointer. The
// vulnerability.cve typed decode carries CVSSScore as *float64 so an absent
// score stays distinct from an observed 0.0; this mirrors the pre-typing
// supplyChainFloat's own default-to-zero behavior for callers that only need
// the value, not the presence.
func derefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
