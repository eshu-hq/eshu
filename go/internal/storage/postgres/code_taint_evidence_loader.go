// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// LoadCodeTaintEvidence implements reducer.CodeTaintEvidenceLoader by scanning
// the raw code_taint_evidence fact envelopes for one scope generation. The
// reducer handler decodes them through the typed contracts seam
// (ExtractCodeTaintEvidenceRowsWithQuarantine) so a fact missing its required
// function_uid dead-letters as an input_invalid quarantine instead of being
// silently dropped. Keeping the raw envelope fetch here (and the typed decode +
// quarantine in the reducer package, where partitionDecodeFailures /
// recordQuarantinedFacts live) matches the code-graph-core (Wave 4f S1) and
// documentation (Wave 4e) precedent: the storage adapter owns the SQL fetch,
// the reducer owns the typed decode. Tombstones are filtered by the decode
// seam, not here, so the handler sees every fact it must quarantine over.
func (s FactStore) LoadCodeTaintEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeTaintEvidenceFactKind})
}
