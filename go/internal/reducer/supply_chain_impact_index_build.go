// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// buildSupplyChainImpactIndexWithQuarantine is the quarantine-aware index
// builder buildSupplyChainImpactFindingsWithQuarantine calls directly so the
// reducer intent path can report each malformed vulnerability.* fact as a
// visible input_invalid dead-letter via recordQuarantinedFacts. Seven kinds
// decode through the typed contracts seam via addSupplyChainImpactIndexEntry:
// vulnerability.cve, .affected_package, .affected_product, .os_package,
// .epss_score, .known_exploited, and scanner_worker.analysis
// (go_module_evidence/go_call_reachability decode separately, through
// classifyGoVulnerabilityReachabilityWithQuarantine below). A fact missing a
// required identity field is routed through
// partitionDecodeFailures into the returned quarantine list rather than
// silently excluded from the index with no operator signal; any OTHER decode
// error (a transient condition partitionDecodeFailures does not quarantine)
// is returned fatally so the caller fails the whole intent for durable
// triage.
func buildSupplyChainImpactIndexWithQuarantine(envelopes []facts.Envelope) (supplyChainImpactIndex, []quarantinedFact, error) {
	index := supplyChainImpactIndex{
		affectedPackages:        map[string][]supplyChainAffectedPackage{},
		affectedProducts:        map[string][]supplyChainAffectedProduct{},
		consumption:             map[string][]supplyChainPackageConsumption{},
		osPackages:              map[string][]supplyChainOSPackage{},
		attachments:             map[string]supplyChainAttachment{},
		images:                  map[string]supplyChainImageIdentity{},
		riskSignals:             map[string]supplyChainRiskSignals{},
		scannerAnalyses:         map[string]supplyChainScannerAnalysis{},
		goReachability:          map[string]GoVulnerabilityFinding{},
		jsTSPackageReachability: buildJSTSPackageReachabilityIndex(envelopes),
		pythonReachability:      map[string]pythonReachabilityRepositoryEvidence{},
		jvmReachability:         buildJVMReachabilityIndex(envelopes),
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		q, isQuarantine, fatal := addSupplyChainImpactIndexEntry(&index, envelope)
		if fatal != nil {
			return supplyChainImpactIndex{}, nil, fatal
		}
		if isQuarantine {
			quarantined = append(quarantined, q)
		}
	}
	goFindings, goQuarantined, err := classifyGoVulnerabilityReachabilityWithQuarantine(envelopes)
	if err != nil {
		return supplyChainImpactIndex{}, nil, err
	}
	quarantined = append(quarantined, goQuarantined...)
	for _, finding := range goFindings {
		if finding.OSVID == "" || finding.ModulePath == "" {
			continue
		}
		key := goSupplyChainReachabilityKey(finding.OSVID, finding.ModulePath, finding.RepositoryID)
		index.goReachability[key] = finding
	}
	index.pythonReachability = extractPythonReachabilityEvidence(envelopes)
	addManifestDependencySupplyChainConsumption(&index, envelopes)
	sort.SliceStable(index.cves, func(i, j int) bool {
		return index.cves[i].cveID < index.cves[j].cveID
	})
	return index, quarantined, nil
}

// addSupplyChainImpactIndexEntry decodes/projects one envelope and indexes it
// in place, dispatching on FactKind. It returns a quarantinedFact and
// isQuarantine=true for a per-fact input_invalid decode failure (the caller
// records it and continues with the next envelope); any other decode error is
// returned as fatal so the caller fails the whole intent. An envelope kind
// this index does not track is a silent no-op, matching the pre-typing
// switch's default behavior.
func addSupplyChainImpactIndexEntry(index *supplyChainImpactIndex, envelope facts.Envelope) (quarantinedFact, bool, error) {
	switch envelope.FactKind {
	case facts.VulnerabilityCVEFactKind:
		cve, err := supplyChainCVEFromEnvelope(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		if cve.cveID != "" {
			index.cves = append(index.cves, cve)
		}
	case facts.VulnerabilityAffectedPackageFactKind:
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		if pkg.cveID != "" {
			index.affectedPackages[pkg.cveID] = append(index.affectedPackages[pkg.cveID], pkg)
		}
	case facts.VulnerabilityAffectedProductFactKind:
		product, err := supplyChainAffectedProductFromEnvelope(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		if product.cveID != "" && product.criteria != "" && product.vulnerable {
			index.affectedProducts[product.cveID] = append(index.affectedProducts[product.cveID], product)
		}
	case packageConsumptionCorrelationFactKind:
		consumption, err := supplyChainConsumptionFromEnvelope(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		if consumption.packageID != "" {
			index.consumption[consumption.packageID] = append(index.consumption[consumption.packageID], consumption)
		}
	case facts.VulnerabilityOSPackageFactKind:
		pkg, err := supplyChainOSPackageFromEnvelope(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		if pkg.packageID != "" && pkg.vendorAdvisorySource != "" && pkg.repositoryClass == "vendor" {
			index.osPackages[pkg.packageID] = append(index.osPackages[pkg.packageID], pkg)
		}
	case facts.ScannerWorkerAnalysisFactKind:
		analysis, err := supplyChainScannerAnalysisFromEnvelope(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		// Only index an analysis that actually carries a real image digest — a
		// blank digest (a legitimate not_scanned/unsupported analysis outcome)
		// must never anchor an os_package's SubjectDigest, so it is simply not
		// indexed rather than indexed as an empty-digest row a lookup could match.
		//
		// Keyed by scope+generation only, so multiple scanner_worker.analysis
		// facts for the same scan (different analyzers) resolve last-write-wins.
		// That is safe for SubjectDigest: imageDigest is a content-addressed
		// property of the scanned image, identical across analyzers of the same
		// target, so any winner yields the same digest. EvidenceFactIDs may then
		// cite an arbitrary analyzer of that scan, which is acceptable evidence
		// provenance for a digest that all of them agree on.
		if analysis.imageDigest != "" {
			index.scannerAnalyses[supplyChainScopeGenerationKey(analysis.scopeID, analysis.generationID)] = analysis
		}
	case facts.SBOMComponentFactKind:
		component := supplyChainSBOMComponentFromEnvelope(envelope)
		if component.purl != "" || component.packageID != "" || component.cpe != "" {
			index.components = append(index.components, component)
		}
	case sbomAttestationAttachmentFactKind:
		attachment := supplyChainAttachmentFromEnvelope(envelope)
		if attachment.documentID != "" {
			index.attachments[attachment.documentID] = attachment
		}
	case containerImageIdentityFactKind:
		image := supplyChainImageIdentityFromEnvelope(envelope)
		if image.digest != "" {
			if existing, ok := index.images[image.digest]; ok {
				image = preferSupplyChainImageIdentity(existing, image)
			}
			index.images[image.digest] = image
		}
	case cicdRunCorrelationFactKind:
		deployment := supplyChainDeploymentContextFromEnvelope(envelope)
		if deployment.factID != "" {
			index.deployments = append(index.deployments, deployment)
		}
	case platformMaterializationFactKind:
		lane := supplyChainDeploymentLaneContextFromEnvelope(envelope)
		if lane.repositoryID != "" && len(lane.deploymentIDs) > 0 {
			index.deploymentLanes = append(index.deploymentLanes, lane)
		}
	case workloadIdentityFactKind:
		index.workloads = append(index.workloads, supplyChainWorkloadContextsFromEnvelope(envelope)...)
	case serviceCatalogCorrelationFactKind:
		service := supplyChainServiceContextFromEnvelope(envelope)
		if service.repositoryID != "" {
			index.services = append(index.services, service)
		}
	case facts.VulnerabilityEPSSScoreFactKind:
		score, err := decodeVulnerabilityEPSSScore(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		signals := index.riskSignals[score.CVEID]
		signals.epssFactID = envelope.FactID
		signals.epssProbability = derefString(score.Probability)
		signals.epssPercentile = derefString(score.Percentile)
		index.riskSignals[score.CVEID] = signals
	case facts.VulnerabilityKnownExploitedFactKind:
		kev, err := decodeVulnerabilityKnownExploited(envelope)
		if err != nil {
			return partitionDecodeFailures(envelope, err)
		}
		signals := index.riskSignals[kev.CVEID]
		signals.kevFactID = envelope.FactID
		signals.knownExploited = true
		index.riskSignals[kev.CVEID] = signals
	}
	return quarantinedFact{}, false, nil
}

// preferSupplyChainImageIdentity picks a deterministic winner between two
// reducer_container_image_identity rows observed for the SAME digest (issue
// #5464 layer 2). The producer writes one canonical decision fact per
// triggering scope/ref with no per-digest canonicalization, so one digest can
// carry many rows -- this corpus alone has 11 for one digest -- and unlike
// the scannerAnalyses last-write-wins case above (safe because a
// content-addressed digest is identical across every writer, so any winner is
// equally correct), these rows can DISAGREE on source_repository_ids: ten
// rows here carry exactly one repository, one carries two (ambiguous). An
// unconditional last-write-wins assignment would make the winning
// RepositoryID/workload/service/environment join outcome depend on envelope
// iteration order -- an accident, not a proof -- so a defined tie-break is
// REQUIRED here, unlike the scannerAnalyses case.
//
// Preference: a row whose singleSupplyChainImageSourceRepositoryID is
// non-empty (an unambiguous git source repository) always beats a row that is
// ambiguous or names none. Between two equally-(un)ambiguous rows, the
// lexicographically smaller factID wins -- an arbitrary but STABLE choice, so
// the same winner is reached regardless of which order the envelopes arrive
// in, including across repeated runs and reprocessing.
func preferSupplyChainImageIdentity(existing, candidate supplyChainImageIdentity) supplyChainImageIdentity {
	existingUnambiguous := singleSupplyChainImageSourceRepositoryID(existing) != ""
	candidateUnambiguous := singleSupplyChainImageSourceRepositoryID(candidate) != ""
	if existingUnambiguous != candidateUnambiguous {
		if candidateUnambiguous {
			return candidate
		}
		return existing
	}
	if candidate.factID < existing.factID {
		return candidate
	}
	return existing
}

// bestSupplyChainImageIdentitiesByDigest folds every
// reducer_container_image_identity envelope in envelopes into one
// deterministic winner per digest via preferSupplyChainImageIdentity. Exposed
// as its own batch helper (rather than inlined into
// addSupplyChainImpactIndexEntry's per-envelope case above) so any future
// consumer that needs the same digest-to-repository resolution over a whole
// envelope batch can reuse this exact tie-break instead of re-deriving it.
func bestSupplyChainImageIdentitiesByDigest(envelopes []facts.Envelope) map[string]supplyChainImageIdentity {
	winners := make(map[string]supplyChainImageIdentity)
	for _, envelope := range envelopes {
		if envelope.FactKind != containerImageIdentityFactKind {
			continue
		}
		image := supplyChainImageIdentityFromEnvelope(envelope)
		if image.digest == "" {
			continue
		}
		if existing, ok := winners[image.digest]; ok {
			image = preferSupplyChainImageIdentity(existing, image)
		}
		winners[image.digest] = image
	}
	return winners
}
