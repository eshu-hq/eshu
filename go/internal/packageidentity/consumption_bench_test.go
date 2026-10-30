// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "testing"

// BenchmarkConsumptionKeysCorpus measures the manifest write-path key builder
// over the ASCII corpus, the shape every manifest dependency fact pays.
func BenchmarkConsumptionKeysCorpus(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, entry := range asciiNameCorpus {
			_ = ConsumptionKeys(entry.Ecosystem, entry.Name)
		}
	}
}

// BenchmarkManifestConsumptionKeysNamespaced measures the two-name form the
// sidecar writer passes for a manifest row that carries a namespace.
func BenchmarkManifestConsumptionKeysNamespaced(b *testing.B) {
	b.ReportAllocs()
	input := ManifestKeyInput{PackageManager: "npm", Names: []string{"Core", "@Babel/Core"}}
	for b.Loop() {
		_ = ManifestConsumptionKeys(input)
	}
}
