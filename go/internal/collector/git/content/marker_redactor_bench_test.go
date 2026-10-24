// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

// BenchmarkRedactMarker measures the marker redactor on three body shapes: a
// typical .yarnrc.yml, a 1 MiB body with no credential, and a 1 MiB body where
// every fifth line holds a credential. The redactor runs once per governed
// file per collection (snapshot and emit), so its cost scales with governed
// file bytes, not repository size.
func BenchmarkRedactMarker(b *testing.B) {
	keys := []string{"npmAuthToken", "npmAuthIdent"}
	typical := "nodeLinker: node-modules\nnpmRegistryServer: \"https://registry.example.com/\"\nnpmAuthToken: abcdef0123456789\nenableTelemetry: false\n"
	clean := strings.Repeat("compressionLevel: mixed\nenableGlobalCache: false\n", 1<<20/50)
	dense := strings.Repeat("a: 1\nb: 2\nc: 3\nd: 4\nnpmAuthToken: abcdef0123456789\n", 1<<20/60)
	for name, body := range map[string]string{"typical": typical, "clean_1MiB": clean, "dense_1MiB": dense} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := redactMarker(body, keys); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
