// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// UpsertQuery records that (generation, work item) committed under a claim
// and token, and refuses a writer whose token is below the stored one or
// equal to it under a different claim. Zero returned rows is a refusal. It
// runs inside the ingestion transaction after the generation upsert (the FK
// to scope_generations is satisfiable then) and while the scope row lock is
// held, so it adds no lock-order edge. Fences of different work items on one
// generation are separate rows and never refuse each other.
//
// $1 generation_id, $2 work_item_id, $3 scope_id, $4 claim_id,
// $5 fencing_token, $6 commit_id, $7 committed_at.
const UpsertQuery = `
INSERT INTO generation_commit_fences (
    generation_id, work_item_id, scope_id, claim_id,
    fencing_token, commit_id, committed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (generation_id, work_item_id) DO UPDATE SET
    claim_id = EXCLUDED.claim_id,
    fencing_token = EXCLUDED.fencing_token,
    commit_id = EXCLUDED.commit_id,
    committed_at = EXCLUDED.committed_at
WHERE generation_commit_fences.fencing_token < EXCLUDED.fencing_token
   OR (generation_commit_fences.fencing_token = EXCLUDED.fencing_token
       AND generation_commit_fences.claim_id = EXCLUDED.claim_id)
RETURNING fencing_token
`

// readQuery classifies a refusal with one primary-key read.
const readQuery = `
SELECT claim_id, fencing_token
FROM generation_commit_fences
WHERE generation_id = $1 AND work_item_id = $2
`

// Row is one plane-path commit's fence record. GenerationID is the
// COMMITTED generation (terraform-state's resolved snapshot), not the
// planned one the pin used.
type Row struct {
	GenerationID string
	WorkItemID   string
	ScopeID      string
	ClaimID      string
	FencingToken int64
	CommitID     string
	CommittedAt  time.Time
}

// Write runs UpsertQuery. It returns nil when the fence accepted the commit,
// a terminal *Rejection (fence_token or fence_claim_mismatch) when it
// refused, and a plain error for a database failure or a refusal with no
// conflicting row, which the predicate cannot produce.
func Write(ctx context.Context, queryer db.Queryer, row Row) error {
	accepted, err := queryOne(ctx, queryer, UpsertQuery, row.GenerationID, row.WorkItemID, row.ScopeID,
		row.ClaimID, row.FencingToken, row.CommitID, row.CommittedAt.UTC())
	if err != nil {
		return fmt.Errorf("write generation commit fence: %w", err)
	}
	if accepted {
		return nil
	}
	rows, err := queryer.QueryContext(ctx, readQuery, row.GenerationID, row.WorkItemID)
	if err != nil {
		return fmt.Errorf("read refused generation commit fence: %w", err)
	}
	var storedClaim string
	var storedToken int64
	found := rows.Next()
	if found {
		err = rows.Scan(&storedClaim, &storedToken)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("read refused generation commit fence: %w", err)
	}
	switch {
	case !found:
		return errors.New("generation commit fence refused the write but holds no row")
	case storedToken > row.FencingToken:
		return &Rejection{Reason: ReasonFenceToken, RequestedToken: row.FencingToken, StoredToken: storedToken}
	case storedToken == row.FencingToken && storedClaim != row.ClaimID:
		return &Rejection{Reason: ReasonFenceClaimMismatch, RequestedToken: row.FencingToken, StoredToken: storedToken}
	default:
		return fmt.Errorf("generation commit fence refused token %d over stored token %d", row.FencingToken, storedToken)
	}
}

func queryOne(ctx context.Context, queryer db.Queryer, query string, args ...any) (bool, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	found := rows.Next()
	if found {
		var token int64
		err = rows.Scan(&token)
	}
	return found, errors.Join(err, rows.Err(), rows.Close())
}
