// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	"github.com/lib/pq"
)

// terraformConfigStateDriftRetireQuery is the generation-authoritative
// retire (review finding on #5594, following the #5848 precedent set for
// aws_cloud_runtime_drift): it removes every
// reducer_terraform_config_state_drift_finding fact for this
// (scope_id, generation_id) whose fact_id is not among the rows the current
// pass just wrote.
//
// A single WriteTerraformConfigStateDriftFindings call is a complete,
// mutually-exclusive assessment of one (scope_id, generation_id): exactly
// one of a set of per-address "exact" findings, one "ambiguous" finding, or
// one "unresolved" finding (see TerraformConfigStateDriftWrite's doc
// comment). Anything ELSE durable under that same (scope_id, generation_id)
// is therefore necessarily stale from an earlier pass, covering every
// outcome-transition shape:
//
//   - a prior "unresolved" row surviving alongside a later pass's "exact"
//     or "ambiguous" resolution (the review finding's own reproduction),
//   - a prior "ambiguous" row surviving a later "exact" or "unresolved"
//     resolution,
//   - a prior "exact" per-address finding surviving when a later pass no
//     longer admits a candidate for that specific address (a different
//     drift-kind reclassification, or the address's drift resolved) while
//     other addresses in the same generation still drift.
//
// This mirrors reapStaleContentEntitiesSQL's anti-join shape
// (go/internal/storage/postgres/content_writer_reap.go): "fact_id <>
// ALL($4)" is the standard SQL negation of membership, vacuously true for
// every existing row when keepFactIDs is empty, so a generation whose fresh
// write genuinely produced zero rows (not reachable via the reducer handler
// today, which only calls Write when it has something to write, but kept
// correct for any future or direct caller) still retires everything stale
// under that generation rather than leaving it silently orphaned.
const terraformConfigStateDriftRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
`

// retireTerraformConfigStateDriftFindings runs the generation-authoritative
// retire for one write. keepFactIDs are the fact ids the current pass just
// wrote for (scopeID, generationID); every other terraform config-state
// drift finding fact under that same (scope_id, generation_id) is deleted.
func retireTerraformConfigStateDriftFindings(
	ctx context.Context,
	db workloadIdentityExecer,
	scopeID string,
	generationID string,
	keepFactIDs []string,
) error {
	if _, err := db.ExecContext(
		ctx, terraformConfigStateDriftRetireQuery,
		terraformConfigStateDriftFactKind, scopeID, generationID, pq.StringArray(keepFactIDs),
	); err != nil {
		return fmt.Errorf("retire stale terraform config state drift findings: %w", err)
	}
	return nil
}
