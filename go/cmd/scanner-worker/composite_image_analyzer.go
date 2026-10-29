// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

const derivedImageScopePrefix = "image://derived/"

type compositeImageAnalyzer struct {
	static  scannerworker.Analyzer
	derived scannerworker.Analyzer
}

func (a compositeImageAnalyzer) Analyze(ctx context.Context, input scannerworker.ClaimInput) (scannerworker.AnalyzerResult, error) {
	if strings.HasPrefix(input.Target.ScopeID, derivedImageScopePrefix) {
		return a.derived.Analyze(ctx, input)
	}
	return a.static.Analyze(ctx, input)
}
