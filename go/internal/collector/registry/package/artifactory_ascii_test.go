// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"testing"
)

// TestNormalizeArtifactoryPackageTypeKeepsLookalikesApart pins that the
// Artifactory package-type mapping folds ASCII letters only (#9622): a
// U+0130 (İ) or U+212A (K) spelling is not routed to another ecosystem the
// way strings.ToLower would route it.
func TestNormalizeArtifactoryPackageTypeKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	// A lookalike must not route into a known ecosystem: the old
	// strings.ToLower switch mapped "PYPİ" onto pypi and "GENERİC" onto
	// generic.
	for lookalike, routed := range map[string]Ecosystem{
		"PYPİ":    EcosystemPyPI,
		"GENERİC": EcosystemGeneric,
	} {
		if got := normalizeArtifactoryPackageType(lookalike); got == routed {
			t.Errorf("normalizeArtifactoryPackageType(%q) = %q, joins a known ecosystem", lookalike, got)
		}
	}
	for raw, want := range map[string]Ecosystem{
		"NPM":    EcosystemNPM,
		"PyPI":   EcosystemPyPI,
		"MAVEN":  EcosystemMaven,
		"Gradle": EcosystemMaven,
	} {
		if got := normalizeArtifactoryPackageType(raw); got != want {
			t.Errorf("normalizeArtifactoryPackageType(%q) = %q, want %q", raw, got, want)
		}
	}
}
