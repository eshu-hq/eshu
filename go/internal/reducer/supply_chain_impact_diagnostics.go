// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

// supplyChainImpactTiming records success-path phase timings for the
// supply_chain_impact reducer domain. These wrappers measure existing work only:
// they do not alter fact loading, matching, durable writes, or counter emission.
type supplyChainImpactTiming struct {
	loadScopeFactsDuration           time.Duration
	loadRepositoryFactsDuration      time.Duration
	loadManifestDependenciesDuration time.Duration
	loadActiveEvidenceDuration       time.Duration
	loadScannerAnalysisScopeDuration time.Duration
	loadPythonReachabilityDuration   time.Duration
	loadJVMReachabilityDuration      time.Duration
	securityAlertScopingDuration     time.Duration
	buildFindingsDuration            time.Duration
	evaluateSuppressionsDuration     time.Duration
	writeFindingsDuration            time.Duration
	emitCountersDuration             time.Duration
	totalDuration                    time.Duration
}

func supplyChainImpactSubDurations(t supplyChainImpactTiming) map[string]float64 {
	return map[string]float64{
		"load_scope_facts":            t.loadScopeFactsDuration.Seconds(),
		"load_repository_facts":       t.loadRepositoryFactsDuration.Seconds(),
		"load_manifest_dependencies":  t.loadManifestDependenciesDuration.Seconds(),
		"load_active_evidence":        t.loadActiveEvidenceDuration.Seconds(),
		"load_scanner_analysis_scope": t.loadScannerAnalysisScopeDuration.Seconds(),
		"load_python_reachability":    t.loadPythonReachabilityDuration.Seconds(),
		"load_jvm_reachability":       t.loadJVMReachabilityDuration.Seconds(),
		"security_alert_scoping":      t.securityAlertScopingDuration.Seconds(),
		"build_findings":              t.buildFindingsDuration.Seconds(),
		"evaluate_suppressions":       t.evaluateSuppressionsDuration.Seconds(),
		"write_findings":              t.writeFindingsDuration.Seconds(),
		"emit_counters":               t.emitCountersDuration.Seconds(),
		"total":                       t.totalDuration.Seconds(),
	}
}

func supplyChainImpactDiagnosticSignals(
	scopeFacts int,
	repositoryFacts int,
	manifestDependencyFacts int,
	activeEvidenceFacts int,
	scannerAnalysisScopeFacts int,
	pythonReachabilityFacts int,
	jvmReachabilityFacts int,
	postScopeFacts int,
	securityAlertScopingApplied bool,
	securityAlertScopedOutFacts int,
	findings int,
	activeEvidenceTruncated bool,
	writtenRows int,
) map[string]float64 {
	inputReady := scopeFacts+
		repositoryFacts+
		manifestDependencyFacts+
		activeEvidenceFacts+
		scannerAnalysisScopeFacts+
		pythonReachabilityFacts+
		jvmReachabilityFacts > 0
	signals := materializationDiagnosticSignals(inputReady, writtenRows)
	signals["scope_facts"] = float64(scopeFacts)
	signals["repository_facts"] = float64(repositoryFacts)
	signals["manifest_dependency_facts"] = float64(manifestDependencyFacts)
	signals["active_evidence_facts"] = float64(activeEvidenceFacts)
	signals["scanner_analysis_scope_facts"] = float64(scannerAnalysisScopeFacts)
	signals["python_reachability_facts"] = float64(pythonReachabilityFacts)
	signals["jvm_reachability_facts"] = float64(jvmReachabilityFacts)
	signals["post_scope_facts"] = float64(postScopeFacts)
	signals["security_alert_scoping_applied"] = boolSignal(securityAlertScopingApplied)
	signals["security_alert_scoped_out_facts"] = float64(securityAlertScopedOutFacts)
	signals["findings"] = float64(findings)
	signals["active_evidence_truncated"] = boolSignal(activeEvidenceTruncated)
	return signals
}

func boolSignal(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
