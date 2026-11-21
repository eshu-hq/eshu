// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkSupplyChainImpactParserFileRepositoryIDs10972 measures the real
// parser-file repository scope path (issue #10972, C5 fix B): the manifest
// dependency loop over the affected-package match list. npm advisories pair
// with npm manifest rows so the match arm executes, as on QA.
func BenchmarkSupplyChainImpactParserFileRepositoryIDs10972(b *testing.B) {
	for _, shape := range []struct {
		affected int
		deps     int
	}{
		{affected: 200, deps: 176},
		{affected: 200, deps: 727},
		{affected: 1000, deps: 727},
	} {
		b.Run(fmt.Sprintf("affected_%d_deps_%d", shape.affected, shape.deps), func(b *testing.B) {
			observedAt := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
			envelopes := make([]facts.Envelope, 0, shape.affected+shape.deps)
			for i := range shape.affected {
				name := fmt.Sprintf("bench-pkg-%d", i%50)
				envelopes = append(envelopes, vulnerabilityAffectedPackageFact(
					fmt.Sprintf("affected-bench-10972-%d", i),
					fmt.Sprintf("CVE-2026-%05d", i%25),
					"pkg:npm/"+name,
					"npm",
					name,
					"1.0.0",
					"2.0.0",
				))
			}
			for i := range shape.deps {
				repo := fmt.Sprintf("repository:bench-%d", i%20)
				name := fmt.Sprintf("bench-pkg-%d", i%50)
				envelopes = append(envelopes, packageManifestDependencyFactWithMetadata(
					repo, "api", "package-lock.json", name, "npm", "1.0.0",
					observedAt, map[string]any{"lockfile": true},
				))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got := supplyChainImpactParserFileRepositoryIDs(envelopes)
				if len(got) == 0 {
					b.Fatal("no repository IDs, want the npm manifest repositories")
				}
			}
		})
	}
}
