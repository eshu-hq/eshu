// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// LoadCodeFunctionSourceFacts implements reducer.CodeFunctionSourceLoader by
// scanning the raw code_function_source fact envelopes for one scope
// generation. The reducer handler decodes them through the typed contracts
// seam (ExtractCodeFunctionSourcesWithQuarantine) so a fact missing its
// required function_id/kind dead-letters as an input_invalid quarantine
// instead of being silently dropped (Contract System v1 Wave 4f S2, issue
// #4754). Tombstones are filtered by the decode seam, not here.
func (s FactStore) LoadCodeFunctionSourceFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeFunctionSourceFactKind})
}
