// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The suppression fan-out wiring (#10019): a cve_id-scoped operator
// suppression changes no finding row on the operator pass, so ChangedCVEIDs
// is empty and no holder reopens. The writer diffs the pass's suppression
// facts against the newest admitted older generation with a cleared owed
// marker and unions the changed suppressions' CVE ids into the fan-out set,
// before the owed marker is recorded. The pure decisions live in
// suppression_diff.go and suppression_walk.go; this file owns the reads,
// the statement bound, and the report the handler emits.

// suppressionDiffStatementTimeout bounds one diff statement inside the writer
// transaction. One pair costs 143 ms at p50 (145 ms max over seven rounds) at
// 100,000 suppressions on a quiet host (#10330: PostgreSQL 18.6, stock
// settings, generic plans); K=4 pairs plus the endpoint remainder run five
// statements in about 0.7 s, so 10 s holds (K+1) statements with headroom for
// a p95 under 5 s. The holder read uses its own 10 s bound in its own
// transaction; this one covers only the diff statements and is reset before
// the row writes that follow.
const suppressionDiffStatementTimeout = 10 * time.Second

// setSuppressionDiffStatementTimeoutQuery scopes the statement bound to the
// writer transaction, so a pooled connection never carries it away.
const setSuppressionDiffStatementTimeoutQuery = `SELECT set_config('statement_timeout', $1, true)`

// resetSuppressionDiffStatementTimeoutQuery restores the transaction default
// after the walk, so the bound cannot leak onto the row writes that follow.
const resetSuppressionDiffStatementTimeoutQuery = `RESET statement_timeout`

// selectSuppressionCandidatesQuery returns every retained generation of the
// scope with what the baseline decision needs: lifecycle order, the
// reader-visible predicate, this pass's admission state, and whether the
// generation's reducer item still owes fan-out. Arguments: $1 scope id,
// $2 domain. The owed probe mirrors the #10060 sweep spare
// (reducer_generation_filter_sql.go): an owed marker or a non-empty
// continuation list means the generation has not finished fanning out, so a
// successor covers its range again instead of taking it as covered.
const selectSuppressionCandidatesQuery = `
SELECT g.generation_id,
       g.status,
       (g.activated_at IS NOT NULL),
       g.ingested_at,
       EXISTS (SELECT 1 FROM supply_chain_impact_write_admission AS a
               WHERE a.scope_id = g.scope_id AND a.generation_id = g.generation_id),
       EXISTS (SELECT 1 FROM fact_work_items AS w
               WHERE w.scope_id = g.scope_id AND w.generation_id = g.generation_id
                 AND w.stage = 'reducer' AND w.domain = $2
                 AND (COALESCE(w.payload->>'fanout_owed', '') = 'true'
                      OR (jsonb_typeof(w.payload->'fanout_remaining') = 'array'
                          AND jsonb_array_length(w.payload->'fanout_remaining') > 0)))
FROM scope_generations AS g
WHERE g.scope_id = $1
`

// selectSuppressionDiffPairQuery returns the suppressions whose payload
// differs between two generations of the scope: added, removed, or edited,
// on both sides of each change so a re-scope reaches the old CVE's holders
// too. The suppression store carries unchanged payload bytes forward and
// JSONB equality ignores key order, so an untouched suppression never shows.
// KEV and EPSS facts cannot join: the fact kind literal matches suppressions
// only. Arguments: $1 scope id, $2 current generation id, $3 prior
// generation id.
const selectSuppressionDiffPairQuery = `
SELECT COALESCE(cur.stable_fact_key, prior.stable_fact_key) AS stable_fact_key,
       COALESCE(cur.payload #>> '{scope,cve_id}', '') AS current_cve_id,
       COALESCE(prior.payload #>> '{scope,cve_id}', '') AS prior_cve_id,
       COALESCE(cur.payload #>> '{scope,advisory_id}', '') AS current_advisory_id,
       COALESCE(prior.payload #>> '{scope,advisory_id}', '') AS prior_advisory_id
FROM (SELECT stable_fact_key, payload FROM fact_records
      WHERE fact_kind = '` + chain.VulnerabilitySuppressionFactKind + `' AND is_tombstone = FALSE
        AND scope_id = $1 AND generation_id = $2) AS cur
FULL JOIN (SELECT stable_fact_key, payload FROM fact_records
      WHERE fact_kind = '` + chain.VulnerabilitySuppressionFactKind + `' AND is_tombstone = FALSE
        AND scope_id = $1 AND generation_id = $3) AS prior USING (stable_fact_key)
WHERE cur.payload IS DISTINCT FROM prior.payload
`

// selectSuppressionWholeGenerationQuery reads every suppression of one
// generation: the no-baseline first pass names them all as new. Arguments:
// $1 scope id, $2 generation id.
const selectSuppressionWholeGenerationQuery = `
SELECT stable_fact_key,
       COALESCE(payload #>> '{scope,cve_id}', '') AS current_cve_id,
       '' AS prior_cve_id,
       COALESCE(payload #>> '{scope,advisory_id}', '') AS current_advisory_id,
       '' AS prior_advisory_id
FROM fact_records
WHERE fact_kind = '` + chain.VulnerabilitySuppressionFactKind + `' AND is_tombstone = FALSE
  AND scope_id = $1 AND generation_id = $2
`

// suppressionDiffReport is one pass's suppression diff for the writer
// outcome and the handler's signals. Action is a suppressionDiffAction and
// Baseline a suppressionBaseline kind (or suppressionBaselineNone when the
// pass never chose one); both are bounded telemetry labels.
type suppressionDiffReport struct {
	action          suppressionDiffAction
	baseline        string
	cveIDs          []string
	rows            int
	statements      int
	noIdentityScope int
	walkTruncated   int
	duration        time.Duration
}

// decided reports whether the pass reached a diff decision (as opposed to a
// write that failed before the diff, which carries no signal).
func (r suppressionDiffReport) decided() bool { return r.action != "" }

// readSuppressionCandidates returns the scope's retained generations for the
// baseline decision, including the current one: its row carries this pass's
// status, activation, and admission state (read before this pass's own
// admission upsert, so Admitted means a previous attempt committed).
func readSuppressionCandidates(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
) ([]suppressionGeneration, error) {
	rows, err := tx.QueryContext(ctx, selectSuppressionCandidatesQuery,
		scopeID, string(reducercontract.DomainSupplyChainImpact))
	if err != nil {
		return nil, fmt.Errorf("read suppression diff candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []suppressionGeneration
	for rows.Next() {
		var (
			g         suppressionGeneration
			activated bool
		)
		if err := rows.Scan(&g.GenerationID, &g.Status, &activated,
			&g.IngestedAt, &g.Admitted, &g.Owed); err != nil {
			return nil, fmt.Errorf("scan suppression diff candidate: %w", err)
		}
		if activated {
			g.ActivatedAt = g.IngestedAt
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate suppression diff candidates: %w", err)
	}
	return out, nil
}

// scanSuppressionDiffRows drains one diff statement into rows.
func scanSuppressionDiffRows(rows SupplyChainImpactRows) ([]suppressionDiffRow, error) {
	var out []suppressionDiffRow
	for rows.Next() {
		var row suppressionDiffRow
		if err := rows.Scan(&row.SuppressionID, &row.CurrentCVEID, &row.PriorCVEID, &row.CurrentAdvisoryID, &row.PriorAdvisoryID); err != nil {
			return nil, fmt.Errorf("scan suppression diff row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate suppression diff rows: %w", err)
	}
	return out, nil
}

// runSuppressionDiff decides and, when the pass runs it, executes the
// suppression diff inside the writer transaction: the candidate read, the
// adjacent-pair walk under the statement bound, and the union. A pass whose
// own facts hold no suppression (KEV, EPSS, advisory) returns before any
// statement. A diff read error or timeout fails the whole walk with no
// partial set as the existing retryable fan-out class, so the pass retries
// as a unit; a persistent failure dead-letters loudly instead of fanning out
// nothing. statementTimeout overrides the default bound; non-positive keeps
// it, so production never threads a value and tests shorten it.
func runSuppressionDiff(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
	scopeFactKinds []string,
	recovery bool,
	statementTimeout time.Duration,
) (suppressionDiffReport, error) {
	started := time.Now()
	report := func(action suppressionDiffAction, baseline string) suppressionDiffReport {
		return suppressionDiffReport{action: action, baseline: baseline, duration: time.Since(started)}
	}
	if !slices.Contains(scopeFactKinds, chain.VulnerabilitySuppressionFactKind) {
		return report(suppressionDiffSkipKind, suppressionBaselineNone), nil
	}
	candidates, err := readSuppressionCandidates(ctx, tx, scopeID)
	if err != nil {
		return report("", suppressionBaselineNone), RetryableFanOutError(err)
	}
	currentIdx := slices.IndexFunc(candidates, func(g suppressionGeneration) bool {
		return g.GenerationID == generationID
	})
	if currentIdx < 0 {
		return report(suppressionDiffSkipNeverActivated, suppressionBaselineNone), nil
	}
	current := candidates[currentIdx]
	pass := suppressionDiffPass{
		ScopeFactKinds:      scopeFactKinds,
		GenerationStatus:    current.Status,
		GenerationActivated: !current.ActivatedAt.IsZero(),
		AlreadyAdmitted:     current.Admitted,
		Recovery:            recovery,
	}
	switch action := decideSuppressionDiff(pass); action {
	case suppressionDiffDeferUnactivated:
		rep := report(action, suppressionBaselineNone)
		return rep, action.failClosedError(scopeID, generationID)
	case suppressionDiffRun:
		// Run below.
	default:
		return report(action, suppressionBaselineNone), nil
	}
	baseline := selectSuppressionDiffBaseline(generationID, candidates)
	bound := statementTimeout
	if bound <= 0 {
		bound = suppressionDiffStatementTimeout
	}
	setRows, err := tx.QueryContext(ctx, setSuppressionDiffStatementTimeoutQuery,
		fmt.Sprintf("%dms", bound.Milliseconds()))
	if err != nil {
		return report(suppressionDiffRun, baseline.Kind), RetryableFanOutError(
			fmt.Errorf("set suppression diff statement timeout: %w", err))
	}
	_ = setRows.Close()
	rep := suppressionDiffReport{
		action:   suppressionDiffRun,
		baseline: baseline.Kind,
	}
	read := func(priorID, currentID string) ([]suppressionDiffRow, error) {
		rows, err := tx.QueryContext(ctx, selectSuppressionDiffPairQuery, scopeID, currentID, priorID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		return scanSuppressionDiffRows(rows)
	}
	if baseline.Kind == suppressionBaselineNone {
		rows, err := tx.QueryContext(ctx, selectSuppressionWholeGenerationQuery, scopeID, generationID)
		if err != nil {
			rep.duration = time.Since(started)
			return rep, RetryableFanOutError(fmt.Errorf("read suppression generation: %w", err))
		}
		all, err := scanSuppressionDiffRows(rows)
		_ = rows.Close()
		if err != nil {
			rep.duration = time.Since(started)
			return rep, RetryableFanOutError(fmt.Errorf("read suppression generation: %w", err))
		}
		rep.statements = 1
		rep.rows = len(all)
		rep.cveIDs = changedSuppressionIDs(all)
		rep.noIdentityScope = countDistinctSuppressionsWithoutIdentityScope(all)
	} else {
		plan := planSuppressionWalk(generationID, baseline, candidates, suppressionWalkMaxPairs)
		result, err := walkSuppressionDiff(plan, read)
		if err != nil {
			rep.duration = time.Since(started)
			return rep, RetryableFanOutError(fmt.Errorf("run suppression diff walk: %w", err))
		}
		rep.cveIDs = result.CVEIDs
		rep.rows = result.DiffRows
		rep.statements = result.Statements
		rep.noIdentityScope = result.NoIdentityScopeRows
		rep.walkTruncated = result.WalkTruncated
	}
	// The bound must not leak onto the row writes that follow. A rollback
	// would discard it, so only the success path resets.
	if _, err := tx.ExecContext(ctx, resetSuppressionDiffStatementTimeoutQuery); err != nil {
		rep.duration = time.Since(started)
		return rep, RetryableFanOutError(
			fmt.Errorf("reset suppression diff statement timeout: %w", err))
	}
	rep.duration = time.Since(started)
	return rep, nil
}

// The suppression diff's operator signals: its counters, its span, its log
// line, and the result sub-signals.

// suppressionDiffSpanForWrite opens the diff span for a write whose
// generation carries suppressions, and returns a no-op span otherwise, so
// KEV, EPSS and advisory passes add no span. The write ignores the returned
// context; it exists so the diff attributes land on one span.
func suppressionDiffSpanForWrite(
	h SupplyChainImpactHandler,
	ctx context.Context,
	intent reducercontract.Intent,
	write SupplyChainImpactWrite,
) (context.Context, trace.Span) {
	if !slices.Contains(write.ScopeFactKinds, chain.VulnerabilitySuppressionFactKind) {
		return ctx, trace.SpanFromContext(context.Background())
	}
	return h.startSuppressionDiffSpan(ctx, intent)
}

// finishSuppressionDiffSpan records the decided report on the span and ends
// it. It is safe on the no-op span.
func finishSuppressionDiffSpan(span trace.Span, rep suppressionDiffReport) {
	if rep.decided() {
		span.SetAttributes(
			attribute.String("action", string(rep.action)),
			attribute.String("baseline", rep.baseline),
			attribute.Int("diff_rows", rep.rows),
			attribute.Int("changed_cve_ids", len(rep.cveIDs)),
			attribute.Int("no_identity_scope_rows", rep.noIdentityScope),
			attribute.Int("walk_truncated", rep.walkTruncated),
			attribute.Int("statements", rep.statements),
			attribute.Float64("duration_seconds", rep.duration.Seconds()),
		)
	}
	span.End()
}

func (h SupplyChainImpactHandler) startSuppressionDiffSpan(ctx context.Context, intent reducercontract.Intent) (context.Context, trace.Span) {
	if h.Tracer == nil {
		// A no-op span: ending it must never end the caller's span.
		return ctx, trace.SpanFromContext(context.Background())
	}
	return h.Tracer.Start(ctx, telemetry.SpanSupplyChainImpactSuppressionDiff, trace.WithAttributes(
		attribute.String("scope_id", intent.ScopeID),
		attribute.String("generation_id", intent.GenerationID),
	))
}

// countSuppressionDiff records one diff decision. A non-positive n or a
// missing instrument records nothing, like the fan-out counters.
func (h SupplyChainImpactHandler) countSuppressionDiff(ctx context.Context, action, baseline string, n int64) {
	if n <= 0 || h.Instruments == nil || h.Instruments.SupplyChainSuppressionDiffTotal == nil {
		return
	}
	if action == "" {
		action = string(suppressionDiffSkipKind)
	}
	if baseline == "" {
		baseline = suppressionBaselineNone
	}
	h.Instruments.SupplyChainSuppressionDiffTotal.Add(ctx, n, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("outcome", action),
		attribute.String("baseline", baseline),
	))
}

func (h SupplyChainImpactHandler) observeSuppressionDiffDuration(ctx context.Context, rep suppressionDiffReport) {
	if h.Instruments == nil || h.Instruments.SupplyChainSuppressionDiffDuration == nil {
		return
	}
	h.Instruments.SupplyChainSuppressionDiffDuration.Record(ctx, rep.duration.Seconds(), metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("outcome", string(rep.action)),
	))
}

// emitSuppressionDiff records the pass's diff counters and its one summary
// log line. A pass that never reached a decision records nothing.
func (h SupplyChainImpactHandler) emitSuppressionDiff(ctx context.Context, intent reducercontract.Intent, rep suppressionDiffReport) {
	if !rep.decided() {
		return
	}
	h.countSuppressionDiff(ctx, string(rep.action), rep.baseline, 1)
	h.observeSuppressionDiffDuration(ctx, rep)
	if rep.walkTruncated > 0 && h.Instruments != nil && h.Instruments.SupplyChainSuppressionWalkTruncatedTotal != nil {
		h.Instruments.SupplyChainSuppressionWalkTruncatedTotal.Add(ctx, int64(rep.walkTruncated), metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
			attribute.String("baseline", rep.baseline),
		))
	}
	if rep.noIdentityScope > 0 && h.Instruments != nil && h.Instruments.SupplyChainSuppressionNoIdentityScopeTotal != nil {
		h.Instruments.SupplyChainSuppressionNoIdentityScopeTotal.Add(ctx, int64(rep.noIdentityScope), metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		))
	}
	if h.Logger == nil {
		return
	}
	h.Logger.InfoContext(ctx, "supply chain impact suppression diff",
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("intent_id", intent.IntentID),
		slog.String("action", string(rep.action)),
		slog.String("baseline", rep.baseline),
		slog.Int("diff_rows", rep.rows),
		slog.Int("changed_cve_ids", len(rep.cveIDs)),
		slog.Int("no_identity_scope_rows", rep.noIdentityScope),
		slog.Int("walk_truncated", rep.walkTruncated),
		slog.Int("statements", rep.statements),
		slog.String("duration", rep.duration.String()),
	)
}

// countSuppressionDiffFailClosed counts the writer's invariant fail-closed
// answer when the write fails with it: the runtime guard defers a pending
// generation before any handler runs, so reaching the writer means that
// guard was bypassed or is not wired. It fires only for a write whose
// generation carried suppressions; the writer never reads a generation
// otherwise.
func (h SupplyChainImpactHandler) countSuppressionDiffFailClosed(ctx context.Context, write SupplyChainImpactWrite, err error) {
	var notActive reducercontract.GenerationNotYetActiveError
	if !errors.As(err, &notActive) {
		return
	}
	if !slices.Contains(write.ScopeFactKinds, chain.VulnerabilitySuppressionFactKind) {
		return
	}
	h.countSuppressionDiff(ctx, string(suppressionDiffDeferUnactivated), suppressionBaselineNone, 1)
}

// mergeSubSignals adds the diff counts to a result's sub-signals, beside the
// fanout_* keys.
func (r suppressionDiffReport) mergeSubSignals(subSignals map[string]float64) {
	subSignals["suppression_diff_rows"] = float64(r.rows)
	subSignals["suppression_diff_cve_ids"] = float64(len(r.cveIDs))
	subSignals["suppression_diff_no_identity_scope"] = float64(r.noIdentityScope)
}

// writeFindingsWithSuppressionDiff runs the writer under the suppression-diff
// span and reports the diff outcome. A write error also records the fail-closed
// diff counter before the caller maps it, so Handle stays under the funlen cap.
func (h SupplyChainImpactHandler) writeFindingsWithSuppressionDiff(ctx context.Context, intent reducercontract.Intent, write SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	diffCtx, diffSpan := suppressionDiffSpanForWrite(h, ctx, intent, write)
	writeResult, err := h.Writer.WriteSupplyChainImpactFindings(diffCtx, write)
	h.emitSuppressionDiff(ctx, intent, writeResult.suppressionDiff)
	finishSuppressionDiffSpan(diffSpan, writeResult.suppressionDiff)
	if err != nil {
		h.countSuppressionDiffFailClosed(ctx, write, err)
		return writeResult, fmt.Errorf("write supply chain impact findings: %w", err)
	}
	return writeResult, nil
}
