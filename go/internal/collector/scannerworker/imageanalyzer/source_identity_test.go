// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestAnalysisFactCarriesSatelliteSourceIdentity proves the analysis fact
// carries the same sanitized source identity as its sibling package facts:
// the SDK satellite contract requires source_ref.uri and record_id on every
// fact, so the analysis fact must not leave them empty while package facts
// set them.
func TestAnalysisFactCarriesSatelliteSourceIdentity(t *testing.T) {
	t.Parallel()

	layer := writeLayer(t, map[string]string{
		"etc/os-release":       "ID=alpine\nVERSION_ID=3.19.1\n",
		"etc/apk/repositories": "https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n",
		"lib/apk/db/installed": "P:openssl\nV:3.1.4-r5\nA:x86_64\no:openssl\n\n",
	})
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		ImageReference: "registry.example/team/app:1.2.3",
		ImageDigest:    "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		LayerPaths:     []string{layer},
		SourceURI:      "oci://registry.example/team/app@sha256:11111111111111111111111111111111111111111111111111111111111111aa?token=redacted",
		SourceRecordID: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
	})
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	analysis := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerAnalysisFactKind)
	pkg := firstFactByKind(t, result.Output.Facts, chain.VulnerabilityOSPackageFactKind)
	if analysis.SourceRef.SourceURI == "" {
		t.Fatal("analysis SourceRef.SourceURI is empty, want sanitized snapshot URI")
	}
	if strings.Contains(analysis.SourceRef.SourceURI, "token=redacted") {
		t.Fatalf("analysis SourceRef.SourceURI leaked credentials: %q", analysis.SourceRef.SourceURI)
	}
	if analysis.SourceRef.SourceURI != pkg.SourceRef.SourceURI {
		t.Fatalf("analysis SourceURI %q != sibling package SourceURI %q", analysis.SourceRef.SourceURI, pkg.SourceRef.SourceURI)
	}
	// Sibling package facts carry per-package records, so the analysis fact
	// only needs its own non-empty snapshot record, not equality with them.
	if analysis.SourceRef.SourceRecordID != "sha256:11111111111111111111111111111111111111111111111111111111111111aa" {
		t.Fatalf("analysis SourceRecordID = %q, want snapshot record", analysis.SourceRef.SourceRecordID)
	}
	if pkg.SourceRef.SourceRecordID == "" {
		t.Fatal("sibling package SourceRecordID is empty, want per-package record")
	}
}

// TestUnsupportedWarningCarriesSatelliteSourceIdentity proves the
// unsupported-target warning carries the sanitized snapshot URI. Its record
// stays empty when no digest exists — that path remains satellite
// inadmissible until a record identity exists for unscanned targets.
func TestUnsupportedWarningCarriesSatelliteSourceIdentity(t *testing.T) {
	t.Parallel()

	rootfs := t.TempDir()
	writeRootFSFile(t, rootfs, "etc/os-release", "ID=alpine\nVERSION_ID=3.19.1\n")
	writeRootFSFile(t, rootfs, "etc/apk/repositories", "https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n")
	writeRootFSFile(t, rootfs, "lib/apk/db/installed", "P:openssl\nV:3.1.4-r5\nA:x86_64\no:openssl\n\n")
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		RootFSPath:     rootfs,
		ImageReference: "registry.example/team/app:1.2.3",
		SourceURI:      "oci://registry.example/team/app?token=redacted",
		SourceRecordID: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
	})
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)
	if warning.SourceRef.SourceURI == "" {
		t.Fatal("warning SourceRef.SourceURI is empty, want sanitized snapshot URI")
	}
	if strings.Contains(warning.SourceRef.SourceURI, "token=redacted") {
		t.Fatalf("warning SourceRef.SourceURI leaked credentials: %q", warning.SourceRef.SourceURI)
	}
	if warning.SourceRef.SourceRecordID != "sha256:11111111111111111111111111111111111111111111111111111111111111aa" {
		t.Fatalf("warning SourceRecordID = %q, want target record", warning.SourceRef.SourceRecordID)
	}
}
