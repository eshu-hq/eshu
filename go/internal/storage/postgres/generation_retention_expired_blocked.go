// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the #7585 PR1 expired-but-blocked report: a pure-Go
// evaluation of one superseded generation against the policy's hard history
// ceiling. It reports only. It never authorizes or performs deletion: the
// candidate SQL's guards (status, active-generation id, live-work anti-join)
// stay the authoritative deletion bound, and the 10-year hold is untouched.
// Later PRs map live store signals (fact_work_items statuses, dependency
// references) onto GenerationRetentionEvaluationInput and surface the
// evaluations to operators; this PR fixes the reason vocabulary and the
// ceiling-edge semantics those PRs will share. Uncovered (#7472) and
// delta-protected (#7802) history has no reason in this vocabulary yet:
// PR2 must add flags or reasons for those classes, or explicitly scope
// them out, before the report is treated as total.

// GenerationRetentionBlockedReason names why hard-ceiling-expired history
// is retained instead of pruned. The set is closed so it can label operator
// telemetry. It mirrors the prune's safety exceptions: the candidate query
// never frees the active generation, generations with live work, or
// dependency-pinned generations, at any age.
type GenerationRetentionBlockedReason string

const (
	// GenerationRetentionBlockedActiveCurrent marks history that is still
	// its scope's active (current) generation. It is not actually
	// superseded-eligible; the report keeps it visible so a stuck active
	// pointer past the ceiling reads as blocked, never compliant.
	GenerationRetentionBlockedActiveCurrent GenerationRetentionBlockedReason = "active_current"
	// GenerationRetentionBlockedLiveWork marks history with in-flight
	// projection work (claimed or running): pruning it would delete facts
	// a worker is still writing.
	GenerationRetentionBlockedLiveWork GenerationRetentionBlockedReason = "live_work"
	// GenerationRetentionBlockedRetrying marks history with only retrying
	// work: nothing is writing now, but a retry may resume, so the prune
	// still holds it. It stays distinct from live work because it clears
	// on its own when the retries drain or dead-letter.
	GenerationRetentionBlockedRetrying GenerationRetentionBlockedReason = "retrying"
	// GenerationRetentionBlockedDependencyPin marks history another
	// generation or projection still references (content, ledger, or
	// shared-projection links): pruning it would orphan that reference.
	GenerationRetentionBlockedDependencyPin GenerationRetentionBlockedReason = "dependency_pin"
)

// GenerationRetentionEvaluationInput carries one generation's retention
// signals to the evaluation. Callers map their store reads onto these
// flags; the evaluation itself takes no database handle and performs no
// I/O, so the report path cannot lock, wait, or change prune behavior.
type GenerationRetentionEvaluationInput struct {
	// SupersededAt is the generation's original superseded_at, the same
	// instant the hard ceiling measures from. Callers pass only superseded
	// generations; the zero time is not a valid input.
	SupersededAt time.Time
	// IsActiveGeneration is true when the generation is still its scope's
	// active (current) generation.
	IsActiveGeneration bool
	// HasLiveWork is true when claimed or running work still references
	// the generation.
	HasLiveWork bool
	// HasRetryingWork is true when retrying work still references the
	// generation.
	HasRetryingWork bool
	// DependencyPinned is true when another generation or projection
	// still references the generation.
	DependencyPinned bool
}

// GenerationRetentionEvaluation is the retention posture of one superseded
// generation under the hard history ceiling: compliant history inside the
// ceiling, eligible history past it with no block, or expired-but-blocked
// history past it with the reason and age that keep it retained.
type GenerationRetentionEvaluation struct {
	// SupersededAge is now minus the original superseded_at, clamped at
	// zero for timestamps ahead of now.
	SupersededAge time.Duration
	// Expired is true when the history is older than the hard ceiling.
	Expired bool
	// BlockedReason is empty unless the history is expired and a safety
	// exception retains it.
	BlockedReason GenerationRetentionBlockedReason
}

// ExpiredBlocked reports whether the history is past the hard ceiling and
// retained by a safety exception: the generations PR1 surfaces to
// operators instead of silently calling compliant.
func (e GenerationRetentionEvaluation) ExpiredBlocked() bool {
	return e.Expired && e.BlockedReason != ""
}

// Compliant reports whether the history needs no operator attention: it is
// inside the hard ceiling. Expired-but-blocked history is not compliant,
// even though the prune retains it.
func (e GenerationRetentionEvaluation) Compliant() bool {
	return !e.Expired
}

// EvaluateGenerationRetention classifies one superseded generation against
// the policy's hard history ceiling (#7585). It assumes a validated
// policy: callers must reject HardMaxSupersededAge < MaxSupersededAge via
// validate, as the prune does, before reporting. An unset ceiling resolves
// through normalize, the same 90-day default (or longer soft window) the
// prune uses, so the report and the deletion bound cannot drift apart for
// validated policies.
// Expiry mirrors the candidate SQL exactly: history is expired only when
// strictly older than the ceiling (`superseded_at < now - ceiling`), so
// history exactly at the ceiling stays compliant. Past the ceiling the
// first matching safety exception wins, active/current before live work
// before retrying before a dependency pin; with no block the history is
// expired and eligible, carrying an empty reason.
func (p GenerationRetentionPolicy) EvaluateGenerationRetention(
	now time.Time,
	input GenerationRetentionEvaluationInput,
) GenerationRetentionEvaluation {
	policy := p.normalize()
	age := now.Sub(input.SupersededAt)
	if age < 0 {
		age = 0
	}
	evaluation := GenerationRetentionEvaluation{SupersededAge: age}
	if age <= policy.HardMaxSupersededAge {
		return evaluation
	}
	evaluation.Expired = true
	switch {
	case input.IsActiveGeneration:
		evaluation.BlockedReason = GenerationRetentionBlockedActiveCurrent
	case input.HasLiveWork:
		evaluation.BlockedReason = GenerationRetentionBlockedLiveWork
	case input.HasRetryingWork:
		evaluation.BlockedReason = GenerationRetentionBlockedRetrying
	case input.DependencyPinned:
		evaluation.BlockedReason = GenerationRetentionBlockedDependencyPin
	}
	return evaluation
}

// generationRetentionExpiredReportMarker tags the PR2 expired-generations
// report, so pg_stat_statements and the store's test doubles tell it apart
// from the prune's own selection.
const generationRetentionExpiredReportMarker = "-- retention: expired-blocked report"

// generationRetentionExpiredReportQuery reads the live store signals the
// PR2 report maps onto GenerationRetentionEvaluationInput, one row per
// expired superseded generation, oldest first. It mirrors the candidate
// selection's guards term for term so the report and the deletion bound
// cannot disagree about what blocks a generation:
//
//   - only `status = 'superseded'` rows with `superseded_at IS NOT NULL`
//     are read, the same population the candidate query ranks;
//   - expiry is the same strict predicate the candidate query's hard
//     ceiling arm uses (`superseded_at < $1`, with $1 the
//     normalize-resolved `now - HardMaxSupersededAge`), so a generation the
//     report omits is exactly one the evaluation calls compliant;
//   - the active-generation flag is the negation of the candidate query's
//     `IS DISTINCT FROM scope.active_generation_id` exclusion: a stuck
//     active pointer past the ceiling reports active_current;
//   - the claimed/running/retrying anti-join the candidate query lumps
//     into one live_work set is split into two EXISTS probes, so
//     retrying-only history reports retrying, distinct from live work. Each
//     probe carries the outer scope_id so it seeks the fact_work_items
//     (scope_id, generation_id, status) index prefix (generation_id is
//     globally unique, so the predicate is result-neutral), and the
//     report's LIMIT bounds the probe count;
//   - uncovered (#7472) and delta-protected (#7802) rows reuse the
//     candidate query's definitions verbatim and ride the report with
//     their flags visible, explicitly scoped out of the blocked verdict:
//     they have no reason in the blocked vocabulary yet.
//
// The statement is a single lock-free SELECT: it takes no row or advisory
// lock, never blocks ingestion or another retention pass, and reads under
// one snapshot. $1 is the ceiling cutoff, $2 the oldest-first row cap.
//
// Dependency pins have no live-store signal to read: every
// generation-referencing row class either cascades on the generation
// delete (fact_records, fact_work_items, search index, acceptance,
// obligations) or is explicitly deleted with the pruned batch
// (shared_projection_intents, content keys, changed-since ledger links
// naming the generation on either side, fact key sidecars, infra
// orphans), and the changed-since read path answers retention_expired
// for a pruned baseline by contract. The mapper therefore passes
// DependencyPinned false; the vocabulary slot stays reserved for a
// future pin class, still covered at the evaluation layer.
const generationRetentionExpiredReportQuery = generationRetentionExpiredReportMarker + `
WITH last_activated_full_write AS MATERIALIZED (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        generation.projection_write_started_at AS full_write_started_at
    FROM scope_generations AS generation
    WHERE generation.is_delta = false
      AND generation.activated_at IS NOT NULL
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
),
delta_base AS (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        generation.generation_id AS base_generation_id,
        generation.ingested_at AS base_ingested_at
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    JOIN scope_generations AS active
      ON active.generation_id = scope.active_generation_id
    WHERE active.is_delta = true
      AND generation.is_delta = false
      AND generation.activated_at IS NOT NULL
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
)
SELECT
    generation.scope_id,
    generation.generation_id,
    generation.superseded_at,
    (generation.generation_id IS NOT DISTINCT FROM scope.active_generation_id) AS is_active,
    EXISTS (
        SELECT 1
        FROM fact_work_items AS work
        WHERE work.scope_id = generation.scope_id
          AND work.generation_id = generation.generation_id
          AND work.status IN ('claimed', 'running')
    ) AS has_live_work,
    EXISTS (
        SELECT 1
        FROM fact_work_items AS work
        WHERE work.scope_id = generation.scope_id
          AND work.generation_id = generation.generation_id
          AND work.status = 'retrying'
    ) AS has_retrying_work,
    (generation.projection_write_started_at IS NOT NULL
     AND generation.activated_at IS NULL
     AND generation.projection_write_started_at > COALESCE(last_full.full_write_started_at, '-infinity'::timestamptz)) AS uncovered,
    (base.base_generation_id IS NOT NULL
     AND (generation.generation_id = base.base_generation_id
          OR (generation.ingested_at, generation.generation_id) > (base.base_ingested_at, base.base_generation_id))) AS delta_protected
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
LEFT JOIN last_activated_full_write AS last_full
  ON last_full.scope_id = generation.scope_id
LEFT JOIN delta_base AS base
  ON base.scope_id = generation.scope_id
WHERE generation.status = 'superseded'
  AND generation.superseded_at IS NOT NULL
  AND generation.superseded_at < $1
ORDER BY generation.superseded_at ASC, generation.generation_id ASC
LIMIT $2
`

// GenerationRetentionExpiredGeneration is one expired superseded
// generation with its live-store retention signals evaluated (#7585 PR2).
// A generation past the ceiling is either expired-but-blocked (a
// BlockedReason with its age), expired and eligible (empty reason), or
// explicitly scoped out of the verdict (uncovered #7472 or
// delta-protected #7802 history, which has no blocked reason yet). History
// inside the ceiling never appears: it is compliant, not expired.
type GenerationRetentionExpiredGeneration struct {
	// ScopeID and GenerationID identify the expired generation.
	ScopeID      string
	GenerationID string
	// Evaluation carries the ceiling verdict: age, expiry, and the
	// blocked reason when a safety exception retains the history.
	// A scoped-out row may still carry a coincidental BlockedReason from
	// its live-work flags (the evaluation has no notion of scoping), so
	// readers must gate on ScopedOut(), ExpiredBlocked(), and Eligible(),
	// never on Evaluation.BlockedReason alone.
	Evaluation GenerationRetentionEvaluation
	// Uncovered marks reconcile-probe evidence (#7472) and
	// DeltaProtected marks delta-chain history (#7802): both ride the
	// report visibly but are scoped out of the blocked verdict.
	Uncovered      bool
	DeltaProtected bool
}

// ExpiredBlocked reports whether the generation is past the hard ceiling
// and retained by a safety exception in the blocked vocabulary: the rows
// the report surfaces to operators instead of calling compliant.
// Scoped-out rows never count, even when their evaluated signals match.
func (g GenerationRetentionExpiredGeneration) ExpiredBlocked() bool {
	return !g.ScopedOut() && g.Evaluation.ExpiredBlocked()
}

// Eligible reports whether the generation is past the hard ceiling with
// no block and no scoping: prune-eligible history the candidate
// selection may admit.
func (g GenerationRetentionExpiredGeneration) Eligible() bool {
	return !g.ScopedOut() && g.Evaluation.Expired && g.Evaluation.BlockedReason == ""
}

// ScopedOut reports whether the generation is explicitly outside the
// blocked verdict: uncovered or delta-protected history, which has no
// reason in the blocked vocabulary yet and is never pruned at any age.
func (g GenerationRetentionExpiredGeneration) ScopedOut() bool {
	return g.Uncovered || g.DeltaProtected
}

// generationRetentionExpiredRow is one report-query row before evaluation,
// in the query's column order.
type generationRetentionExpiredRow struct {
	scopeID         string
	generationID    string
	supersededAt    time.Time
	isActive        bool
	hasLiveWork     bool
	hasRetryingWork bool
	uncovered       bool
	deltaProtected  bool
}

// evaluationInput maps the row's live-store signals onto the PR1
// evaluation input.
func (r generationRetentionExpiredRow) evaluationInput() GenerationRetentionEvaluationInput {
	return GenerationRetentionEvaluationInput{
		SupersededAt:       r.supersededAt,
		IsActiveGeneration: r.isActive,
		HasLiveWork:        r.hasLiveWork,
		HasRetryingWork:    r.hasRetryingWork,
		// No live-store signal feeds this flag: every
		// generation-referencing row class cascades or is batch-deleted
		// with the prune (see the report query). The slot stays reserved.
		DependencyPinned: false,
	}
}

// scanRetentionExpiredRows scans report rows whose column order matches
// generationRetentionExpiredReportQuery.
func scanRetentionExpiredRows(rows db.Rows) ([]generationRetentionExpiredRow, error) {
	var expired []generationRetentionExpiredRow
	for rows.Next() {
		var row generationRetentionExpiredRow
		if err := rows.Scan(&row.scopeID, &row.generationID, &row.supersededAt, &row.isActive, &row.hasLiveWork, &row.hasRetryingWork, &row.uncovered, &row.deltaProtected); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("generation retention: scan expired generations: %w", err)
		}
		expired = append(expired, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	return expired, nil
}

// SelectExpiredGenerations reports expired superseded generations with
// their live-store retention signals evaluated (#7585 PR2): past the
// ceiling, each generation carries its blocked reason and age, or an
// empty reason when it is eligible, or its scoping flags when it is
// explicitly outside the verdict. History inside the ceiling is absent.
// It is read-side wiring only: a single lock-free SELECT evaluated
// through EvaluateGenerationRetention. It changes no deletion behavior,
// runs no migration, and changes no default.
//
// The policy resolves through the same normalize and fail-closed
// validate the prune uses, so an explicit hard ceiling below the soft
// window is rejected before any store read and the report's ceiling
// cannot drift from the deletion bound. The cutoff and the evaluations
// share one now, so the SQL predicate and the evaluated expiry agree
// exactly at the ceiling edge.
//
// A wired tracer opens the expired-report span around the read; wired
// instruments count expired-but-blocked rows by closed reason, time the
// read, and count read failures on the shared retention failure counter.
// All three are optional and nil-safe: without them the report is the
// same pure read PR2 proved.
func (s GenerationRetentionStore) SelectExpiredGenerations(
	ctx context.Context,
	policy GenerationRetentionPolicy,
) ([]GenerationRetentionExpiredGeneration, error) {
	if s.database == nil {
		return nil, fmt.Errorf("generation retention database is required")
	}
	policy = policy.normalize()
	if err := policy.validate(); err != nil {
		return nil, err
	}
	if s.tracer != nil {
		var span trace.Span
		ctx, span = s.tracer.Start(ctx, telemetry.SpanGenerationRetentionExpiredReport)
		defer span.End()
	}
	now := s.now()
	reportStart := time.Now()
	rows, err := s.database.QueryContext(ctx, generationRetentionExpiredReportQuery,
		now.Add(-policy.HardMaxSupersededAge), policy.BatchGenerationLimit)
	if err != nil {
		s.recordExpiredReportFailure(ctx, err)
		return nil, fmt.Errorf("generation retention: select expired generations: %w", err)
	}
	signals, err := scanRetentionExpiredRows(rows)
	if err != nil {
		s.recordExpiredReportFailure(ctx, err)
		return nil, fmt.Errorf("generation retention: select expired generations: %w", err)
	}
	report := make([]GenerationRetentionExpiredGeneration, 0, len(signals))
	for _, signal := range signals {
		report = append(report, GenerationRetentionExpiredGeneration{
			ScopeID:        signal.scopeID,
			GenerationID:   signal.generationID,
			Evaluation:     policy.EvaluateGenerationRetention(now, signal.evaluationInput()),
			Uncovered:      signal.uncovered,
			DeltaProtected: signal.deltaProtected,
		})
	}
	s.recordExpiredReport(ctx, report, time.Since(reportStart))
	return report, nil
}
