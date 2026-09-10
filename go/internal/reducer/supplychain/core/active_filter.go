// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func appendUniqueSupplyChainImpactFacts(envelopes []facts.Envelope, active ...facts.Envelope) []facts.Envelope {
	if len(active) == 0 {
		return envelopes
	}
	seen := make(map[string]struct{}, len(envelopes)+len(active))
	for _, envelope := range envelopes {
		if envelope.FactID == "" {
			continue
		}
		seen[envelope.FactID] = struct{}{}
	}
	for _, envelope := range active {
		if envelope.FactID == "" {
			envelopes = append(envelopes, envelope)
			continue
		}
		if _, ok := seen[envelope.FactID]; ok {
			continue
		}
		seen[envelope.FactID] = struct{}{}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func supplyChainImpactFollowUpFilter(
	requested SupplyChainImpactFactFilter,
	current SupplyChainImpactFactFilter,
) SupplyChainImpactFactFilter {
	return SupplyChainImpactFactFilter{
		PackageIDs:        missingStringValues(current.PackageIDs, requested.PackageIDs),
		PURLs:             missingStringValues(current.PURLs, requested.PURLs),
		CVEIDs:            missingStringValues(current.CVEIDs, requested.CVEIDs),
		AdvisoryIDs:       missingStringValues(current.AdvisoryIDs, requested.AdvisoryIDs),
		SubjectDigests:    missingStringValues(current.SubjectDigests, requested.SubjectDigests),
		DocumentIDs:       missingStringValues(current.DocumentIDs, requested.DocumentIDs),
		ProductCriteria:   missingStringValues(current.ProductCriteria, requested.ProductCriteria),
		RepositoryIDs:     missingStringValues(current.RepositoryIDs, requested.RepositoryIDs),
		FileRepositoryIDs: missingStringValues(current.FileRepositoryIDs, requested.FileRepositoryIDs),
		ImageRefs:         missingStringValues(current.ImageRefs, requested.ImageRefs),
	}
}

func mergeSupplyChainImpactFactFilters(filters ...SupplyChainImpactFactFilter) SupplyChainImpactFactFilter {
	var merged SupplyChainImpactFactFilter
	for _, filter := range filters {
		merged.PackageIDs = append(merged.PackageIDs, filter.PackageIDs...)
		merged.PURLs = append(merged.PURLs, filter.PURLs...)
		merged.CVEIDs = append(merged.CVEIDs, filter.CVEIDs...)
		merged.AdvisoryIDs = append(merged.AdvisoryIDs, filter.AdvisoryIDs...)
		merged.SubjectDigests = append(merged.SubjectDigests, filter.SubjectDigests...)
		merged.DocumentIDs = append(merged.DocumentIDs, filter.DocumentIDs...)
		merged.ProductCriteria = append(merged.ProductCriteria, filter.ProductCriteria...)
		merged.RepositoryIDs = append(merged.RepositoryIDs, filter.RepositoryIDs...)
		merged.FileRepositoryIDs = append(merged.FileRepositoryIDs, filter.FileRepositoryIDs...)
		merged.ImageRefs = append(merged.ImageRefs, filter.ImageRefs...)
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:        payloadcore.UniqueSortedStrings(merged.PackageIDs),
		PURLs:             payloadcore.UniqueSortedStrings(merged.PURLs),
		CVEIDs:            payloadcore.UniqueSortedStrings(merged.CVEIDs),
		AdvisoryIDs:       payloadcore.UniqueSortedStrings(merged.AdvisoryIDs),
		SubjectDigests:    payloadcore.UniqueSortedStrings(merged.SubjectDigests),
		DocumentIDs:       payloadcore.UniqueSortedStrings(merged.DocumentIDs),
		ProductCriteria:   payloadcore.UniqueSortedStrings(merged.ProductCriteria),
		RepositoryIDs:     payloadcore.UniqueSortedStrings(merged.RepositoryIDs),
		FileRepositoryIDs: payloadcore.UniqueSortedStrings(merged.FileRepositoryIDs),
		ImageRefs:         payloadcore.UniqueSortedStrings(merged.ImageRefs),
	}
}

// missingStringValues forwards to [payloadcore.MissingStrings].
func missingStringValues(current []string, initial []string) []string {
	return payloadcore.MissingStrings(current, initial)
}

func supplyChainImpactFilter(envelopes []facts.Envelope) SupplyChainImpactFactFilter {
	var packageIDs, purls, cveIDs, digests, documentIDs, productCriteria, repositoryIDs, imageRefs []string
	// advisoryIDs is collected separately from cveIDs because
	// supplyChainCVEID prefers cve_id over advisory_id when both are
	// present. The storage reader uses this independent list for both
	// top-level exact matching and normalized suppression-scope matching.
	var advisoryIDs []string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.VulnerabilityCVEFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(envelope.Payload, "advisory_id"))
		case facts.VulnerabilityAffectedPackageFactKind:
			purl := payloadcore.PayloadStr(envelope.Payload, "purl")
			packageIDs = append(packageIDs, canonicalSupplyChainAffectedPackageID(payloadcore.PayloadStr(envelope.Payload, "package_id"), purl))
			purls = append(purls, purl)
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(envelope.Payload, "advisory_id"))
		case facts.VulnerabilityAffectedProductFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(envelope.Payload, "advisory_id"))
			if payloadBool(envelope.Payload, "vulnerable") {
				productCriteria = append(productCriteria, payloadcore.PayloadStr(envelope.Payload, "criteria"))
			}
		case facts.VulnerabilityEPSSScoreFactKind, facts.VulnerabilityKnownExploitedFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
		case facts.VulnerabilitySuppressionFactKind:
			if scope := payloadcore.PayloadMap(envelope.Payload, "scope"); scope != nil {
				cveIDs = append(cveIDs, payloadcore.PayloadStr(scope, "cve_id"))
				advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(scope, "advisory_id"))
				packageIDs = append(packageIDs, payloadcore.PayloadStr(scope, "package_id"))
				purls = append(purls, payloadcore.PayloadStr(scope, "purl"))
				digests = append(digests, payloadcore.PayloadStr(scope, "subject_digest"))
				repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(scope, "repository_id"))
			}
		case facts.VulnerabilityGoModuleEvidenceFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case facts.VulnerabilityGoCallReachabilityFactKind:
			cveIDs = append(cveIDs, payloadcore.PayloadStr(envelope.Payload, "osv_id"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case facts.SecurityAlertRepositoryAlertFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			cveIDs = append(cveIDs, payloadcore.PayloadStrings(envelope.Payload, "cve_id", "cve_ids")...)
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case facts.PackageRegistryPackageFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
		case correlation.PackageConsumptionFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case factload.FactKindContentEntity:
			dependencies := correlation.ExtractPackageManifestDependencies([]facts.Envelope{envelope})
			for _, dependency := range dependencies {
				repositoryIDs = append(repositoryIDs, dependency.RepositoryID)
			}
		case facts.SBOMComponentFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			purls = append(purls, payloadcore.PayloadStr(envelope.Payload, "purl"))
			documentIDs = append(documentIDs, payloadcore.PayloadStr(envelope.Payload, "document_id"))
			productCriteria = append(productCriteria, payloadcore.PayloadStr(envelope.Payload, "cpe"))
		case reducercontract.SBOMAttestationAttachmentFactKind:
			digests = append(digests, payloadcore.PayloadStr(envelope.Payload, "subject_digest"))
			documentIDs = append(documentIDs, payloadcore.PayloadStr(envelope.Payload, "document_id"))
		case reducercontract.ContainerImageIdentityFactKind:
			digests = append(digests, payloadcore.PayloadStr(envelope.Payload, "digest"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
			imageRefs = append(imageRefs, payloadcore.PayloadStr(envelope.Payload, "image_ref"))
		case facts.ScannerWorkerAnalysisFactKind:
			// Feeds loadSupplyChainImpactResolvedDigestEvidenceFacts (issue
			// #5464): the scanner-analysis-scope stage only learns an
			// os_package's real scanned digest after the original
			// active-evidence stage already ran, so that digest/image_ref
			// must be re-derived from the analysis envelope itself here,
			// rather than from a case this switch already has for some
			// other fact kind.
			digests = append(digests, payloadcore.PayloadStr(envelope.Payload, "image_digest"))
			imageRefs = append(imageRefs, payloadcore.PayloadStr(envelope.Payload, "image_reference"))
		case facts.OCIImageManifestFactKind, facts.OCIImageIndexFactKind:
			digests = append(digests, payloadcore.PayloadStr(envelope.Payload, "digest"))
			repositoryIDs = append(repositoryIDs, payloadcore.OCIRepositoryID(envelope.Payload))
			imageRefs = append(imageRefs, ociRegistryImageRef(envelope.Payload, payloadcore.PayloadStr(envelope.Payload, "source_tag")))
		case facts.OCIImageTagObservationFactKind:
			digests = append(
				digests,
				payloadcore.PayloadStr(envelope.Payload, "resolved_digest"),
				payloadcore.PayloadStr(envelope.Payload, "digest"),
			)
			repositoryIDs = append(repositoryIDs, payloadcore.OCIRepositoryID(envelope.Payload))
			imageRefs = append(
				imageRefs,
				payloadcore.PayloadStr(envelope.Payload, "image_ref"),
				ociRegistryImageRef(envelope.Payload, payloadcore.PayloadStr(envelope.Payload, "tag")),
			)
		case facts.OCIImageReferrerFactKind:
			digests = append(
				digests,
				payloadcore.PayloadStr(envelope.Payload, "subject_digest"),
				payloadcore.PayloadStr(envelope.Payload, "referrer_digest"),
			)
			repositoryIDs = append(repositoryIDs, payloadcore.OCIRepositoryID(envelope.Payload))
		case cicdrun.CICDRunCorrelationFactKind:
			digests = append(digests, payloadcore.PayloadStr(envelope.Payload, "artifact_digest"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
			imageRefs = append(imageRefs, payloadcore.PayloadStr(envelope.Payload, "image_ref"))
		case reducercontract.PlatformMaterializationFactKind:
			repositoryIDs = append(repositoryIDs, supplyChainWorkloadRepositoryID(envelope))
		case reducercontract.WorkloadIdentityFactKind:
			repositoryIDs = append(repositoryIDs, supplyChainWorkloadRepositoryID(envelope))
		case reducercontract.ServiceCatalogCorrelationFactKind:
			repositoryIDs = append(repositoryIDs, supplyChainServiceRepositoryID(envelope))
		}
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:        payloadcore.UniqueSortedStrings(packageIDs),
		PURLs:             payloadcore.UniqueSortedStrings(purls),
		CVEIDs:            payloadcore.UniqueSortedStrings(cveIDs),
		AdvisoryIDs:       payloadcore.UniqueSortedStrings(advisoryIDs),
		SubjectDigests:    payloadcore.UniqueSortedStrings(digests),
		DocumentIDs:       payloadcore.UniqueSortedStrings(documentIDs),
		ProductCriteria:   payloadcore.UniqueSortedStrings(productCriteria),
		RepositoryIDs:     supplyChainImpactRepositoryFilterIDs(repositoryIDs),
		FileRepositoryIDs: supplyChainImpactParserFileRepositoryIDs(envelopes),
		ImageRefs:         payloadcore.UniqueSortedStrings(imageRefs),
	}
}

func supplyChainImpactRepositoryFilterIDs(repositoryIDs []string) []string {
	ids := payloadcore.UniqueSortedStrings(repositoryIDs)
	out := append([]string(nil), ids...)
	for _, repositoryID := range ids {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID != "" && !strings.HasPrefix(repositoryID, "git-repository-scope:") {
			out = append(out, "git-repository-scope:"+repositoryID)
		}
	}
	return payloadcore.UniqueSortedStrings(out)
}

func (f SupplyChainImpactFactFilter) empty() bool {
	return len(f.PackageIDs) == 0 && len(f.PURLs) == 0 && len(f.CVEIDs) == 0 && len(f.AdvisoryIDs) == 0 &&
		len(f.SubjectDigests) == 0 && len(f.DocumentIDs) == 0 && len(f.ProductCriteria) == 0 &&
		len(f.RepositoryIDs) == 0 && len(f.FileRepositoryIDs) == 0 && len(f.ImageRefs) == 0
}

func supplyChainImpactParserFileRepositoryIDs(envelopes []facts.Envelope) []string {
	affectedByPackageID, affectedGroups := npmAffectedPackages(envelopes)
	if len(affectedByPackageID) == 0 {
		return nil
	}

	var repositoryIDs []string
	for _, envelope := range envelopes {
		if envelope.FactKind != correlation.PackageConsumptionFactKind {
			continue
		}
		consumption, err := supplyChainConsumptionFromEnvelope(envelope)
		if err != nil {
			continue
		}
		if consumption.RepositoryID == "" {
			continue
		}
		if _, ok := affectedByPackageID[consumption.PackageID]; ok {
			repositoryIDs = append(repositoryIDs, consumption.RepositoryID)
		}
	}

	for _, dependency := range correlation.ExtractPackageManifestDependencies(envelopes) {
		if dependency.RepositoryID == "" {
			continue
		}
		dependencyKeys := stringSet(correlation.PackageConsumptionKeys(dependency.PackageManager, dependency.DependencyName))
		if len(dependencyKeys) == 0 {
			continue
		}
		for _, affected := range manifestAffectedPackageMatches(affectedGroups) {
			if manifestDependencyMatchesAffectedPackage(dependencyKeys, affected.keys) {
				repositoryIDs = append(repositoryIDs, dependency.RepositoryID)
				break
			}
		}
	}

	return payloadcore.UniqueSortedStrings(repositoryIDs)
}

func ociRegistryImageRef(payload map[string]any, tag string) string {
	repositoryID := strings.TrimPrefix(payloadcore.OCIRepositoryID(payload), "oci-registry://")
	tag = strings.TrimSpace(tag)
	if repositoryID == "" || tag == "" {
		return ""
	}
	return repositoryID + ":" + tag
}

// npmAffectedPackages is a best-effort filter-hint builder (feeding the
// follow-up manifest-dependency query filter), not the authoritative decode
// path — buildSupplyChainImpactIndex is. A fact that fails typed decode here
// is silently skipped rather than quarantined; the authoritative index build
// still quarantines and reports it as an input_invalid dead-letter.
func npmAffectedPackages(envelopes []facts.Envelope) (map[string]struct{}, map[string][]supplychainmodel.AffectedPackage) {
	byPackageID := map[string]struct{}{}
	groups := map[string][]supplychainmodel.AffectedPackage{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityAffectedPackageFactKind {
			continue
		}
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			continue
		}
		if normalizedSupplyChainVersionEcosystem(pkg.Ecosystem) != "npm" {
			continue
		}
		if pkg.PackageID != "" {
			byPackageID[pkg.PackageID] = struct{}{}
		}
		if pkg.CVEID != "" {
			groups[pkg.CVEID] = append(groups[pkg.CVEID], pkg)
		}
	}
	return byPackageID, groups
}
