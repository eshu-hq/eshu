// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

// AnalyzerKind identifies one scanner or reducer analyzer profile.
type AnalyzerKind string

const (
	// AnalyzerSBOMGeneration extracts or builds SBOM source evidence.
	AnalyzerSBOMGeneration AnalyzerKind = "sbom_generation"
	// AnalyzerImageUnpacking unpacks images or filesystem layers for source evidence.
	AnalyzerImageUnpacking AnalyzerKind = "image_unpacking"
	// AnalyzerSourceAnalysis runs CPU-heavy source analyzers.
	AnalyzerSourceAnalysis AnalyzerKind = "source_analysis"
	// AnalyzerOSPackageExtraction extracts OS package inventories.
	AnalyzerOSPackageExtraction AnalyzerKind = "os_package_extraction"
	// AnalyzerSecretScan runs source or artifact secret scanning.
	AnalyzerSecretScan AnalyzerKind = "secret_scan"
	// AnalyzerLicenseScan runs source or artifact license scanning.
	AnalyzerLicenseScan AnalyzerKind = "license_scan"
	// AnalyzerMisconfigurationScan runs configuration analyzers.
	AnalyzerMisconfigurationScan AnalyzerKind = "misconfiguration_scan"
	// AnalyzerVulnerabilityMatching is reducer-owned vulnerability matching.
	AnalyzerVulnerabilityMatching AnalyzerKind = "vulnerability_matching"
	// AnalyzerCoverageReadiness is reducer-owned coverage readiness analysis.
	AnalyzerCoverageReadiness AnalyzerKind = "coverage_readiness"
	// AnalyzerSecurityPriority is reducer-owned prioritization analysis.
	AnalyzerSecurityPriority AnalyzerKind = "security_priority"
)

// ExecutionLane identifies the runtime owner for an analyzer profile.
type ExecutionLane string

const (
	// LaneScannerWorker runs outside reducers with isolated CPU and memory limits.
	LaneScannerWorker ExecutionLane = "scanner_worker"
	// LaneReducer remains the truth owner for finding admission and prioritization.
	LaneReducer ExecutionLane = "reducer"
)

var analyzerLanes = map[AnalyzerKind]ExecutionLane{
	AnalyzerSBOMGeneration:        LaneScannerWorker,
	AnalyzerImageUnpacking:        LaneScannerWorker,
	AnalyzerSourceAnalysis:        LaneScannerWorker,
	AnalyzerOSPackageExtraction:   LaneScannerWorker,
	AnalyzerSecretScan:            LaneScannerWorker,
	AnalyzerLicenseScan:           LaneScannerWorker,
	AnalyzerMisconfigurationScan:  LaneScannerWorker,
	AnalyzerVulnerabilityMatching: LaneReducer,
	AnalyzerCoverageReadiness:     LaneReducer,
	AnalyzerSecurityPriority:      LaneReducer,
}

// AnalyzerLane returns the execution lane for an analyzer profile.
func AnalyzerLane(analyzer AnalyzerKind) (ExecutionLane, bool) {
	lane, ok := analyzerLanes[analyzer]
	return lane, ok
}
