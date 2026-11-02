// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import "testing"

// BenchmarkBuildSectionsKeyBody is the worst case for the run scan (#9799): one
// label, then 499 lines of a PEM body, so every region is withheld and every
// region is read by the token checks.
func BenchmarkBuildSectionsKeyBody(b *testing.B) {
	lines := append([]string{"-----BEGIN RSA PRIVATE KEY-----"}, randomLines(11, base64Alphabet, 499, 64, 0)...)
	benchmarkBuildSections(b, stackedRegions(lines...))
}

// BenchmarkBuildSectionsTokenLinesNoLabel is 500 token-shaped lines with no
// label. Each one is a seed, so the scan reads every line to its end.
func BenchmarkBuildSectionsTokenLinesNoLabel(b *testing.B) {
	benchmarkBuildSections(b, stackedRegions(randomLines(12, base64Alphabet, 500, 64, 0)...))
}
