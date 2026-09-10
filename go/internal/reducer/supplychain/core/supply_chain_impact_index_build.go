// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// buildSupplyChainImpactIndexWithQuarantine is the quarantine-aware index
// builder buildSupplyChainImpactFindingsWithQuarantine calls directly so the
// reducer intent path can report each malformed vulnerability.* fact as a
// visible input_invalid dead-letter via factdecode.RecordQuarantinedFacts. Seven kinds
// decode through the typed contracts seam via addSupplyChainImpactIndexEntry:
// vulnerability.cve, .affected_package, .affected_product, .os_package,
// .epss_score, .known_exploited, and scanner_worker.analysis
// (go_module_evidence/go_call_reachability decode separately, through
// classifyGoVulnerabilityReachabilityWithQuarantine below). A fact missing a
// required identity field is routed through
// factdecode.PartitionDecodeFailures into the returned quarantine list rather than
// silently excluded from the index with no operator signal; any OTHER decode
// error (a transient condition factdecode.PartitionDecodeFailures does not quarantine)
// is returned fatally so the caller fails the whole intent for durable
// triage.
func buildSupplyChainImpactIndexWithQuarantine(envelopes []facts.Envelope) (supplyChainImpactIndex, []factdecode.QuarantinedFact, error) {
	index := supplyChainImpactIndex{
		affectedPackages:        map[string][]supplychainmodel.AffectedPackage{},
		affectedProducts:        map[string][]supplychainmodel.AffectedProduct{},
		consumption:             map[string][]supplychainmodel.PackageConsumption{},
		osPackages:              map[string][]supplychainmodel.OSPackage{},
		attachments:             map[string]supplychainmodel.Attachment{},
		images:                  map[string]supplyChainImageIdentity{},
		riskSignals:             map[string]supplychainmodel.RiskSignals{},
		scannerAnalyses:         map[string]supplychainmodel.ScannerAnalysis{},
		goReachability:          map[string]GoVulnerabilityFinding{},
		jsTSPackageReachability: buildJSTSPackageReachabilityIndex(envelopes),
		pythonReachability:      map[string]pythonReachabilityRepositoryEvidence{},
		jvmReachability:         buildJVMReachabilityIndex(envelopes),
	}
	// Computed once over the full batch (#5887): the consensus-aware winner
	// pick addSupplyChainImpactIndexEntry uses for reducercontract.ContainerImageIdentityFactKind
	// needs every row's tier+repository up front to count corroboration, not
	// just the two rows being compared at this instant. See
	// preferSupplyChainImageIdentityConsensus's doc for why.
	imageConsensus := buildSupplyChainImageIdentityConsensus(envelopes)
	var quarantined []factdecode.QuarantinedFact
	for _, envelope := range envelopes {
		q, isQuarantine, fatal := addSupplyChainImpactIndexEntry(&index, envelope, imageConsensus)
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
		return index.cves[i].CVEID < index.cves[j].CVEID
	})
	return index, quarantined, nil
}

// addSupplyChainImpactIndexEntry decodes/projects one envelope and indexes it
// in place, dispatching on FactKind. It returns a factdecode.QuarantinedFact and
// isQuarantine=true for a per-fact input_invalid decode failure (the caller
// records it and continues with the next envelope); any other decode error is
// returned as fatal so the caller fails the whole intent. An envelope kind
// this index does not track is a silent no-op, matching the pre-typing
// switch's default behavior.
func addSupplyChainImpactIndexEntry(
	index *supplyChainImpactIndex,
	envelope facts.Envelope,
	imageConsensus map[supplyChainImageIdentityConsensusKey]int,
) (factdecode.QuarantinedFact, bool, error) {
	switch envelope.FactKind {
	case facts.VulnerabilityCVEFactKind:
		cve, err := supplyChainCVEFromEnvelope(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		if cve.CVEID != "" {
			index.cves = append(index.cves, cve)
		}
	case facts.VulnerabilityAffectedPackageFactKind:
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		if pkg.CVEID != "" {
			index.affectedPackages[pkg.CVEID] = append(index.affectedPackages[pkg.CVEID], pkg)
		}
	case facts.VulnerabilityAffectedProductFactKind:
		product, err := supplyChainAffectedProductFromEnvelope(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		if product.CVEID != "" && product.Criteria != "" && product.Vulnerable {
			index.affectedProducts[product.CVEID] = append(index.affectedProducts[product.CVEID], product)
		}
	case correlation.PackageConsumptionFactKind:
		consumption, err := supplyChainConsumptionFromEnvelope(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		if consumption.PackageID != "" {
			index.consumption[consumption.PackageID] = append(index.consumption[consumption.PackageID], consumption)
		}
	case facts.VulnerabilityOSPackageFactKind:
		pkg, err := supplyChainOSPackageFromEnvelope(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		if pkg.PackageID != "" && pkg.VendorAdvisorySource != "" && pkg.RepositoryClass == "vendor" {
			index.osPackages[pkg.PackageID] = append(index.osPackages[pkg.PackageID], pkg)
		}
	case facts.ScannerWorkerAnalysisFactKind:
		analysis, err := supplyChainScannerAnalysisFromEnvelope(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
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
		if analysis.ImageDigest != "" {
			index.scannerAnalyses[supplychainmodel.ScopeGenerationKey(analysis.ScopeID, analysis.GenerationID)] = analysis
		}
	case facts.SBOMComponentFactKind:
		component := supplyChainSBOMComponentFromEnvelope(envelope)
		if component.PURL != "" || component.PackageID != "" || component.CPE != "" {
			index.components = append(index.components, component)
		}
	case reducercontract.SBOMAttestationAttachmentFactKind:
		attachment := supplyChainAttachmentFromEnvelope(envelope)
		if attachment.DocumentID != "" {
			index.attachments[attachment.DocumentID] = attachment
		}
	case reducercontract.ContainerImageIdentityFactKind:
		image := supplyChainImageIdentityFromEnvelope(envelope)
		if image.digest != "" {
			if existing, ok := index.images[image.digest]; ok {
				image = preferSupplyChainImageIdentityConsensus(existing, image, imageConsensus)
			}
			index.images[image.digest] = image
		}
	case cicdrun.CICDRunCorrelationFactKind:
		deployment := supplyChainDeploymentContextFromEnvelope(envelope)
		if deployment.FactID != "" {
			index.deployments = append(index.deployments, deployment)
		}
	case reducercontract.PlatformMaterializationFactKind:
		lane := supplyChainDeploymentLaneContextFromEnvelope(envelope)
		if lane.RepositoryID != "" && len(lane.DeploymentIDs) > 0 {
			index.deploymentLanes = append(index.deploymentLanes, lane)
		}
	case reducercontract.WorkloadIdentityFactKind:
		index.workloads = append(index.workloads, supplyChainWorkloadContextsFromEnvelope(envelope)...)
	case reducercontract.ServiceCatalogCorrelationFactKind:
		service := supplyChainServiceContextFromEnvelope(envelope)
		if service.RepositoryID != "" {
			index.services = append(index.services, service)
		}
	case facts.VulnerabilityEPSSScoreFactKind:
		score, err := schemadecode.DecodeVulnerabilityEPSSScore(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		signals := index.riskSignals[score.CVEID]
		signals.EPSSFactID = envelope.FactID
		signals.EPSSProbability = payloadcore.DerefString(score.Probability)
		signals.EPSSPercentile = payloadcore.DerefString(score.Percentile)
		index.riskSignals[score.CVEID] = signals
	case facts.VulnerabilityKnownExploitedFactKind:
		kev, err := schemadecode.DecodeVulnerabilityKnownExploited(envelope)
		if err != nil {
			return factdecode.PartitionDecodeFailures(envelope, err)
		}
		signals := index.riskSignals[kev.CVEID]
		signals.KEVFactID = envelope.FactID
		signals.KnownExploited = true
		index.riskSignals[kev.CVEID] = signals
	}
	return factdecode.QuarantinedFact{}, false, nil
}

// preferSupplyChainImageIdentity, supplyChainImageIdentityAnchorTier, and
// bestSupplyChainImageIdentitiesByDigest live in
// supply_chain_impact_anchor_tier.go (split out to keep this file under the
// repo's 500-line cap).
