// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// BenchmarkBuildGoReachabilityAliasIndex measures the once-per-intent index
// build with distinct GO advisories that each carry a CVE alias and call proof.
func BenchmarkBuildGoReachabilityAliasIndex(b *testing.B) {
	const advisoryCount = 200
	const modulePath = "example.com/vulnerable"
	const repositoryID = "repo-benchmark"
	envelopes := []facts.Envelope{{
		FactID:   "module",
		FactKind: chain.VulnerabilityGoModuleEvidenceFactKind,
		Payload: map[string]any{
			"repository_id":    repositoryID,
			"relative_path":    "go.mod",
			"module_path":      modulePath,
			"required_version": "v1.0.0",
		},
	}}
	for i := 0; i < advisoryCount; i++ {
		advisoryID := fmt.Sprintf("GO-2026-%04d", i)
		cveID := fmt.Sprintf("CVE-2026-%04d", i)
		envelopes = append(envelopes,
			facts.Envelope{
				FactID:   fmt.Sprintf("affected-%d", i),
				FactKind: chain.VulnerabilityAffectedPackageFactKind,
				Payload: map[string]any{
					"advisory_id":  advisoryID,
					"cve_id":       cveID,
					"ecosystem":    "go",
					"package_name": modulePath,
				},
			},
			facts.Envelope{
				FactID:   fmt.Sprintf("reach-%d", i),
				FactKind: chain.VulnerabilityGoCallReachabilityFactKind,
				Payload: map[string]any{
					"repository_id":      repositoryID,
					"osv_id":             advisoryID,
					"deepest_module":     modulePath,
					"deepest_package":    modulePath + "/package",
					"deepest_symbol":     "Vulnerable",
					"reachability_level": "symbol",
				},
			},
		)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
		if err != nil || len(quarantined) != 0 || len(index.goReachability) < advisoryCount {
			b.Fatalf("build index: reachability=%d quarantine=%d err=%v", len(index.goReachability), len(quarantined), err)
		}
	}
}
