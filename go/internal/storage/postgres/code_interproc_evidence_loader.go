// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// LoadCodeInterprocEvidenceFacts implements
// reducer.CodeInterprocEvidenceFactLoader by scanning the raw
// code_interproc_evidence fact envelopes for one scope generation. The reducer
// handler decodes them through the typed contracts seam
// (ExtractCodeInterprocEvidenceRowsWithQuarantine) so a fact missing a required
// endpoint uid dead-letters as an input_invalid quarantine instead of being
// silently dropped. This is the raw-fact loader path used by the
// materialization handler; the fixpoint projector reads a separate in-memory
// solve (reducer.ValueFlowFixpointEvidenceLoader) that produces typed inputs
// directly. Tombstones are filtered by the decode seam, not here.
func (s FactStore) LoadCodeInterprocEvidenceFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeInterprocEvidenceFactKind})
}
