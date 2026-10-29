// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

type imageAnalyzerFunc func(context.Context, scannerworker.ClaimInput) (scannerworker.AnalyzerResult, error)

func (f imageAnalyzerFunc) Analyze(ctx context.Context, input scannerworker.ClaimInput) (scannerworker.AnalyzerResult, error) {
	return f(ctx, input)
}

func TestCompositeImageAnalyzerRoutesDerivedAndStaticScopes(t *testing.T) {
	t.Parallel()
	staticCalls, derivedCalls := 0, 0
	analyzer := compositeImageAnalyzer{
		static: imageAnalyzerFunc(func(context.Context, scannerworker.ClaimInput) (scannerworker.AnalyzerResult, error) {
			staticCalls++
			return scannerworker.AnalyzerResult{}, nil
		}),
		derived: imageAnalyzerFunc(func(context.Context, scannerworker.ClaimInput) (scannerworker.AnalyzerResult, error) {
			derivedCalls++
			return scannerworker.AnalyzerResult{}, nil
		}),
	}
	input := testSBOMClaimInput(t)
	input.Target.ScopeID = "image://static/one"
	if _, err := analyzer.Analyze(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.Target.ScopeID = "image://derived/one"
	if _, err := analyzer.Analyze(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if staticCalls != 1 || derivedCalls != 1 {
		t.Fatalf("calls static=%d derived=%d, want 1 each", staticCalls, derivedCalls)
	}
}
