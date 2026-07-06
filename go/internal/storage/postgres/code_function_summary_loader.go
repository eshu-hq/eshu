// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// LoadCodeFunctionSummaryFacts implements reducer.CodeFunctionSummaryLoader by
// scanning the raw code_function_summary fact envelopes for one scope
// generation. The reducer handler decodes them through the typed contracts
// seam (ExtractCodeFunctionSummaryEffectsWithQuarantine /
// ExtractCodeFunctionGraphIDsWithQuarantine) so a fact missing its required
// function_id dead-letters as an input_invalid quarantine instead of being
// silently dropped (Contract System v1 Wave 4f S2, issue #4754). Tombstones
// are filtered by the decode seam, not here.
func (s FactStore) LoadCodeFunctionSummaryFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeFunctionSummaryFactKind})
}

// LoadCodeFunctionGraphIDFacts implements reducer.CodeFunctionGraphIDLoader by
// scanning the same raw code_function_summary fact envelopes
// LoadCodeFunctionSummaryFacts returns; the reducer handler derives the
// FunctionID->graph-uid map from them. It is a distinct method so the graph-id
// store wiring stays independent of the summary store's.
func (s FactStore) LoadCodeFunctionGraphIDFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeFunctionSummaryFactKind})
}
