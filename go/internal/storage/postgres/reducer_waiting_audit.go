// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// ReducerWaitingAuditor runs AuditWaitingItems on an interval (#10876
// decision 4). The audit is idempotent, so every reducer replica may run
// one without a lease. The batch SELECT runs outside an explicit
// transaction (autocommit), so its FOR UPDATE SKIP LOCKED row locks release
// when the SELECT finishes and concurrent auditors can evaluate overlapping
// batches; the overlap stays benign because the wake and the reconcile
// delete are idempotent. Claims never wait for the audit — parked rows stay
// claimable through their backstop — and a missed wake is bounded by two
// audit cycles plus one claim latency.
type ReducerWaitingAuditor struct {
	// Queue supplies the database, clock, and instruments.
	Queue ReducerQueue
	// Interval is the delay between cycles; zero means
	// DefaultWaitingAuditInterval (5 minutes).
	Interval time.Duration
	// BatchSize bounds one audit pass; zero means
	// DefaultWaitingAuditBatchSize.
	BatchSize int
	// Logger receives one line per cycle that reopened or reconciled rows,
	// plus failures. Nil disables logging.
	Logger *slog.Logger
	// Now and Wait are test seams; nil means time.Now and a timer.
	Now  func() time.Time
	Wait func(context.Context, time.Duration) error
}

// Run audits once at startup, then once per interval until ctx ends.
func (s ReducerWaitingAuditor) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.AuditOnce(ctx)
		if err := s.wait(ctx, s.interval()); err != nil {
			return err
		}
	}
}

// AuditOnce runs one bounded audit pass and records the per-reason gauges. A
// failed pass is logged, never returned: the next cycle retries, and parked
// rows stay correct through the backstop meanwhile.
func (s ReducerWaitingAuditor) AuditOnce(ctx context.Context) {
	now := s.clock()
	reopened, reconciled, missed, err := AuditWaitingItems(
		ctx, s.Queue.database, s.Queue.Instruments, now, s.interval(), s.batchSize())
	if err != nil {
		s.log(ctx, "reducer waiting audit failed", slog.String("error", err.Error()))
		return
	}
	if reopened > 0 || reconciled > 0 || missed > 0 {
		s.log(ctx, "reducer waiting audit pass",
			slog.Int64("reopened", reopened),
			slog.Int64("reconciled", reconciled),
			slog.Int64("missed_wake", missed))
	}
	s.recordGauges(ctx)
}

// recordGauges publishes the per-reason waiting count and oldest-age gauges
// (acceptance criterion 7): the number of parked waits and the oldest age,
// by failure class.
func (s ReducerWaitingAuditor) recordGauges(ctx context.Context) {
	inst := s.Queue.Instruments
	if inst == nil || inst.ReducerWaitingCount == nil || inst.ReducerWaitingOldestAge == nil {
		return
	}
	counts, err := WaitingCounts(ctx, s.Queue.database)
	if err != nil {
		s.log(ctx, "reducer waiting counts failed", slog.String("error", err.Error()))
		return
	}
	for _, row := range counts {
		attrs := metric.WithAttributes(telemetry.AttrFailureClass(row.FailureClass))
		inst.ReducerWaitingCount.Record(ctx, row.Count, attrs)
		inst.ReducerWaitingOldestAge.Record(ctx, row.OldestAge.Milliseconds()/1000, attrs)
	}
}

func (s ReducerWaitingAuditor) clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s ReducerWaitingAuditor) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return DefaultWaitingAuditInterval
}

func (s ReducerWaitingAuditor) batchSize() int {
	if s.BatchSize > 0 {
		return s.BatchSize
	}
	return DefaultWaitingAuditBatchSize
}

func (s ReducerWaitingAuditor) wait(ctx context.Context, d time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s ReducerWaitingAuditor) log(ctx context.Context, msg string, attrs ...slog.Attr) {
	if s.Logger == nil {
		return
	}
	s.Logger.LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

// waitingAuditRow is one parked wait the audit evaluates.
type waitingAuditRow struct {
	workItemID   string
	kind         WaitingWakeKind
	keyJSON      string
	key          waitingKey
	scopeID      string
	generationID string
	domain       string
	failureClass string
	firstSeen    time.Time
}

// selectWaitingAuditBatchQuery reads the oldest parked waits for one audit
// pass. FOR UPDATE SKIP LOCKED skips rows a concurrent writer holds at
// SELECT time instead of stacking behind them, but the SELECT runs outside
// an explicit transaction (autocommit), so the locks release when it
// finishes: concurrent auditors may evaluate overlapping batches, and the
// idempotent wake plus reconcile delete keep the overlap benign.
const selectWaitingAuditBatchQuery = `
SELECT work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
       domain, failure_class, first_deferred_at
FROM reducer_waiting_items
ORDER BY first_deferred_at
LIMIT $1
FOR UPDATE SKIP LOCKED
`

// deleteWaitingRowQuery drops one wait row without touching its queue row:
// the reconciliation path for waits whose queue row already left the parked
// state through another path (completion fanout, replay, supersede sweep).
const deleteWaitingRowQuery = `
DELETE FROM reducer_waiting_items
WHERE work_item_id = $1 AND waits_for_kind = $2 AND waits_for_key = $3::jsonb
`

// reconciledDelete runs one audit reconcile delete and reports whether it
// dropped a row. A concurrent wake or audit pass may delete the same wait
// row first; a zero-row delete is a lost race, not a reconciliation, so
// only an affected row counts toward the reconciled total.
func reconciledDelete(ctx context.Context, database db.ExecQueryer, op string, args ...any) (bool, error) {
	res, err := database.ExecContext(ctx, deleteWaitingRowQuery, args...)
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("%s rows affected: %w", op, err)
	}
	return affected > 0, nil
}

// waitingQueueStateQuery reads the parked row's queue state so the audit
// never reopens work that already moved on.
const waitingQueueStateQuery = `
SELECT status, failure_class FROM fact_work_items WHERE work_item_id = $1
`

// AuditWaitingItems re-evaluates up to limit parked waits and reopens the
// satisfied ones. It returns reopened and reconciled (dangling wait rows
// dropped because their queue row already moved on) counts. Missed wakes —
// rows satisfied more than two audit intervals after they parked, meaning
// an earlier pass should have caught them — are counted separately so the
// seeded audit test can assert the audit finds a planted miss within its
// stated bound.
func AuditWaitingItems(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	now time.Time,
	interval time.Duration,
	limit int,
) (reopened, reconciled, missed int64, err error) {
	if limit <= 0 {
		limit = DefaultWaitingAuditBatchSize
	}
	rows, err := database.QueryContext(ctx, selectWaitingAuditBatchQuery, limit)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("select waiting audit batch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var batch []waitingAuditRow
	for rows.Next() {
		var row waitingAuditRow
		var kind, keyJSON string
		var firstSeen time.Time
		if err := rows.Scan(&row.workItemID, &kind, &keyJSON, &row.scopeID,
			&row.generationID, &row.domain, &row.failureClass, &firstSeen); err != nil {
			return 0, 0, 0, fmt.Errorf("scan waiting audit batch: %w", err)
		}
		row.kind = WaitingWakeKind(kind)
		row.keyJSON = keyJSON
		row.firstSeen = firstSeen.UTC()
		if err := json.Unmarshal([]byte(keyJSON), &row.key); err != nil {
			return 0, 0, 0, fmt.Errorf("decode waiting audit key: %w", err)
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("iterate waiting audit batch: %w", err)
	}

	// wokenKeys dedupes the pass: one wake statement per waits-for key. Rows
	// sharing a key are resolved by the same statement, so a sibling the
	// wake already consumed counts as reopened, never as reconciled.
	wokenKeys := map[string]bool{}
	for _, row := range batch {
		wakeKey := string(row.kind) + "\x00" + row.keyJSON
		if wokenKeys[wakeKey] {
			reopened++
			if interval > 0 && now.Sub(row.firstSeen) > 2*interval {
				missed++
				recordWaitingMissedWake(ctx, instruments)
			}
			continue
		}
		var status string
		var failureClass sql.NullString
		if err := querySingleRow(ctx, database, waitingQueueStateQuery,
			[]any{row.workItemID}, &status, &failureClass); err != nil {
			if err == sql.ErrNoRows {
				cleared, derr := reconciledDelete(ctx, database,
					"reconcile missing waiting queue row",
					row.workItemID, string(row.kind), row.keyJSON)
				if derr != nil {
					return reopened, reconciled, missed, derr
				}
				if cleared {
					reconciled++
				}
				continue
			}
			return reopened, reconciled, missed, fmt.Errorf("read waiting queue state: %w", err)
		}
		if status != "retrying" || (failureClass.Valid && failureClass.String != row.failureClass) {
			cleared, derr := reconciledDelete(ctx, database,
				"reconcile moved waiting queue row",
				row.workItemID, string(row.kind), row.keyJSON)
			if derr != nil {
				return reopened, reconciled, missed, derr
			}
			if cleared {
				reconciled++
			}
			continue
		}
		scopeID, generationID, producerDomain := row.scopeID, row.generationID, producerDomainForClass(row.failureClass)
		if row.kind == WaitingKindProducerTerminal && row.key.ScopeID == "" && row.key.ProducerDomain != "" {
			// #11000: a domain-only wait names no scope or generation, so
			// the legacy scope-pinned branch can never match; the audit
			// evaluates the quiescence branch on the parked domain instead.
			scopeID, generationID, producerDomain = "", "", row.key.ProducerDomain
		}
		satisfied, err := waitingSatisfied(
			ctx, database, row.kind,
			scopeID, generationID, producerDomain,
			row.key.EnqueuedAt, row.workItemID, row.key.Domains, row.firstSeen,
		)
		if err != nil {
			return reopened, reconciled, missed, err
		}
		if !satisfied {
			continue
		}
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, WaitingWakeSiteAudit,
			row.kind, row.keyJSON, now); err != nil {
			return reopened, reconciled, missed, err
		}
		wokenKeys[wakeKey] = true
		reopened++
		if interval > 0 && now.Sub(row.firstSeen) > 2*interval {
			missed++
			recordWaitingMissedWake(ctx, instruments)
		}
	}
	return reopened, reconciled, missed, nil
}

// producerDomainForClass returns the narrowed producer domain for a
// producer-terminal class, or "" when the class has no narrowing.
func producerDomainForClass(failureClass string) string {
	if entry, ok := WaitingDeferralByClass()[failureClass]; ok {
		return entry.ProducerDomain
	}
	return ""
}

// recordWaitingMissedWake counts one audit reopen that an earlier audit pass
// should have caught, on eshu_dp_reducer_waiting_missed_wake_total.
func recordWaitingMissedWake(ctx context.Context, instruments *telemetry.Instruments) {
	if instruments == nil || instruments.ReducerWaitingMissedWake == nil {
		return
	}
	instruments.ReducerWaitingMissedWake.Add(ctx, 1)
}

// WaitingCountsRow is one per-reason waiting aggregate for the operator
// gauges (acceptance criterion 7): the number of parked waits and the age of
// the oldest, by failure class.
type WaitingCountsRow struct {
	FailureClass string
	Count        int64
	OldestAge    time.Duration
}

// waitingCountsQuery aggregates the waiting table per reason. The oldest age
// derives from the minimum first_deferred_at, so one stuck wait stays
// visible no matter how many fresh waits arrive behind it.
const waitingCountsQuery = `
SELECT failure_class, count(*),
       EXTRACT(EPOCH FROM (now() - min(first_deferred_at)))::bigint
FROM reducer_waiting_items
GROUP BY failure_class
ORDER BY failure_class
`

// WaitingCounts returns the per-reason waiting aggregates.
func WaitingCounts(ctx context.Context, database db.ExecQueryer) ([]WaitingCountsRow, error) {
	rows, err := database.QueryContext(ctx, waitingCountsQuery)
	if err != nil {
		return nil, fmt.Errorf("count waiting items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WaitingCountsRow
	for rows.Next() {
		var row WaitingCountsRow
		var oldestSeconds int64
		if err := rows.Scan(&row.FailureClass, &row.Count, &oldestSeconds); err != nil {
			return nil, fmt.Errorf("scan waiting counts: %w", err)
		}
		row.OldestAge = time.Duration(oldestSeconds) * time.Second
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate waiting counts: %w", err)
	}
	return out, nil
}
