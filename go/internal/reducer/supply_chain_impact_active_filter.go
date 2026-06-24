// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
		merged.SubjectDigests = append(merged.SubjectDigests, filter.SubjectDigests...)
		merged.DocumentIDs = append(merged.DocumentIDs, filter.DocumentIDs...)
		merged.ProductCriteria = append(merged.ProductCriteria, filter.ProductCriteria...)
		merged.RepositoryIDs = append(merged.RepositoryIDs, filter.RepositoryIDs...)
		merged.FileRepositoryIDs = append(merged.FileRepositoryIDs, filter.FileRepositoryIDs...)
		merged.ImageRefs = append(merged.ImageRefs, filter.ImageRefs...)
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:        uniqueSortedStrings(merged.PackageIDs),
		PURLs:             uniqueSortedStrings(merged.PURLs),
		CVEIDs:            uniqueSortedStrings(merged.CVEIDs),
		SubjectDigests:    uniqueSortedStrings(merged.SubjectDigests),
		DocumentIDs:       uniqueSortedStrings(merged.DocumentIDs),
		ProductCriteria:   uniqueSortedStrings(merged.ProductCriteria),
		RepositoryIDs:     uniqueSortedStrings(merged.RepositoryIDs),
		FileRepositoryIDs: uniqueSortedStrings(merged.FileRepositoryIDs),
		ImageRefs:         uniqueSortedStrings(merged.ImageRefs),
	}
}

func missingStringValues(current []string, initial []string) []string {
	if len(current) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(initial))
	for _, value := range initial {
		seen[value] = struct{}{}
	}
	missing := make([]string, 0, len(current))
	for _, value := range current {
		if _, ok := seen[value]; ok {
			continue
		}
		missing = append(missing, value)
	}
	return missing
}

func supplyChainImpactFilter(envelopes []facts.Envelope) SupplyChainImpactFactFilter {
	var packageIDs, purls, cveIDs, digests, documentIDs, productCriteria, repositoryIDs, imageRefs []string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.VulnerabilityCVEFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
		case facts.VulnerabilityAffectedPackageFactKind:
			purl := payloadStr(envelope.Payload, "purl")
			packageIDs = append(packageIDs, canonicalSupplyChainAffectedPackageID(envelope.Payload, purl))
			purls = append(purls, purl)
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
		case facts.VulnerabilityAffectedProductFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			if payloadBool(envelope.Payload, "vulnerable") {
				productCriteria = append(productCriteria, payloadStr(envelope.Payload, "criteria"))
			}
		case facts.VulnerabilityEPSSScoreFactKind, facts.VulnerabilityKnownExploitedFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
		case facts.VulnerabilitySuppressionFactKind:
			if scope := payloadMap(envelope.Payload, "scope"); scope != nil {
				cveIDs = append(cveIDs, payloadStr(scope, "cve_id"))
				packageIDs = append(packageIDs, payloadStr(scope, "package_id"))
				purls = append(purls, payloadStr(scope, "purl"))
				digests = append(digests, payloadStr(scope, "subject_digest"))
				repositoryIDs = append(repositoryIDs, payloadStr(scope, "repository_id"))
			}
		case facts.VulnerabilityGoModuleEvidenceFactKind:
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
		case facts.VulnerabilityGoCallReachabilityFactKind:
			cveIDs = append(cveIDs, payloadStr(envelope.Payload, "osv_id"))
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
		case facts.SecurityAlertRepositoryAlertFactKind:
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			cveIDs = append(cveIDs, payloadStrings(envelope.Payload, "cve_id", "cve_ids")...)
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
		case facts.PackageRegistryPackageFactKind:
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
		case packageConsumptionCorrelationFactKind:
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
		case factKindContentEntity:
			dependencies := extractPackageManifestDependencies([]facts.Envelope{envelope})
			for _, dependency := range dependencies {
				repositoryIDs = append(repositoryIDs, dependency.RepositoryID)
			}
		case facts.SBOMComponentFactKind:
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			purls = append(purls, payloadStr(envelope.Payload, "purl"))
			documentIDs = append(documentIDs, payloadStr(envelope.Payload, "document_id"))
			productCriteria = append(productCriteria, payloadStr(envelope.Payload, "cpe"))
		case sbomAttestationAttachmentFactKind:
			digests = append(digests, payloadStr(envelope.Payload, "subject_digest"))
			documentIDs = append(documentIDs, payloadStr(envelope.Payload, "document_id"))
		case containerImageIdentityFactKind:
			digests = append(digests, payloadStr(envelope.Payload, "digest"))
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
			imageRefs = append(imageRefs, payloadStr(envelope.Payload, "image_ref"))
		case facts.OCIImageManifestFactKind, facts.OCIImageIndexFactKind:
			digests = append(digests, payloadStr(envelope.Payload, "digest"))
			repositoryIDs = append(repositoryIDs, ociRepositoryID(envelope.Payload))
			imageRefs = append(imageRefs, ociRegistryImageRef(envelope.Payload, payloadStr(envelope.Payload, "source_tag")))
		case facts.OCIImageTagObservationFactKind:
			digests = append(
				digests,
				payloadStr(envelope.Payload, "resolved_digest"),
				payloadStr(envelope.Payload, "digest"),
			)
			repositoryIDs = append(repositoryIDs, ociRepositoryID(envelope.Payload))
			imageRefs = append(
				imageRefs,
				payloadStr(envelope.Payload, "image_ref"),
				ociRegistryImageRef(envelope.Payload, payloadStr(envelope.Payload, "tag")),
			)
		case facts.OCIImageReferrerFactKind:
			digests = append(
				digests,
				payloadStr(envelope.Payload, "subject_digest"),
				payloadStr(envelope.Payload, "referrer_digest"),
			)
			repositoryIDs = append(repositoryIDs, ociRepositoryID(envelope.Payload))
		case cicdRunCorrelationFactKind:
			digests = append(digests, payloadStr(envelope.Payload, "artifact_digest"))
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
			imageRefs = append(imageRefs, payloadStr(envelope.Payload, "image_ref"))
		case platformMaterializationFactKind:
			repositoryIDs = append(repositoryIDs, supplyChainWorkloadRepositoryID(envelope))
		case workloadIdentityFactKind:
			repositoryIDs = append(repositoryIDs, supplyChainWorkloadRepositoryID(envelope))
		case serviceCatalogCorrelationFactKind:
			repositoryIDs = append(repositoryIDs, supplyChainServiceRepositoryID(envelope))
		}
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:        uniqueSortedStrings(packageIDs),
		PURLs:             uniqueSortedStrings(purls),
		CVEIDs:            uniqueSortedStrings(cveIDs),
		SubjectDigests:    uniqueSortedStrings(digests),
		DocumentIDs:       uniqueSortedStrings(documentIDs),
		ProductCriteria:   uniqueSortedStrings(productCriteria),
		RepositoryIDs:     supplyChainImpactRepositoryFilterIDs(repositoryIDs),
		FileRepositoryIDs: supplyChainImpactParserFileRepositoryIDs(envelopes),
		ImageRefs:         uniqueSortedStrings(imageRefs),
	}
}

func supplyChainImpactRepositoryFilterIDs(repositoryIDs []string) []string {
	ids := uniqueSortedStrings(repositoryIDs)
	out := append([]string(nil), ids...)
	for _, repositoryID := range ids {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID != "" && !strings.HasPrefix(repositoryID, "git-repository-scope:") {
			out = append(out, "git-repository-scope:"+repositoryID)
		}
	}
	return uniqueSortedStrings(out)
}

func (f SupplyChainImpactFactFilter) empty() bool {
	return len(f.PackageIDs) == 0 && len(f.PURLs) == 0 && len(f.CVEIDs) == 0 &&
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
		if envelope.FactKind != packageConsumptionCorrelationFactKind {
			continue
		}
		consumption := supplyChainConsumptionFromEnvelope(envelope)
		if consumption.repositoryID == "" {
			continue
		}
		if _, ok := affectedByPackageID[consumption.packageID]; ok {
			repositoryIDs = append(repositoryIDs, consumption.repositoryID)
		}
	}

	for _, dependency := range extractPackageManifestDependencies(envelopes) {
		if dependency.RepositoryID == "" {
			continue
		}
		dependencyKeys := stringSet(packageConsumptionKeys(dependency.PackageManager, dependency.DependencyName))
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

	return uniqueSortedStrings(repositoryIDs)
}

func ociRegistryImageRef(payload map[string]any, tag string) string {
	repositoryID := strings.TrimPrefix(ociRepositoryID(payload), "oci-registry://")
	tag = strings.TrimSpace(tag)
	if repositoryID == "" || tag == "" {
		return ""
	}
	return repositoryID + ":" + tag
}

func npmAffectedPackages(envelopes []facts.Envelope) (map[string]struct{}, map[string][]supplyChainAffectedPackage) {
	byPackageID := map[string]struct{}{}
	groups := map[string][]supplyChainAffectedPackage{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityAffectedPackageFactKind {
			continue
		}
		pkg := supplyChainAffectedPackageFromEnvelope(envelope)
		if normalizedSupplyChainVersionEcosystem(pkg.ecosystem) != "npm" {
			continue
		}
		if pkg.packageID != "" {
			byPackageID[pkg.packageID] = struct{}{}
		}
		if pkg.cveID != "" {
			groups[pkg.cveID] = append(groups[pkg.cveID], pkg)
		}
	}
	return byPackageID, groups
}
