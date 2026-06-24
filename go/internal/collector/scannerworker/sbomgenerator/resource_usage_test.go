// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

func TestAnalyzerPropagatesSourceResourceUsage(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	analyzer := Analyzer{Source: &stubSource{inventory: Inventory{
		SubjectDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		FileCount:     1,
		InputBytes:    512,
		ResourceUsage: scannerworker.ResourceUsage{
			CPUSeconds:      0.25,
			PeakMemoryBytes: 512,
		},
		Components: []Component{{
			Name:    "component",
			Version: "1.0.0",
		}},
	}}}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if got, want := result.Usage.CPUSeconds, 0.25; got != want {
		t.Fatalf("CPUSeconds = %v, want %v", got, want)
	}
	if got, want := result.Usage.PeakMemoryBytes, int64(512); got != want {
		t.Fatalf("PeakMemoryBytes = %d, want %d", got, want)
	}
}
