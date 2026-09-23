// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// driftedRetireQuery deletes every code drifted finding fact under one
// (scope_id, generation_id) except the keep set the current pass just
// wrote. It is kind-scoped (never touches other families), scope-scoped,
// and generation-scoped: only the current pass's own superseded rows go.
const driftedRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
`

// retireDriftedFindings runs the generation-authoritative retire for one
// write. keepFactIDs are the fact ids the current pass just wrote for
// (scopeID, generationID); every other code drifted finding fact under that
// same (scope_id, generation_id) is deleted, including the fully-resolved
// case where the keep set is empty.
func retireDriftedFindings(
	ctx context.Context,
	db factwrite.Execer,
	scopeID string,
	generationID string,
	keepFactIDs []string,
) error {
	if _, err := db.ExecContext(
		ctx, driftedRetireQuery,
		facts.ReducerCodeDriftedFindingFactKind, scopeID, generationID, array.StringArray(keepFactIDs),
	); err != nil {
		return fmt.Errorf("retire stale code drifted findings: %w", err)
	}
	return nil
}
