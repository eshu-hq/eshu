// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestAnalyzerEmitsAnalysisCoverageForSupportedImageTarget(t *testing.T) {
	t.Parallel()

	layer := writeLayer(t, map[string]string{
		"etc/os-release":       "ID=alpine\nVERSION_ID=3.19.1\n",
		"lib/apk/db/installed": "P:openssl\nV:3.1.4-r5\nA:x86_64\n\n",
	})
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		ImageReference: "registry.example/team/app:1.2.3",
		ImageDigest:    "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		LayerPaths:     []string{layer},
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	analysis := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerAnalysisFactKind)
	if got, want := analysis.Payload["analysis_status"], "completed"; got != want {
		t.Fatalf("analysis_status = %#v, want %q", got, want)
	}
	if got, want := analysis.Payload["coverage_status"], "scanned"; got != want {
		t.Fatalf("coverage_status = %#v, want %q", got, want)
	}
	if got, want := analysis.Payload["target_kind"], string(scannerworker.TargetImage); got != want {
		t.Fatalf("target_kind = %#v, want %q", got, want)
	}
	if got, want := analysis.Payload["image_digest"], "sha256:11111111111111111111111111111111111111111111111111111111111111aa"; got != want {
		t.Fatalf("image_digest = %#v, want %q", got, want)
	}
	if _, exists := analysis.Payload["impact_status"]; exists {
		t.Fatalf("analysis payload includes impact_status: %#v", analysis.Payload)
	}
}

func TestAnalyzerEmitsUnsupportedEvidenceForTagOnlyImageTarget(t *testing.T) {
	t.Parallel()

	rootfs := t.TempDir()
	writeRootFSFile(t, rootfs, "etc/os-release", "ID=debian\nVERSION_ID=12\n")
	writeRootFSFile(t, rootfs, "var/lib/dpkg/status", strings.Join([]string{
		"Package: openssl",
		"Status: install ok installed",
		"Version: 3.0.11-1~deb12u2",
		"Architecture: amd64",
		"",
	}, "\n"))
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		RootFSPath:     rootfs,
		ImageReference: "registry.example/team/app:tag-only",
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil warning output", err)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	if got := countFactsByKind(result.Output.Facts, facts.VulnerabilityOSPackageFactKind); got != 0 {
		t.Fatalf("os package facts = %d, want 0 when image digest identity is missing", got)
	}
	warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)
	if got, want := warning.Payload["reason"], "image_analyzer_unsupported_target"; got != want {
		t.Fatalf("reason = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["extraction_reason"], "missing_image_digest"; got != want {
		t.Fatalf("extraction_reason = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["analysis_status"], "not_scanned"; got != want {
		t.Fatalf("analysis_status = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["coverage_status"], "unsupported"; got != want {
		t.Fatalf("coverage_status = %#v, want %q", got, want)
	}
}

func TestAnalyzerEmitsUnsupportedEvidenceForMalformedLayerArchive(t *testing.T) {
	t.Parallel()

	layerPath := filepath.Join(t.TempDir(), "layer.tar.gz")
	if err := os.WriteFile(layerPath, []byte("not a tar archive"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", layerPath, err)
	}
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		ImageReference: "registry.example/team/app:malformed",
		ImageDigest:    "sha256:55555555555555555555555555555555555555555555555555555555555555ee",
		LayerPaths:     []string{layerPath},
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil warning output", err)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)
	if got, want := warning.Payload["extraction_reason"], "malformed_image_evidence"; got != want {
		t.Fatalf("extraction_reason = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["coverage_status"], "unsupported"; got != want {
		t.Fatalf("coverage_status = %#v, want %q", got, want)
	}
}

func countFactsByKind(envelopes []facts.Envelope, kind string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			count++
		}
	}
	return count
}
