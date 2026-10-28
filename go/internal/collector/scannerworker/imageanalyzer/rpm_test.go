// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func TestAnalyzerExtractsRPMFromLayer(t *testing.T) {
	db, err := os.ReadFile(filepath.Join("..", "..", "ospackagevulnerability", "testdata", "rpmdb.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	layer := writeLayer(t, map[string]string{
		"etc/os-release":                    "ID=rocky\nVERSION_ID=9\n",
		"usr/lib/sysimage/rpm/rpmdb.sqlite": string(db),
	})
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:     input.Target.ScopeID,
		ImageDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		LayerPaths:  []string{layer},
	})
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.ResultCount != 1 {
		t.Fatalf("ResultCount = %d, want 1", result.Output.ResultCount)
	}
	firstFactByKind(t, result.Output.Facts, chain.VulnerabilityOSPackageFactKind)
}
