// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SupportsReadiness binds the supports readiness predicate the fence runs (#11197).
// The holder read answers from supply_chain_impact_finding_supports, so a table that
// is not ready must stop the fence. The composition root fills it from the postgres
// root, which owns the predicate text and its parameters; it is a plain value so this
// leaf package does not import the postgres root.
//
// The fence uses the predicate twice. Check 1 runs SQL on its own before the pointer
// detach, as a fail-fast. The holder statement embeds the same SQL as its first FROM
// item (HolderStatement), so the predicate and the holders are read in one snapshot:
// the backfill drain commits a scope's supports before it clears that scope's dirty
// mark, so a snapshot that sees the mark cleared also sees the supports, and a
// snapshot that sees the mark reports not ready.
type SupportsReadiness struct {
	// SQL is the readiness predicate: one scalar boolean SELECT taking the first
	// len(Args) parameters.
	SQL string
	// Args are the predicate's parameters, bound as $1 to $len(Args).
	Args []any
	// Observe classifies one readiness outcome and returns the error the fence
	// returns: nil when ready, the typed not-ready refusal when not, and the probe
	// error under its own class when err is set. It also records the outcome.
	Observe func(ctx context.Context, ready bool, err error) error
}

// validate fails closed on an unusable binding: the fence must not run a holder
// read it cannot classify.
func (r SupportsReadiness) validate() error {
	if r.SQL == "" {
		return errors.New("readiness SQL is empty")
	}
	if r.Observe == nil {
		return errors.New("readiness observer is nil")
	}
	return nil
}

// check is check 1: it runs the predicate on its own, as the first statement after
// the timeouts and before the pointer detach, and returns Observe's verdict. A
// predicate that returns no row reads as not ready.
func (r SupportsReadiness) check(ctx context.Context, tx db.Queryer) error {
	rows, err := tx.QueryContext(ctx, r.SQL, r.Args...)
	if err != nil {
		return r.Observe(ctx, false, fmt.Errorf("read supports readiness: %w", err))
	}
	defer func() { _ = rows.Close() }()
	var ready bool
	if rows.Next() {
		if err := rows.Scan(&ready); err != nil {
			return r.Observe(ctx, false, fmt.Errorf("scan supports readiness: %w", err))
		}
	}
	if err := rows.Err(); err != nil {
		return r.Observe(ctx, false, fmt.Errorf("read supports readiness: %w", err))
	}
	return r.Observe(ctx, ready, nil)
}

// The holder statement (#11197) finds the scopes holding an active finding support
// for the repository, on supply_chain_impact_finding_supports_repository_idx, and
// reports the readiness predicate in the same statement. READ COMMITTED gives one
// snapshot per statement, so the predicate and the holders agree.
//
// The text is split around the readiness predicate so the predicate is embedded
// byte-for-byte from the postgres root (HolderStatement). The predicate takes
// parameters $1 and $2; the holder parameters are $3 (repository) and $4 (the fenced
// scope), after the predicate's, so the injected text is never edited.
//
// Every row carries the predicate's verdict as ok. A repository with no holders
// returns one row (ok, NULL), because the predicate is the left side of the join and
// the holders hang off it. A table that is not ready joins no holder (the join
// condition is ok), so the Go side reads ok before it reads any holder. The table
// holds live finding facts only, so no tombstone predicate applies, and the empty
// repository id it stores for a finding without one never matches a repository id.
// Holders are found by repository, not by CVE, so no osv-to-CVE alias mapping is
// needed. Over-reopening is safe: a pass re-derives its own findings from active
// supports.
//
// The holders come back in the lock order of their supply_chain_impact work item at
// the active generation, work_item_id COLLATE "C" (#11147). The fence reopens every
// holder in this one transaction, and a reopen locks a holder's dead-letter row,
// which the admin replay, the recovery drain, and poison recovery lock in that same
// order. Scope-id order is not the same order: the work item id replaces ':' and '/'
// with '_'. A holder with no item takes no row lock (it is enqueued), so it sorts
// last. The statement takes no row lock itself.
const (
	fenceHolderStatementHead = `SELECT ready.ok, holder.scope_id
		FROM (`
	fenceHolderStatementTail = `) AS ready(ok)
		LEFT JOIN (
		    SELECT candidate.scope_id, item.lock_key
		    FROM (
		        SELECT DISTINCT support.scope_id, scope.active_generation_id
		        FROM supply_chain_impact_finding_supports AS support
		        JOIN ingestion_scopes AS scope
		          ON scope.scope_id = support.scope_id
		         AND scope.active_generation_id = support.generation_id
		        WHERE support.repository_id = $3
		          AND support.repository_id <> ''
		          AND support.scope_id <> $4
		    ) AS candidate
		    LEFT JOIN LATERAL (
		        SELECT min(work.work_item_id COLLATE "C") AS lock_key
		        FROM fact_work_items AS work
		        WHERE work.scope_id = candidate.scope_id
		          AND work.generation_id = candidate.active_generation_id
		          AND work.stage = 'reducer'
		          AND work.domain = 'supply_chain_impact'
		    ) AS item ON TRUE
		) AS holder ON ready.ok
		ORDER BY holder.lock_key COLLATE "C" NULLS LAST, holder.scope_id COLLATE "C"`
)

// HolderStatement composes the holder statement around readinessSQL, the readiness
// predicate the composition root binds. The predicate is embedded exactly once, in
// parentheses, as the first FROM item.
func HolderStatement(readinessSQL string) string {
	return fenceHolderStatementHead + readinessSQL + fenceHolderStatementTail
}

// readHolderScopes runs the holder statement and returns the holders, or Observe's
// error when the table is not ready in the statement's snapshot (even when holder
// rows came back). It reads every row and closes the rows before returning, because
// the reopen runs on the same transaction. A statement that returns no row is an
// error: the predicate always yields one. A NULL scope_id is the no-holder row, and
// is never returned.
func readHolderScopes(
	ctx context.Context,
	tx db.Transaction,
	readiness SupportsReadiness,
	repositoryID, scopeID string,
) ([]string, error) {
	args := append(append([]any{}, readiness.Args...), repositoryID, scopeID)
	rows, err := tx.QueryContext(ctx, HolderStatement(readiness.SQL), args...)
	if err != nil {
		return nil, readiness.Observe(ctx, false, err)
	}
	defer func() { _ = rows.Close() }()
	var (
		holders []string
		seen    int
		ready   = true
	)
	for rows.Next() {
		var (
			ok     bool
			holder sql.NullString
		)
		if err := rows.Scan(&ok, &holder); err != nil {
			return nil, readiness.Observe(ctx, false, fmt.Errorf("scan holder: %w", err))
		}
		seen++
		ready = ready && ok
		if holder.Valid {
			holders = append(holders, holder.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, readiness.Observe(ctx, false, err)
	}
	if err := rows.Close(); err != nil {
		return nil, readiness.Observe(ctx, false, err)
	}
	if seen == 0 {
		return nil, readiness.Observe(ctx, false, errors.New("holder statement returned no row"))
	}
	if err := readiness.Observe(ctx, ready, nil); err != nil {
		return nil, err
	}
	return holders, nil
}
