// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func appendUniqueSupplyChainImpactFacts(envelopes []facts.Envelope, active ...facts.Envelope) []facts.Envelope {
	if len(active) == 0 {
		return envelopes
	}
	seen := make(map[string]struct{}, len(envelopes)+len(active))
	for _, envelope := range envelopes {
		// A fold's files left the pass as decoded envelopes but are still
		// present: a later stage must not append one of them again (#9781).
		if fold := fileReachabilityFoldFromDigest(envelope); fold != nil {
			for factID := range fold.seen {
				seen[factID] = struct{}{}
			}
		}
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
		UpstreamCVEIDs:    missingStringValues(current.UpstreamCVEIDs, requested.UpstreamCVEIDs),
		AliasIDs:          missingStringValues(current.AliasIDs, requested.AliasIDs),
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
		merged.UpstreamCVEIDs = append(merged.UpstreamCVEIDs, filter.UpstreamCVEIDs...)
		merged.AliasIDs = append(merged.AliasIDs, filter.AliasIDs...)
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
		UpstreamCVEIDs:    payloadcore.UniqueSortedStrings(merged.UpstreamCVEIDs),
		AliasIDs:          payloadcore.UniqueSortedStrings(merged.AliasIDs),
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
	// aliasIDs collects the typed aliases lists of loaded vulnerability.cve
	// records: the alias arm the until-stable loop iterates to an
	// alias-closed fixed point (#10407).
	var aliasIDs []string
	// signalCVEIDs is the CVE ids of facts that only describe a CVE. They reach
	// cveIDs after the loop, unless a record's upstream list is why they are here.
	var upstreamKeys, signalCVEIDs []string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case chain.VulnerabilityCVEFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(envelope.Payload, "advisory_id"))
			upstreamKeys = appendUpstreamCVEKeys(upstreamKeys, envelope.Payload)
			aliasIDs = appendAliasIDs(aliasIDs, envelope.Payload)
		case chain.VulnerabilityAffectedPackageFactKind:
			purl := payloadcore.PayloadStr(envelope.Payload, "purl")
			packageIDs = append(packageIDs, canonicalSupplyChainAffectedPackageID(payloadcore.PayloadStr(envelope.Payload, "package_id"), purl))
			purls = append(purls, purl)
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(envelope.Payload, "advisory_id"))
		case chain.VulnerabilityAffectedProductFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
			advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(envelope.Payload, "advisory_id"))
			if payloadBool(envelope.Payload, "vulnerable") {
				productCriteria = append(productCriteria, payloadcore.PayloadStr(envelope.Payload, "criteria"))
			}
		case chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityKnownExploitedFactKind:
			signalCVEIDs = append(signalCVEIDs, supplyChainCVEID(envelope.Payload))
		case chain.VulnerabilitySuppressionFactKind:
			if scope := payloadcore.PayloadMap(envelope.Payload, "scope"); scope != nil {
				signalCVEIDs = append(signalCVEIDs, payloadcore.PayloadStr(scope, "cve_id"))
				advisoryIDs = append(advisoryIDs, payloadcore.PayloadStr(scope, "advisory_id"))
				packageIDs = append(packageIDs, payloadcore.PayloadStr(scope, "package_id"))
				purls = append(purls, payloadcore.PayloadStr(scope, "purl"))
				digests = append(digests, payloadcore.PayloadStr(scope, "subject_digest"))
				repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(scope, "repository_id"))
			}
		case chain.VulnerabilityGoModuleEvidenceFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case chain.VulnerabilityGoCallReachabilityFactKind:
			cveIDs = append(cveIDs, payloadcore.PayloadStr(envelope.Payload, "osv_id"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case facts.SecurityAlertRepositoryAlertFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			cveIDs = append(cveIDs, payloadcore.PayloadStrings(envelope.Payload, "cve_id", "cve_ids")...)
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case chain.PackageRegistryPackageFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
		case correlation.PackageConsumptionFactKind:
			packageIDs = append(packageIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			repositoryIDs = append(repositoryIDs, payloadcore.PayloadStr(envelope.Payload, "repository_id"))
		case factload.FactKindContentEntity:
			dependencies := correlation.ExtractPackageManifestDependencies([]facts.Envelope{envelope})
			for _, dependency := range dependencies {
				repositoryIDs = append(repositoryIDs, dependency.RepositoryID)
			}
		case chain.SBOMComponentFactKind:
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
		case chain.OCIImageManifestFactKind, chain.OCIImageIndexFactKind:
			digests = append(digests, payloadcore.PayloadStr(envelope.Payload, "digest"))
			repositoryIDs = append(repositoryIDs, payloadcore.OCIRepositoryID(envelope.Payload))
			imageRefs = append(imageRefs, ociRegistryImageRef(envelope.Payload, payloadcore.PayloadStr(envelope.Payload, "source_tag")))
		case chain.OCIImageTagObservationFactKind:
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
		case chain.OCIImageReferrerFactKind:
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
	upstreamKeys = payloadcore.UniqueSortedStrings(upstreamKeys)
	cveIDs = appendSignalCVEIDs(cveIDs, signalCVEIDs, upstreamKeys)
	sortedCVEIDs := payloadcore.UniqueSortedStrings(cveIDs)
	return SupplyChainImpactFactFilter{
		PackageIDs:        payloadcore.UniqueSortedStrings(packageIDs),
		PURLs:             payloadcore.UniqueSortedStrings(purls),
		CVEIDs:            sortedCVEIDs,
		UpstreamCVEIDs:    missingStringValues(upstreamKeys, sortedCVEIDs),
		AliasIDs:          payloadcore.UniqueSortedStrings(aliasIDs),
		AdvisoryIDs:       payloadcore.UniqueSortedStrings(advisoryIDs),
		SubjectDigests:    payloadcore.UniqueSortedStrings(digests),
		DocumentIDs:       payloadcore.UniqueSortedStrings(documentIDs),
		ProductCriteria:   payloadcore.UniqueSortedStrings(productCriteria),
		RepositoryIDs:     supplyChainImpactRepositoryFilterIDs(repositoryIDs),
		FileRepositoryIDs: supplyChainImpactParserFileRepositoryIDs(envelopes),
		ImageRefs:         payloadcore.UniqueSortedStrings(imageRefs),
	}
}

// appendAliasIDs adds the advisory ids a record's aliases list names. The
// filter reads the raw payload because it runs before the typed decode;
// blank entries are dropped so the reader never binds an empty probe value.
func appendAliasIDs(ids []string, payload map[string]any) []string {
	for _, entry := range payloadcore.PayloadStrings(payload, "", "aliases") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

// appendUpstreamCVEKeys adds the group keys a record's upstream list forms, under
// the same rule the grouping pass reads them. The filter reads the raw payload
// because it runs before the typed decode.
func appendUpstreamCVEKeys(keys []string, payload map[string]any) []string {
	if _, ok := payload["upstream"]; !ok {
		return keys
	}
	for _, entry := range payloadcore.PayloadStrings(payload, "", "upstream") {
		if key, ok := upstreamCVEKey(entry); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// appendSignalCVEIDs adds the CVE ids of KEV, EPSS and suppression facts to the
// full CVE arm, except those a loaded record's upstream list names. Those facts
// reached the pass through the upstream arm, and promoting their CVE here would
// load its NVD record and product rows on the next round, the widening the #9914
// shim failed. A signal for any other CVE keeps the full arm, because an EPSS or
// KEV intent re-reads the findings for its CVE through it. upstreamKeys is sorted.
func appendSignalCVEIDs(cveIDs, signalCVEIDs, upstreamKeys []string) []string {
	if len(upstreamKeys) == 0 {
		return append(cveIDs, signalCVEIDs...)
	}
	for _, id := range signalCVEIDs {
		if key, ok := upstreamCVEKey(id); ok {
			if _, named := slices.BinarySearch(upstreamKeys, key); named {
				continue
			}
		}
		cveIDs = append(cveIDs, id)
	}
	return cveIDs
}

func supplyChainImpactRepositoryFilterIDs(repositoryIDs []string) []string {
	ids := payloadcore.UniqueSortedStrings(repositoryIDs)
	out := append([]string(nil), ids...)
	for _, repositoryID := range ids {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID != "" && !strings.HasPrefix(repositoryID, scope.GitRepositoryScopePrefix) {
			out = append(out, scope.GitRepositoryScopePrefix+repositoryID)
		}
	}
	return payloadcore.UniqueSortedStrings(out)
}

func (f SupplyChainImpactFactFilter) empty() bool {
	return len(f.PackageIDs) == 0 && len(f.PURLs) == 0 && len(f.CVEIDs) == 0 && len(f.UpstreamCVEIDs) == 0 && len(f.AliasIDs) == 0 && len(f.AdvisoryIDs) == 0 &&
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
		dependencyKeys := manifestDependencyKeySet(dependency)
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
		if envelope.FactKind != chain.VulnerabilityAffectedPackageFactKind {
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

// supplyChainImpactFactKinds is the set of fact kinds loaded from the intent's
// OWN scope/generation via factload.LoadFactsForKinds. It intentionally OMITS
// chain.VulnerabilityOSPackageFactKind and facts.ScannerWorkerAnalysisFactKind
// even though both feed supply-chain-impact classification: neither lives in
// the intent's vulnerability-intelligence scope, so listing them here would
// query the wrong scope and return nothing. They are loaded cross-scope by two
// dedicated stages in loadSupplyChainImpactEvidence instead —
// loadSupplyChainImpactOSPackageAdvisoryFacts pulls vulnerability.os_package by
// ecosystem through the advisory-target reader (#5705/#5463), and
// loadSupplyChainImpactScannerAnalysisScopeFacts then loads each os_package's
// sibling scanner_worker.analysis from that package's OWN scan scope to stamp
// SubjectDigest (#5463). Adding either kind here is the tempting-but-wrong fix
// flagged in review: it cannot reach a cross-scope fact. The end-to-end path is
// proven by TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope
// and the golden-corpus list_supply_chain_impact_findings floor (CVE-2026-00010).
func supplyChainImpactFactKinds() []string {
	return []string{
		chain.VulnerabilityCVEFactKind,
		chain.VulnerabilityAffectedPackageFactKind,
		chain.VulnerabilityAffectedProductFactKind,
		chain.VulnerabilityEPSSScoreFactKind,
		chain.VulnerabilityKnownExploitedFactKind,
		chain.VulnerabilitySuppressionFactKind,
		chain.VulnerabilityGoModuleEvidenceFactKind,
		chain.VulnerabilityGoCallReachabilityFactKind,
		facts.SecurityAlertRepositoryAlertFactKind,
		chain.PackageRegistryPackageFactKind,
		chain.SBOMComponentFactKind,
		chain.OCIImageManifestFactKind,
		chain.OCIImageIndexFactKind,
		chain.OCIImageTagObservationFactKind,
		chain.OCIImageReferrerFactKind,
		reducercontract.SBOMAttestationAttachmentFactKind,
		reducercontract.ContainerImageIdentityFactKind,
		correlation.PackageConsumptionFactKind,
		cicdrun.CICDRunCorrelationFactKind,
		reducercontract.PlatformMaterializationFactKind,
		reducercontract.WorkloadIdentityFactKind,
		reducercontract.ServiceCatalogCorrelationFactKind,
		factload.FactKindFile,
	}
}
