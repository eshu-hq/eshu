// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

// awsCloudRuntimeDriftRetireQuery is the generation-authoritative retire
// (#5848): it removes a finding for an ARN this pass evaluated when that ARN's
// current fact id is not among the rows this pass just wrote. That covers both
// shapes a reclassification can take:
//
//   - The ARN reclassified to a DIFFERENT finding_kind (e.g. orphaned_cloud_resource
//     -> image_version_drift once Terraform state activates, #5837). The new
//     fact id is in the keep set; the OLD fact id for the same ARN is not, so
//     it retires.
//   - The ARN converged (Classify returns "" this pass, no candidate at all).
//     Nothing for that ARN is in the keep set, so a still-present prior finding
//     retires.
//
// scope_id/generation_id and the evaluated-ARN allowlist bound the blast
// radius to exactly the ARNs this pass's evidence load actually covered — an
// ARN outside that set is left untouched regardless of its fact_id, so a
// partial or scoped evidence load can never retire a finding it never looked
// at.
//
// fencing_token <= $6 is the same fence the insert's conflict guard uses,
// applied to DELETE instead of UPSERT: a superseded pass could reach this
// statement only if it were admitted (tryAdmitAWSCloudRuntimeDriftWrite already
// rejects that), so in production this predicate is a second, redundant line
// of defense rather than the primary guard -- but it costs nothing to keep and
// means a future caller of this query in isolation (outside the admission-gated
// writer) is still safe against retiring a fresher row.
const awsCloudRuntimeDriftRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND payload->>'arn' = ANY($4::text[])
  AND fact_id <> ALL($5::text[])
  AND fencing_token <= $6
`

// retireAWSCloudRuntimeDriftFindings runs the generation-authoritative retire
// for one write and returns how many stale rows it removed. evaluatedARNs must
// be every ARN this pass's evidence load covered (not only the ones that
// produced an admitted candidate), so a converged ARN's stale prior finding is
// still cleaned up. keepFactIDs are the fact ids this pass just wrote.
func retireAWSCloudRuntimeDriftFindings(
	ctx context.Context,
	tx AWSCloudRuntimeDriftTx,
	scopeID string,
	generationID string,
	evaluatedARNs []string,
	keepFactIDs []string,
	fencingToken int64,
) (int, error) {
	if len(evaluatedARNs) == 0 {
		return 0, nil
	}
	result, err := tx.ExecContext(
		ctx,
		awsCloudRuntimeDriftRetireQuery,
		awsCloudRuntimeDriftFactKind,
		scopeID,
		generationID,
		evaluatedARNs,
		keepFactIDs,
		fencingToken,
	)
	if err != nil {
		return 0, fmt.Errorf("retire stale aws cloud runtime drift findings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read aws cloud runtime drift retire rows affected: %w", err)
	}
	return int(affected), nil
}
