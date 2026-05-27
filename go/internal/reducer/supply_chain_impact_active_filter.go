package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

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
		PackageIDs:      missingStringValues(current.PackageIDs, requested.PackageIDs),
		PURLs:           missingStringValues(current.PURLs, requested.PURLs),
		CVEIDs:          missingStringValues(current.CVEIDs, requested.CVEIDs),
		SubjectDigests:  missingStringValues(current.SubjectDigests, requested.SubjectDigests),
		DocumentIDs:     missingStringValues(current.DocumentIDs, requested.DocumentIDs),
		ProductCriteria: missingStringValues(current.ProductCriteria, requested.ProductCriteria),
		RepositoryIDs:   missingStringValues(current.RepositoryIDs, requested.RepositoryIDs),
		ImageRefs:       missingStringValues(current.ImageRefs, requested.ImageRefs),
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
		merged.ImageRefs = append(merged.ImageRefs, filter.ImageRefs...)
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:      uniqueSortedStrings(merged.PackageIDs),
		PURLs:           uniqueSortedStrings(merged.PURLs),
		CVEIDs:          uniqueSortedStrings(merged.CVEIDs),
		SubjectDigests:  uniqueSortedStrings(merged.SubjectDigests),
		DocumentIDs:     uniqueSortedStrings(merged.DocumentIDs),
		ProductCriteria: uniqueSortedStrings(merged.ProductCriteria),
		RepositoryIDs:   uniqueSortedStrings(merged.RepositoryIDs),
		ImageRefs:       uniqueSortedStrings(merged.ImageRefs),
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
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			purls = append(purls, payloadStr(envelope.Payload, "purl"))
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
		case cicdRunCorrelationFactKind:
			digests = append(digests, payloadStr(envelope.Payload, "artifact_digest"))
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
			imageRefs = append(imageRefs, payloadStr(envelope.Payload, "image_ref"))
		case workloadIdentityFactKind:
			repositoryIDs = append(repositoryIDs, firstNonBlank(
				payloadStr(envelope.Payload, "repository_id"),
				payloadStr(envelope.Payload, "repo_id"),
				payloadStr(envelope.Payload, "scope_id"),
				envelope.ScopeID,
			))
		case serviceCatalogCorrelationFactKind:
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
		}
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:      uniqueSortedStrings(packageIDs),
		PURLs:           uniqueSortedStrings(purls),
		CVEIDs:          uniqueSortedStrings(cveIDs),
		SubjectDigests:  uniqueSortedStrings(digests),
		DocumentIDs:     uniqueSortedStrings(documentIDs),
		ProductCriteria: uniqueSortedStrings(productCriteria),
		RepositoryIDs:   uniqueSortedStrings(repositoryIDs),
		ImageRefs:       uniqueSortedStrings(imageRefs),
	}
}

func (f SupplyChainImpactFactFilter) empty() bool {
	return len(f.PackageIDs) == 0 && len(f.PURLs) == 0 && len(f.CVEIDs) == 0 &&
		len(f.SubjectDigests) == 0 && len(f.DocumentIDs) == 0 && len(f.ProductCriteria) == 0 &&
		len(f.RepositoryIDs) == 0 && len(f.ImageRefs) == 0
}
