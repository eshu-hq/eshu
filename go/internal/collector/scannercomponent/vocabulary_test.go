// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"testing"
)

// TestVocabularyStringsArePinnedVerbatim is the #8878 frozen-vocabulary
// contract: every AnalyzerKind, TargetKind, FailureClass, FailureDisposition,
// ExecutionLane, component ID, manifest version, and protocol string is
// asserted with its exact wire value. The coordinator planner, the satellite
// binaries, telemetry dimensions, and status classes all key on these exact
// strings, so a rename, removal, or value change breaks this test on purpose.
// This supersedes the extraction-census string check that lived in the
// removed in-tree scanner-worker command's census test.
func TestVocabularyStringsArePinnedVerbatim(t *testing.T) {
	t.Parallel()

	for kind, want := range map[AnalyzerKind]string{
		AnalyzerSBOMGeneration:        "sbom_generation",
		AnalyzerImageUnpacking:        "image_unpacking",
		AnalyzerSourceAnalysis:        "source_analysis",
		AnalyzerOSPackageExtraction:   "os_package_extraction",
		AnalyzerSecretScan:            "secret_scan",
		AnalyzerLicenseScan:           "license_scan",
		AnalyzerMisconfigurationScan:  "misconfiguration_scan",
		AnalyzerVulnerabilityMatching: "vulnerability_matching",
		AnalyzerCoverageReadiness:     "coverage_readiness",
		AnalyzerSecurityPriority:      "security_priority",
	} {
		if string(kind) != want {
			t.Errorf("AnalyzerKind = %q, want pinned %q", string(kind), want)
		}
	}
	for kind, want := range map[TargetKind]string{
		TargetRepository: "repository",
		TargetImage:      "image",
		TargetArtifact:   "artifact",
	} {
		if string(kind) != want {
			t.Errorf("TargetKind = %q, want pinned %q", string(kind), want)
		}
	}
	for class, want := range map[FailureClass]string{
		FailureClassAnalyzerFailed:      "analyzer_failed",
		FailureClassCPULimitExceeded:    "cpu_limit_exceeded",
		FailureClassMemoryLimitExceeded: "memory_limit_exceeded",
		FailureClassTimeout:             "timeout",
		FailureClassInputLimitExceeded:  "input_limit_exceeded",
		FailureClassFileLimitExceeded:   "file_limit_exceeded",
		FailureClassFactLimitExceeded:   "fact_limit_exceeded",
		FailureClassTargetUnavailable:   "target_unavailable",
		FailureClassUnsupportedTarget:   "unsupported_target",
		FailureClassSourceUnavailable:   "source_unavailable",
		FailureClassCommitFailed:        "commit_failed",
	} {
		if string(class) != want {
			t.Errorf("FailureClass = %q, want pinned %q", string(class), want)
		}
	}
	for disposition, want := range map[FailureDisposition]string{
		FailureRetryable:  "retryable",
		FailureDeadLetter: "dead_letter",
	} {
		if string(disposition) != want {
			t.Errorf("FailureDisposition = %q, want pinned %q", string(disposition), want)
		}
	}
	for lane, want := range map[ExecutionLane]string{
		LaneScannerWorker: "scanner_worker",
		LaneReducer:       "reducer",
	} {
		if string(lane) != want {
			t.Errorf("ExecutionLane = %q, want pinned %q", string(lane), want)
		}
	}
	if IDImage != "eshu.scanner.image" {
		t.Errorf("IDImage = %q, want pinned %q", IDImage, "eshu.scanner.image")
	}
	if IDSBOM != "eshu.scanner.sbom" {
		t.Errorf("IDSBOM = %q, want pinned %q", IDSBOM, "eshu.scanner.sbom")
	}
	if IDOSRuntime != "eshu.scanner.osruntime" {
		t.Errorf("IDOSRuntime = %q, want pinned %q", IDOSRuntime, "eshu.scanner.osruntime")
	}
	if Version != "0.1.0" {
		t.Errorf("Version = %q, want pinned %q", Version, "0.1.0")
	}
	if CollectorSDKProtocolV1Alpha3 != "collector-sdk/v1alpha3" {
		t.Errorf("CollectorSDKProtocolV1Alpha3 = %q, want pinned %q", CollectorSDKProtocolV1Alpha3, "collector-sdk/v1alpha3")
	}
}

// TestVocabularyLaneRoutingIsPinned pins which analyzers the admission half
// serves: the scanner-lane analyzers resolve to the scanner lane, every
// reducer-lane analyzer resolves to the reducer lane, and unknown analyzers
// stay unknown.
func TestVocabularyLaneRoutingIsPinned(t *testing.T) {
	t.Parallel()

	for _, kind := range []AnalyzerKind{
		AnalyzerSBOMGeneration,
		AnalyzerImageUnpacking,
		AnalyzerOSPackageExtraction,
		AnalyzerSourceAnalysis,
		AnalyzerSecretScan,
		AnalyzerLicenseScan,
		AnalyzerMisconfigurationScan,
	} {
		lane, ok := AnalyzerLane(kind)
		if !ok {
			t.Errorf("AnalyzerLane(%q) ok = false, want the scanner lane", string(kind))
			continue
		}
		if lane != LaneScannerWorker {
			t.Errorf("AnalyzerLane(%q) = %q, want pinned %q", string(kind), string(lane), string(LaneScannerWorker))
		}
	}
	for _, kind := range []AnalyzerKind{
		AnalyzerVulnerabilityMatching,
		AnalyzerCoverageReadiness,
		AnalyzerSecurityPriority,
	} {
		lane, ok := AnalyzerLane(kind)
		if !ok {
			t.Errorf("AnalyzerLane(%q) ok = false, want the reducer lane", string(kind))
			continue
		}
		if lane != LaneReducer {
			t.Errorf("AnalyzerLane(%q) = %q, want pinned %q", string(kind), string(lane), string(LaneReducer))
		}
	}
	if _, ok := AnalyzerLane("no_such_analyzer"); ok {
		t.Error("AnalyzerLane(unknown) ok = true, want false")
	}
}

// TestArtifactDigestPinsArePinnedVerbatim pins the published image digests
// the admission half enforces: any republish must update these pins together
// with the Scanner-Publish-SHA anchor and the chart seed.image entries in a
// reviewed follow-up PR.
func TestArtifactDigestPinsArePinnedVerbatim(t *testing.T) {
	t.Parallel()

	for id, want := range map[string]string{
		IDImage:     "0945164c39f3e392e64b6640e2c5de023b591a0bea9e72dabaac91fa04c461ee",
		IDSBOM:      "19b1def7f3649bb2efb3d53d8a8336dba2ae019233a8cb2d58cf0dd854fa3dca",
		IDOSRuntime: "9f81bd96390a50ecc1deac8757103e6282e27ba2689b0bb9ec2028ae8a561056",
	} {
		got, ok := artifactDigests[id]
		if !ok {
			t.Errorf("artifactDigests[%q] missing, want the published pin", id)
			continue
		}
		if got != want {
			t.Errorf("artifactDigests[%q] = %q, want pinned %q", id, got, want)
		}
	}
}
