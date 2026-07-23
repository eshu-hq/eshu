// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyChainImpactLoadedEvidence carries the fact envelopes and per-stage
// fact counts SupplyChainImpactHandler.Handle's multi-stage evidence-loading
// pipeline produces, so the pipeline lives in its own function
// (loadSupplyChainImpactEvidence) while Handle stays focused on
// classification, suppression, and the write/emit tail.
type supplyChainImpactLoadedEvidence struct {
	envelopes                   []facts.Envelope
	scopeFacts                  int
	repositoryFacts             int
	manifestDependencyFacts     int
	activeEvidenceFacts         int
	activeEvidenceTruncated     bool
	osPackageAdvisoryFacts      int
	scannerAnalysisScopeFacts   int
	pythonReachabilityFacts     int
	jvmReachabilityFactCount    int
	postSecurityAlertScopeFacts int
	securityAlertScopingApplied bool
	securityAlertScopedOutFacts int
}

// loadSupplyChainImpactEvidence runs the scope-fact, repository, manifest-
// dependency, active-evidence, os-package-advisory, scanner-analysis-scope,
// Python/JVM reachability, and security-alert scoping load stages for one
// supply-chain-impact intent, in the same order and with the same per-stage
// timing SupplyChainImpactHandler.Handle recorded before this extraction. The
// os-package-advisory stage runs right after active-evidence, deriving
// candidate vendor advisory sources from the affected_package facts already
// loaded and fetching cross-scope vulnerability.os_package evidence through
// the advisory-target reader (loadSupplyChainImpactOSPackageAdvisoryFacts) —
// the only path that kind reaches this pipeline through, since
// supplyChainImpactFactKinds intentionally omits it. The scanner-analysis-
// scope stage runs right after that because it depends on the os_package
// facts the new stage (and any active-evidence os_package already present)
// loads: each os_package's own ScopeID+GenerationID (not the intent's) is
// where its sibling scanner_worker.analysis fact lives in production, so it
// is queried directly rather than through the shared active-evidence filter.
// It returns the accumulated evidence plus a timing value with every
// load-stage duration filled in; Handle continues filling the remaining
// (classification/write/emit) stages on the same value.
func (h SupplyChainImpactHandler) loadSupplyChainImpactEvidence(
	ctx context.Context,
	intent Intent,
) (supplyChainImpactLoadedEvidence, supplyChainImpactTiming, error) {
	var timing supplyChainImpactTiming

	phaseStarted := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, supplyChainImpactFactKinds())
	timing.loadScopeFactsDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact facts: %w", err)
	}
	scopeFacts := len(envelopes)

	phaseStarted = time.Now()
	repositories, err := h.loadActiveSupplyChainImpactRepositoryFacts(ctx, envelopes)
	timing.loadRepositoryFactsDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active supply chain impact repository facts: %w", err)
	}
	repositoryFacts := len(repositories)
	envelopes = append(envelopes, repositories...)

	phaseStarted = time.Now()
	manifestDependencies, err := h.loadActivePackageManifestDependencyFacts(ctx, envelopes)
	timing.loadManifestDependenciesDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active package manifest dependency facts: %w", err)
	}
	manifestDependencyFacts := len(manifestDependencies)
	envelopes = append(envelopes, manifestDependencies...)

	activeEvidenceStartCount := len(envelopes)
	phaseStarted = time.Now()
	envelopes, activeEvidenceTruncated, err := h.loadActiveSupplyChainImpactFactsUntilStable(ctx, envelopes)
	timing.loadActiveEvidenceDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active supply chain impact facts: %w", err)
	}
	activeEvidenceFacts := len(envelopes) - activeEvidenceStartCount

	osPackageAdvisoryStartCount := len(envelopes)
	phaseStarted = time.Now()
	osPackageAdvisoryEnvelopes, err := h.loadSupplyChainImpactOSPackageAdvisoryFacts(ctx, envelopes)
	timing.loadOSPackageAdvisoryDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact os package advisory facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, osPackageAdvisoryEnvelopes...)
	osPackageAdvisoryFacts := len(envelopes) - osPackageAdvisoryStartCount

	scannerAnalysisScopeStartCount := len(envelopes)
	phaseStarted = time.Now()
	scannerAnalysisScopeEnvelopes, scannerAnalysisScopeTruncated, err := h.loadSupplyChainImpactScannerAnalysisScopeFacts(ctx, envelopes)
	timing.loadScannerAnalysisScopeDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact scanner analysis scope facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, scannerAnalysisScopeEnvelopes...)
	scannerAnalysisScopeFacts := len(envelopes) - scannerAnalysisScopeStartCount
	activeEvidenceTruncated = activeEvidenceTruncated || scannerAnalysisScopeTruncated

	pythonReachabilityStartCount := len(envelopes)
	phaseStarted = time.Now()
	pythonReachabilityEvidence, err := h.loadPythonReachabilityEvidenceFacts(ctx, envelopes)
	timing.loadPythonReachabilityDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load Python reachability evidence facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, pythonReachabilityEvidence...)
	pythonReachabilityFacts := len(envelopes) - pythonReachabilityStartCount

	jvmReachabilityStartCount := len(envelopes)
	phaseStarted = time.Now()
	jvmReachabilityFacts, err := h.loadActiveJVMReachabilityFacts(ctx, envelopes)
	timing.loadJVMReachabilityDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active JVM reachability facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, jvmReachabilityFacts...)
	jvmReachabilityFactCount := len(envelopes) - jvmReachabilityStartCount

	preSecurityAlertScopeFacts := len(envelopes)
	phaseStarted = time.Now()
	securityAlertScopingApplied := supplyChainImpactUsesSecurityAlertScope(intent, envelopes)
	if securityAlertScopingApplied {
		envelopes = scopeSupplyChainImpactEvidenceToSecurityAlerts(envelopes)
	}
	timing.securityAlertScopingDuration = time.Since(phaseStarted)
	postSecurityAlertScopeFacts := len(envelopes)
	securityAlertScopedOutFacts := 0
	if securityAlertScopingApplied && preSecurityAlertScopeFacts > postSecurityAlertScopeFacts {
		securityAlertScopedOutFacts = preSecurityAlertScopeFacts - postSecurityAlertScopeFacts
	}

	return supplyChainImpactLoadedEvidence{
		envelopes:                   envelopes,
		scopeFacts:                  scopeFacts,
		repositoryFacts:             repositoryFacts,
		manifestDependencyFacts:     manifestDependencyFacts,
		activeEvidenceFacts:         activeEvidenceFacts,
		activeEvidenceTruncated:     activeEvidenceTruncated,
		osPackageAdvisoryFacts:      osPackageAdvisoryFacts,
		scannerAnalysisScopeFacts:   scannerAnalysisScopeFacts,
		pythonReachabilityFacts:     pythonReachabilityFacts,
		jvmReachabilityFactCount:    jvmReachabilityFactCount,
		postSecurityAlertScopeFacts: postSecurityAlertScopeFacts,
		securityAlertScopingApplied: securityAlertScopingApplied,
		securityAlertScopedOutFacts: securityAlertScopedOutFacts,
	}, timing, nil
}
