// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

// AnalyzerKind identifies one scanner or reducer analyzer profile. The
// strings are frozen by #8878: the coordinator planner, the satellite
// binaries, telemetry dimensions, and status classes all key on the exact
// values asserted verbatim in vocabulary_test.go. No SDK addition carries
// this vocabulary; an SDK publication is a separate versioned contract
// change.
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

// TargetKind identifies the bounded target type passed to a scanner worker.
// The strings are frozen by #8878 and asserted verbatim in
// vocabulary_test.go.
type TargetKind string

const (
	// TargetRepository scopes scanner work to one repository generation.
	TargetRepository TargetKind = "repository"
	// TargetImage scopes scanner work to one container image digest or image
	// artifact whose subject identity is carried by the analyzer source.
	TargetImage TargetKind = "image"
	// TargetArtifact scopes scanner work to one build, release, or package
	// artifact whose subject identity is carried by the analyzer source.
	TargetArtifact TargetKind = "artifact"
)

// FailureDisposition identifies whether a failure should retry or dead-letter.
// The strings are frozen by #8878 and asserted verbatim in
// vocabulary_test.go.
type FailureDisposition string

const (
	// FailureRetryable records a failure that workflow may retry.
	FailureRetryable FailureDisposition = "retryable"
	// FailureDeadLetter records a terminal scanner-worker failure.
	FailureDeadLetter FailureDisposition = "dead_letter"
)

// FailureClass is the bounded scanner-worker failure vocabulary used in retry,
// dead-letter, and telemetry payloads. The strings are frozen by #8878 and
// asserted verbatim in vocabulary_test.go; core status renders satellite
// dead-letters against these exact values.
type FailureClass string

const (
	// FailureClassAnalyzerFailed covers analyzer process or tool failures.
	FailureClassAnalyzerFailed FailureClass = "analyzer_failed"
	// FailureClassCPULimitExceeded covers CPU budget exhaustion.
	FailureClassCPULimitExceeded FailureClass = "cpu_limit_exceeded"
	// FailureClassMemoryLimitExceeded covers memory budget exhaustion.
	FailureClassMemoryLimitExceeded FailureClass = "memory_limit_exceeded"
	// FailureClassTimeout covers analyzer timeout expiration.
	FailureClassTimeout FailureClass = "timeout"
	// FailureClassInputLimitExceeded covers oversized input payloads.
	FailureClassInputLimitExceeded FailureClass = "input_limit_exceeded"
	// FailureClassFileLimitExceeded covers target file-count overflow.
	FailureClassFileLimitExceeded FailureClass = "file_limit_exceeded"
	// FailureClassFactLimitExceeded covers excessive source fact output.
	FailureClassFactLimitExceeded FailureClass = "fact_limit_exceeded"
	// FailureClassTargetUnavailable covers missing or unreadable bounded targets.
	FailureClassTargetUnavailable FailureClass = "target_unavailable"
	// FailureClassUnsupportedTarget covers target shapes this worker cannot scan.
	FailureClassUnsupportedTarget FailureClass = "unsupported_target"
	// FailureClassSourceUnavailable covers unavailable source dependencies.
	FailureClassSourceUnavailable FailureClass = "source_unavailable"
	// FailureClassCommitFailed covers retryable persistence failures after a
	// scanner worker has produced bounded source facts.
	FailureClassCommitFailed FailureClass = "commit_failed"
)
