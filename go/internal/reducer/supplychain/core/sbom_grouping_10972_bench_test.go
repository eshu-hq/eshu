// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// BenchmarkAppendSupplyChainImpactManifestFindings10972 measures the real
// per-repository manifest-findings build path (issue #10972, C5 fix A): one
// consumption list per package fanned out over one finding per repository.
// Rows interleave repositories in index order so the fixture mirrors QA,
// where many consumers share one package.
func BenchmarkAppendSupplyChainImpactManifestFindings10972(b *testing.B) {
	for _, shape := range []struct {
		repos int
		rows  int
	}{
		{repos: 200, rows: 400},
		{repos: 351, rows: 702},
		{repos: 351, rows: 3510},
		{repos: 813, rows: 8130},
	} {
		b.Run(fmt.Sprintf("repos_%d_rows_%d", shape.repos, shape.rows), func(b *testing.B) {
			pkg := supplychainmodel.AffectedPackage{
				FactID:    "affected-bench-10972",
				CVEID:     "CVE-2026-0001",
				PackageID: "pkg:npm/lodash",
				Ecosystem: "npm",
				Name:      "lodash",
			}
			cves := supplyChainCVEGroup{cveID: "CVE-2026-0001"}
			rows := make([]supplychainmodel.PackageConsumption, 0, shape.rows)
			for i := range shape.rows {
				rows = append(rows, supplychainmodel.PackageConsumption{
					FactID:       fmt.Sprintf("consume-bench-10972-%d", i),
					PackageID:    pkg.PackageID,
					RepositoryID: fmt.Sprintf("repository:bench-%d", i%shape.repos),
				})
			}
			index := supplyChainImpactIndex{
				consumption: map[string][]supplychainmodel.PackageConsumption{
					pkg.PackageID: rows,
				},
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				findings := appendSupplyChainImpactManifestFindings(nil, cves, []supplychainmodel.AffectedPackage{pkg}, index, false)
				if len(findings) != shape.repos {
					b.Fatalf("findings=%d, want %d", len(findings), shape.repos)
				}
			}
		})
	}
}
