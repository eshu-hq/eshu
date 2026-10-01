// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactScannerScopePairsOverCapConverge pins #7154 on the
// scanner-analysis-scope stage: a scope with more distinct os_package scan
// targets than the old 256-pair cap used to drop the excess and report
// truncation, so it never retracted. Every pair must now be loaded.
func TestSupplyChainImpactScannerScopePairsOverCapConverge(t *testing.T) {
	t.Parallel()

	for _, count := range []int{10, supplyChainImpactFilterChunkSize, supplyChainImpactFilterChunkSize + 1, supplyChainImpactFilterChunkSize * 3} {
		loader := &scanScopedSupplyChainImpactFactLoader{}
		handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader}
		envelopes := make([]facts.Envelope, 0, count)
		for i := range count {
			envelopes = append(envelopes, facts.Envelope{
				FactID:       fmt.Sprintf("os-%04d", i),
				FactKind:     facts.VulnerabilityOSPackageFactKind,
				ScopeID:      fmt.Sprintf("scan-%04d", i),
				GenerationID: fmt.Sprintf("gen-%04d", i),
			})
		}
		if _, err := handler.loadSupplyChainImpactScannerAnalysisScopeFacts(context.Background(), envelopes, nil); err != nil {
			t.Fatalf("count=%d: error = %v", count, err)
		}
		if got := len(loader.kindCalls); got != count {
			t.Fatalf("count=%d: scan scopes queried = %d, want every pair", count, got)
		}
	}
}

// TestSupplyChainImpactResolvedDigestsOverCapConverge pins #7154 on the
// resolved-digest stage: digests past one filter chunk used to be dropped.
// Every digest must reach the active-evidence reader across chunks, and the
// stage's bool (the suppression tail only) must stay false.
func TestSupplyChainImpactResolvedDigestsOverCapConverge(t *testing.T) {
	t.Parallel()

	for _, count := range []int{10, supplyChainImpactFilterChunkSize, supplyChainImpactFilterChunkSize + 1, supplyChainImpactFilterChunkSize * 3} {
		loader := &stubSupplyChainImpactFactLoader{}
		handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader}
		analyses := make([]facts.Envelope, 0, count)
		for i := range count {
			analyses = append(analyses, facts.Envelope{
				FactID:   fmt.Sprintf("analysis-%04d", i),
				FactKind: facts.ScannerWorkerAnalysisFactKind,
				Payload:  map[string]any{"image_digest": fmt.Sprintf("sha256:%064d", i)},
			})
		}
		_, truncated, err := handler.loadSupplyChainImpactResolvedDigestEvidenceFacts(context.Background(), analyses, nil)
		if err != nil {
			t.Fatalf("count=%d: error = %v", count, err)
		}
		if truncated {
			t.Fatalf("count=%d: truncated = true; the excess digests must be paged, not dropped", count)
		}
		requested := map[string]bool{}
		for _, filter := range loader.filters {
			for _, digest := range filter.SubjectDigests {
				requested[digest] = true
			}
		}
		if len(requested) != count {
			t.Fatalf("count=%d: distinct digests reaching the reader = %d, want %d", count, len(requested), count)
		}
	}
}
