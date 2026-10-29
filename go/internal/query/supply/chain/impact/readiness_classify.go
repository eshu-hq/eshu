// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "slices"

const missingCommittedNVDSnapshotReason = "nvd:committed_snapshot_missing"

func classifyReadinessState(
	findings []FindingResult,
	sources []EvidenceFamily,
	snapshot ReadinessSnapshot,
	missing []string,
) ReadinessState {
	if len(findings) > 0 {
		return ReadinessStateReadyWithFindings
	}
	advisoryCount := evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory)
	// A committed coverage gap stays incomplete even when a CVE advisory fact
	// exists: advisory presence does not prove this configured NVD target ran.
	// Keep the legacy target_incomplete flag advisory-guarded for unrelated
	// in-flight sources.
	if snapshot.TargetIncomplete && (advisoryCount == 0 ||
		committedNVDCoverageMissing(snapshot) ||
		sourceStatesIncomplete(snapshot.SourceStates) || sourceSnapshotsIncomplete(snapshot.SourceSnapshots)) {
		return ReadinessStateTargetIncomplete
	}
	if advisoryCount == 0 &&
		len(snapshot.SourceStates) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyPackageConsumption) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyPackageRegistry) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMComponent) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyContainerImageIdentity) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyScannerWorkerAnalysis) == 0 {
		// A scanner_worker.analysis fact is real observed coverage for the
		// requested image even when every other family is empty (issue
		// #5467): the OS-package scan tier can be the ONLY evidence Eshu has
		// for a scanned image, and that must not read as "not configured".
		return ReadinessStateNotConfigured
	}
	// Unsupported target evidence outranks both evidence_incomplete AND
	// ready_zero_findings: when Eshu observed real target evidence the
	// matcher cannot resolve, the answer is not "we are missing data" or
	// "we ran cleanly" but "we have data we cannot match." Surfacing
	// ready_zero_findings here would let callers read the response as
	// clean while a real coverage gap exists. Findings still outrank
	// unsupported (handled by the early ready_with_findings return above)
	// because an admitted finding is the reducer's positive verdict;
	// unsupported_targets[] is then surfaced additively so the gap is
	// still visible.
	if len(snapshot.UnsupportedTargets) > 0 {
		return ReadinessStateUnsupported
	}
	if len(missing) > 0 {
		return ReadinessStateEvidenceIncomplete
	}
	return ReadinessStateReadyZeroFindings
}

func classifyMissingEvidence(
	scope TargetScope,
	sources []EvidenceFamily,
	snapshot ReadinessSnapshot,
) []string {
	var missing []string
	if snapshot.TargetIncomplete &&
		(evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory) == 0 ||
			committedNVDCoverageMissing(snapshot) ||
			sourceStatesIncomplete(snapshot.SourceStates) || sourceSnapshotsIncomplete(snapshot.SourceSnapshots)) {
		missing = append(missing, MissingEvidenceTargetCollection)
	}
	if evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory) == 0 &&
		!sourceStatesHaveCoveredSuccess(snapshot.SourceStates) {
		missing = append(missing, MissingEvidenceAdvisorySources)
	}
	if advisoryFreshness := evidenceFamilyFreshness(sources, EvidenceFamilyVulnerabilityAdvisory); advisoryFreshness == FreshnessLabelStale {
		missing = append(missing, MissingEvidenceAdvisorySources)
	}
	// package.registry without a package_id anchor is global metadata, not
	// proof of consumption for the requested repository. Repo-only scopes
	// must demonstrate ownership through package.consumption (manifest or
	// reducer correlation); registry counts only satisfy owned_packages when
	// the request is anchored on a specific package.
	if scopeRequiresOwnedPackages(scope) {
		consumption := evidenceFactCount(sources, EvidenceFamilyPackageConsumption)
		registry := evidenceFactCount(sources, EvidenceFamilyPackageRegistry)
		switch {
		case consumption == 0 && scope.PackageID == "":
			missing = append(missing, MissingEvidenceOwnedPackages)
		case consumption == 0 && registry == 0:
			missing = append(missing, MissingEvidenceOwnedPackages)
		}
	}
	if scopeRequiresPackageRegistryMetadata(scope, sources) {
		registryFreshness := evidenceFamilyFreshness(sources, EvidenceFamilyPackageRegistry)
		if evidenceFactCount(sources, EvidenceFamilyPackageRegistry) == 0 ||
			registryFreshness == FreshnessLabelStale ||
			registryFreshness == FreshnessLabelUnknown {
			missing = append(missing, MissingEvidencePackageRegistryMetadata)
		}
	}
	if scopeRequiresImageEvidence(scope) &&
		evidenceFactCount(sources, EvidenceFamilyContainerImageIdentity) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMComponent) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMAttestation) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyScannerWorkerAnalysis) == 0 {
		// A scanner_worker.analysis fact for the requested image proves the
		// OS-package scan tier ran, independent of SBOM/image-identity
		// evidence (issue #5467). vulnerability.os_package is deliberately
		// NOT checked here: a completed scan can legitimately find zero
		// installed packages (a distroless image), so os_package==0 must not
		// reopen this gate once scanner_worker.analysis already proves the
		// scan happened.
		missing = append(missing, MissingEvidenceSBOMOrImage)
	}
	if scopeRequiresServiceOrWorkloadEvidence(scope) {
		missing = append(missing, ServiceCatalogAnchorMissingReason)
	}
	if len(snapshot.UnsupportedTargets) > 0 {
		missing = append(missing, MissingEvidenceUnsupportedTargets)
	}
	return uniqueSortedReadinessStrings(missing)
}

func committedNVDCoverageMissing(snapshot ReadinessSnapshot) bool {
	return slices.Contains(snapshot.IncompleteReasons, missingCommittedNVDSnapshotReason)
}

func isReadyState(state ReadinessState) bool {
	switch state {
	case ReadinessStateReadyWithFindings, ReadinessStateReadyZeroFindings:
		return true
	default:
		return false
	}
}
