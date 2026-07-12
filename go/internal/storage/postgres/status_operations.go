// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// liveActivityQueryPrefix selects every in-flight work item (#5137) joined to
// its originating ingestion scope for repo/collector identity, and
// liveActivityQuerySuffix orders and bounds the result. buildLiveActivityQuery
// composes the two, inserting an access-scope predicate between them only
// when the caller is not allScopes; splitting the query this way keeps the
// admin/all-scopes shape byte-identical to the original single-string query
// this package used before #5137 cold-review P1-1.
//
// lease_owner is COALESCEd to empty string so the Go scan target stays a
// plain string, matching the rest of this package's convention (see
// generationTransitionsQuery's current_active_generation_id); claim_until can
// be genuinely NULL (for example a retrying item not currently leased), so it
// scans into sql.NullTime.
//
// source_display resolves the operator-facing repo name (#5137 follow-up: raw
// source_key is an opaque hash like "repository:r_ea78e8bb" for git scopes).
// Git collectors carry the human-readable identity in the scope payload as
// repo_slug ("acme/orders-api") or, on older payload shapes, repo_name;
// COALESCE/NULLIF/BTRIM falls back through both before landing on source_key
// so every row always has a display value, never NULL or blank.
const (
	liveActivityQueryPrefix = `
SELECT w.work_item_id, w.stage, w.status, w.domain, COALESCE(w.lease_owner, '') AS lease_owner,
       w.claim_until, w.attempt_count, w.updated_at, w.created_at,
       s.scope_kind, s.collector_kind, s.source_system, s.source_key,
       COALESCE(NULLIF(BTRIM(s.payload->>'repo_slug'), ''), NULLIF(BTRIM(s.payload->>'repo_name'), ''), s.source_key) AS source_display
FROM fact_work_items w
JOIN ingestion_scopes s ON s.scope_id = w.scope_id
WHERE w.status IN ('claimed', 'running', 'retrying')
`
	liveActivityQuerySuffix = `
ORDER BY w.updated_at DESC, w.work_item_id
LIMIT $1
`
)

// buildLiveActivityQuery returns the parameterized SQL and positional args
// for one ReadLiveActivity call.
//
// Access scoping (#5137 cold-review P1-1): when allScopes is true
// (admin/shared tokens; the pre-fix behavior) the returned query is
// byte-identical to the original unfiltered query -- no predicate, no plan
// change, preserving the proven 6.1ms/12.3ms shape below. When allScopes is
// false, an additional AND clause restricts rows to the caller's granted
// repositories (matched against ingestion_scopes.source_key for
// scope_kind='repository') or ingestion scopes (matched against
// fact_work_items.scope_id directly), mirroring
// admin_store_dead_letters.go's buildListDeadLetterWorkItemsQuery over the
// same two tables. Callers MUST short-circuit before calling this (or
// ReadLiveActivity) at all when the caller is scoped but holds NO grants --
// an empty allowedRepositoryIDs/allowedScopeIDs here, when allScopes is
// false, is handled by ReadLiveActivity itself (see its own doc); this
// function is never called in that case.
//
// Performance Evidence: scratch Postgres 16 + migrations 001/002/005,
// synthetic corpus of 20k ingestion_scopes / 150k fact_work_items rows --
// normal shape (~1.9k in-flight rows) ran in 6.1ms via a Bitmap Index Scan on
// fact_work_items_status_idx (status, visible_at, updated_at) feeding a top-N
// heapsort under LIMIT; pathological shape (61k in-flight/retrying rows) ran
// in 12.3ms. Both are well inside the console's 10-12s poll budget. See
// go/internal/storage/postgres/README.md, "Live operations activity read
// (#5137)" for the re-proof of both the allScopes and the granted-set shapes
// after the P1-1 access-scope predicate was added.
//
// No-Regression Evidence: the source_display expression is a JSONB key
// extraction (->>' ') plus two NULLIF/BTRIM calls, evaluated only on the rows
// the LIMIT already returns (at most limit+1, capped at LiveActivityMaxLimit
// 500). It adds no join, no new index requirement, and no per-row cost that
// scales with corpus size, so it does not change the query's plan shape or
// the 6.1ms/12.3ms proof above. The P1-1 access-scope predicate is index-free
// too (ANY() over a small caller-supplied array, no new join), re-proven
// against the same synthetic corpus; see the README section above.
func buildLiveActivityQuery(limit int, allScopes bool, allowedRepositoryIDs, allowedScopeIDs []string) (string, []any) {
	args := []any{limit}
	if allScopes {
		return liveActivityQueryPrefix + liveActivityQuerySuffix, args
	}

	args = append(args, pq.Array(allowedRepositoryIDs))
	repoArg := len(args)
	args = append(args, pq.Array(allowedScopeIDs))
	scopeArg := len(args)

	var builder strings.Builder
	builder.WriteString(liveActivityQueryPrefix)
	_, _ = fmt.Fprintf(&builder,
		"  AND ((s.scope_kind = 'repository' AND s.source_key = ANY($%d)) OR w.scope_id = ANY($%d))\n",
		repoArg, scopeArg,
	)
	builder.WriteString(liveActivityQuerySuffix)
	return builder.String(), args
}

// Bounds for the operator-supplied `limit` query parameter on
// GET /api/v0/status/operations. The query handler clamps to this range; the
// store itself falls back to LiveActivityDefaultLimit for a non-positive
// limit so a direct caller never issues an unbounded query.
const (
	LiveActivityDefaultLimit = 100
	LiveActivityMaxLimit     = 500
)

// LiveActivityStore reads the bounded in-flight work-item read model for the
// live operations board (#5137). It is a narrow, dedicated store rather than
// a StatusStore section because this query is bounded by an
// operator-supplied limit, unlike the fixed RawSnapshot shape the rest of the
// status surface composes.
type LiveActivityStore struct {
	queryer Queryer
	// Instruments is left nil by NewLiveActivityStore (matching StatusStore's
	// convention) so existing construction call sites stay source-compatible;
	// NewInstrumentedLiveActivityStore is the wiring entry point that wants
	// the duration/error signals.
	Instruments *telemetry.Instruments
}

// NewLiveActivityStore constructs a read-only live-activity store with no
// telemetry wired.
func NewLiveActivityStore(queryer Queryer) LiveActivityStore {
	return LiveActivityStore{queryer: queryer}
}

// NewInstrumentedLiveActivityStore constructs a live-activity store that
// records eshu_dp_status_operations_live_activity_query_duration_seconds and
// eshu_dp_status_operations_live_activity_query_errors_total on every read.
func NewInstrumentedLiveActivityStore(queryer Queryer, instruments *telemetry.Instruments) LiveActivityStore {
	return LiveActivityStore{queryer: queryer, Instruments: instruments}
}

// ReadLiveActivity returns up to limit in-flight work items (claimed,
// running, or retrying), ordered by most-recently-updated first, each joined
// to its originating repo/scope identity. A non-positive limit falls back to
// LiveActivityDefaultLimit. The bool result reports whether more matching
// rows existed than limit allowed through.
//
// Access scoping (#5137 cold-review P1-1): allScopes selects the
// admin/all-scopes path (no row filtering; the query text is unchanged from
// before this fix). When allScopes is false, rows are restricted to
// allowedRepositoryIDs/allowedScopeIDs; if BOTH are empty this method returns
// zero rows WITHOUT querying Postgres at all -- a scoped caller with no
// granted repository or ingestion scope must never observe another tenant's
// in-flight work (existence, volume, domain, or timing), even though
// identity fields are already redacted at the query-handler layer. The
// query handler (go/internal/query/status_operations.go getOperations)
// short-circuits before calling this method for the same case; this check is
// defense in depth, not a substitute for that guard.
func (s LiveActivityStore) ReadLiveActivity(
	ctx context.Context,
	limit int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]statuspkg.LiveActivityRow, bool, error) {
	if limit <= 0 {
		limit = LiveActivityDefaultLimit
	}
	if !allScopes && len(allowedRepositoryIDs) == 0 && len(allowedScopeIDs) == 0 {
		return nil, false, nil
	}

	start := time.Now()
	rows, truncated, err := s.readLiveActivity(ctx, limit, allScopes, allowedRepositoryIDs, allowedScopeIDs)
	s.recordOutcome(ctx, time.Since(start), err)
	return rows, truncated, err
}

func (s LiveActivityStore) readLiveActivity(
	ctx context.Context,
	limit int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]statuspkg.LiveActivityRow, bool, error) {
	if s.queryer == nil {
		return nil, false, fmt.Errorf("read live activity: queryer is required")
	}

	query, args := buildLiveActivityQuery(limit+1, allScopes, allowedRepositoryIDs, allowedScopeIDs)
	rows, err := s.queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("read live activity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]statuspkg.LiveActivityRow, 0, limit)
	truncated := false
	for rows.Next() {
		if len(out) == limit {
			// The limit+1-th row only proves more rows exist; do not scan or
			// keep it.
			truncated = true
			break
		}
		var row statuspkg.LiveActivityRow
		var claimUntil sql.NullTime
		if scanErr := rows.Scan(
			&row.WorkItemID,
			&row.Stage,
			&row.Status,
			&row.Domain,
			&row.LeaseOwner,
			&claimUntil,
			&row.AttemptCount,
			&row.UpdatedAt,
			&row.CreatedAt,
			&row.ScopeKind,
			&row.CollectorKind,
			&row.SourceSystem,
			&row.SourceKey,
			&row.SourceDisplay,
		); scanErr != nil {
			return nil, false, fmt.Errorf("read live activity: %w", scanErr)
		}
		if claimUntil.Valid {
			row.ClaimUntil = claimUntil.Time
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read live activity: %w", err)
	}
	return out, truncated, nil
}

// recordOutcome is nil-safe: when Instruments (or either instrument on it) is
// nil, recording is a no-op so LiveActivityStore stays usable without a
// meter provider wired, matching StatusStore's Instruments convention.
func (s LiveActivityStore) recordOutcome(ctx context.Context, duration time.Duration, err error) {
	if s.Instruments == nil {
		return
	}
	if s.Instruments.LiveActivityQueryDuration != nil {
		s.Instruments.LiveActivityQueryDuration.Record(ctx, duration.Seconds())
	}
	if err != nil && s.Instruments.LiveActivityQueryErrors != nil {
		s.Instruments.LiveActivityQueryErrors.Add(ctx, 1)
	}
}
