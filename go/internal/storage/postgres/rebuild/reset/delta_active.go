// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// RequestReindexQuery records a per-repository reindex watermark for every
// scope in $1, stamped from the database clock and never moved backward. It is
// byte-identical to the POST /api/v0/admin/reindex upsert in
// storage/postgres/maintenance (a test there pins the two), so both writers
// share one semantics: the input is deduplicated and locked in scope_id order,
// because a duplicate inside one statement fails with SQLSTATE 21000 and two
// writers locking overlapping rows in different orders deadlock (SQLSTATE
// 40P01). It lives here because the maintenance store depends on the runtime
// package, which the postgres root must not import.
const RequestReindexQuery = `
INSERT INTO repository_reindex_requests AS request (scope_id, requested_at)
SELECT scope_id, now()
FROM (SELECT DISTINCT unnest($1::text[]) AS scope_id ORDER BY 1) AS requested
ON CONFLICT (scope_id) DO UPDATE
SET requested_at = GREATEST(request.requested_at, EXCLUDED.requested_at)
RETURNING scope_id, requested_at
`

// RequestReindex runs RequestReindexQuery for scopeIDs inside the caller's
// refinalize transaction and checks that every scope got a watermark. An
// empty list writes nothing.
func RequestReindex(ctx context.Context, q Queryer, scopeIDs []string) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, RequestReindexQuery, scopeIDs)
	if err != nil {
		return fmt.Errorf("refinalize reindex request: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stored := 0
	for rows.Next() {
		stored++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("refinalize reindex request: %w", err)
	}
	if want := len(uniqueSorted(scopeIDs)); stored != want {
		return fmt.Errorf("refinalize reindex request: %d watermarks returned for %d scopes", stored, want)
	}
	return nil
}

// DeltaActiveGeneration is one refinalized pair whose generation is a delta,
// with the outcome the refinalize reports for it.
type DeltaActiveGeneration struct {
	ScopeID      string
	GenerationID string
	// Outcome is recovery.DeltaActiveOutcomeReindexRequested for a git
	// default-branch scope, else recovery.DeltaActiveOutcomeReindexUnsupported.
	Outcome string
}

// DeltaActive returns the pairs of g whose generation is a delta, in the order
// g holds them (ascending scope ID for a refinalize read), and the scope IDs
// a reindex watermark can force to a full re-parse. A delta carries only the
// files that changed since its baseline, so re-projecting it onto an empty
// graph restores only those files (#7797).
func (g Generations) DeltaActive() ([]DeltaActiveGeneration, []string) {
	var delta []DeltaActiveGeneration
	var reindexable []string
	for i, isDelta := range g.IsDelta {
		if !isDelta {
			continue
		}
		scopeID := g.ScopeIDs[i]
		outcome := recovery.DeltaActiveOutcomeReindexUnsupported
		// The git collector keys per-repository reindex watermarks by the
		// default-branch scope ID only, and POST /api/v0/admin/reindex
		// accepts only those scopes, so only they can be forced to a full
		// re-parse. Both callers share recovery.IsGitDefaultBranchScope.
		if recovery.IsGitDefaultBranchScope(scopeID) {
			outcome = recovery.DeltaActiveOutcomeReindexRequested
			reindexable = append(reindexable, scopeID)
		}
		delta = append(delta, DeltaActiveGeneration{
			ScopeID:      scopeID,
			GenerationID: g.GenerationIDs[i],
			Outcome:      outcome,
		})
	}
	return delta, reindexable
}

// RequestDeltaActiveReindex classifies the refinalized pairs whose generation
// is a delta and records a per-repository reindex watermark for each git
// default-branch scope among them, inside the caller's refinalize transaction
// (#7797). It returns the report the refinalize result carries and the
// delta-active pairs the caller logs after commit.
//
// The caller runs it after AcquireReducerClaimFence and EnqueueProjectorWork,
// so the transaction already holds EXCLUSIVE on fact_work_items. The upsert
// adds ROW EXCLUSIVE on repository_reindex_requests plus row locks on the named
// scope rows. The only other writer of that table is the admin reindex route,
// one autocommit statement that takes no fact_work_items lock, and the git
// collector only reads it with a plain SELECT, so no lock cycle is possible.
// A rolled-back refinalize therefore records no watermark, and a committed one
// cannot leave a delta scope re-projected without its repair request.
func RequestDeltaActiveReindex(
	ctx context.Context,
	q Queryer,
	generations Generations,
) (recovery.DeltaActiveScopes, []DeltaActiveGeneration, error) {
	delta, reindexable := generations.DeltaActive()
	var report recovery.DeltaActiveScopes
	for _, generation := range delta {
		report.Add(generation.Outcome, generation.ScopeID)
	}
	if err := RequestReindex(ctx, q, reindexable); err != nil {
		return recovery.DeltaActiveScopes{}, nil, err
	}
	return report, delta, nil
}
