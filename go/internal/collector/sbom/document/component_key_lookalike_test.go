// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package document

import "testing"

// TestComponentCanonicalKeyKeepsLookalikesApart pins #9584 for parsed
// CycloneDX and SPDX documents: the duplicate-detection key folds ASCII
// letters only, so a lookalike component is not flagged as a duplicate of
// the real one, and ASCII case variants still are.
func TestComponentCanonicalKeyKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	if componentCanonicalKey("pkg:npm/\u212Aafka@1.0.0", "", "") == componentCanonicalKey("pkg:npm/kafka@1.0.0", "", "") {
		t.Error("componentCanonicalKey gives a Kelvin-sign PURL the real PURL's key")
	}
	if componentCanonicalKey("", "Ser\u0130log", "1.0.0") == componentCanonicalKey("", "Serilog", "1.0.0") {
		t.Error("componentCanonicalKey gives a lookalike name the real name's key")
	}
	if componentCanonicalKey("", "serilog", "1.0.0-\u0130") == componentCanonicalKey("", "serilog", "1.0.0-i") {
		t.Error("componentCanonicalKey gives a lookalike version the real version's key")
	}
	if componentCanonicalKey("pkg:npm/Kafka@1.0.0", "", "") != componentCanonicalKey("pkg:npm/kafka@1.0.0", "", "") {
		t.Error("componentCanonicalKey splits ASCII case variants")
	}
}
