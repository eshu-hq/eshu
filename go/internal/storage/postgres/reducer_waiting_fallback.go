// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// waitingFallbackUnregistered labels a fallback-resolved wait whose failure
// class is not a parking class of the waiting registry, so both labels stay
// inside closed sets: registry classes and registry wake sites.
const waitingFallbackUnregistered = "unregistered"

// waitingFallbackLabels returns the failure_class and wake_site labels of
// one fallback-resolved wait: the class and its registered wake site when
// the registry parks that class (in_tx, completion_event, or audit), else
// "unregistered" for both.
func waitingFallbackLabels(failureClass string) (class, wakeSite string) {
	entry, ok := WaitingDeferralByClass()[failureClass]
	if !ok {
		return waitingFallbackUnregistered, waitingFallbackUnregistered
	}
	switch entry.WakeSite {
	case WaitingWakeSiteInTx, WaitingWakeSiteCompletion, WaitingWakeSiteAudit:
		return entry.FailureClass, entry.WakeSite
	default:
		return waitingFallbackUnregistered, waitingFallbackUnregistered
	}
}

// recordWaitingFallbackResolved counts one parked wait a fallback resolved
// instead of a wake, on eshu_dp_reducer_waiting_fallback_resolved_total
// (#11228), by reason, the reason's registered wake site, and outcome. A
// reopened or backstop count on a reason whose wake site is not audit
// means that reason's wake was lost; an audit-site reason resolves here by
// design.
func recordWaitingFallbackResolved(ctx context.Context, instruments *telemetry.Instruments, failureClass, outcome string) {
	if instruments == nil || instruments.ReducerWaitingFallbackResolved == nil {
		return
	}
	class, wakeSite := waitingFallbackLabels(failureClass)
	instruments.ReducerWaitingFallbackResolved.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrFailureClass(class),
		telemetry.AttrWaitingWakeSite(wakeSite),
		telemetry.AttrOutcome(outcome),
	))
}

// selectBackstopClaimWaitRowsQuery returns which of the claimed rows still
// hold a wait row. $1 is the candidate work item ids; the probe reads the
// reducer_waiting_items primary key by its leading column.
const selectBackstopClaimWaitRowsQuery = `
SELECT DISTINCT work_item_id FROM reducer_waiting_items WHERE work_item_id = ANY($1::text[])
`

// backstopClaimCandidates returns the claimed intents that may have left
// the set-aside through the 10-minute backstop: a parking class from the
// waiting registry rode the claim, and the row's visible_at is later than
// its reopened_at. A wake that stamps both columns with the wake time
// (in-tx, post-commit, audit, park re-check) leaves them equal, so a row it
// woke, including one woken on one key while a sibling key's wait row
// survives, sends no statement. The winners watermark wake is the
// documented exception: its reopened_at is the covering watermark, earlier
// than visible_at, so its claim sends one primary-key probe that finds no
// wait row (that wake deletes it) and counts nothing. A parked row the
// backstop released still carries the park's visible_at (park time plus
// WaitingBackstopDelay).
func backstopClaimCandidates(intents []reducercontract.Intent) []reducercontract.Intent {
	var candidates []reducercontract.Intent
	for _, intent := range intents {
		if intent.Failure == nil || !intent.AvailableAt.After(intent.CycleStartedAt) {
			continue
		}
		if class, _ := waitingFallbackLabels(intent.Failure.FailureClass); class == waitingFallbackUnregistered {
			continue
		}
		candidates = append(candidates, intent)
	}
	return candidates
}

// recordBackstopClaims counts the claimed rows that left the set-aside at
// the backstop with their wait row still present (#11228, ruling decision
// 4): outcome=backstop on eshu_dp_reducer_waiting_fallback_resolved_total.
// It runs after the claim statement committed, on the pool, and only for
// backstop candidates, so a healthy claim (woken or never parked) sends no
// statement. It is telemetry: a probe failure is logged at WARN and never
// fails the claim. Without the counter wired it sends nothing.
func (q ReducerQueue) recordBackstopClaims(ctx context.Context, intents []reducercontract.Intent) {
	if q.Instruments == nil || q.Instruments.ReducerWaitingFallbackResolved == nil {
		return
	}
	candidates := backstopClaimCandidates(intents)
	if len(candidates) == 0 {
		return
	}
	ids := make([]string, 0, len(candidates))
	for _, intent := range candidates {
		ids = append(ids, intent.IntentID)
	}
	rows, err := q.database.QueryContext(ctx, selectBackstopClaimWaitRowsQuery, array.StringArray(ids))
	if err != nil {
		slog.WarnContext(ctx, "reducer backstop claim probe failed; backstop exits not counted",
			slog.Int("candidates", len(ids)), slog.String("error", err.Error()))
		return
	}
	defer func() { _ = rows.Close() }()
	parked := make(map[string]bool, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.WarnContext(ctx, "reducer backstop claim probe scan failed; backstop exits not counted",
				slog.String("error", err.Error()))
			return
		}
		parked[id] = true
	}
	if err := rows.Err(); err != nil {
		slog.WarnContext(ctx, "reducer backstop claim probe failed; backstop exits not counted",
			slog.String("error", err.Error()))
		return
	}
	for _, intent := range candidates {
		if parked[intent.IntentID] {
			recordWaitingFallbackResolved(ctx, q.Instruments, intent.Failure.FailureClass,
				telemetry.WaitingResolvedOutcomeBackstop)
		}
	}
}

// noteBackstopClaim is recordBackstopClaims for the single-row Claim; it
// returns intent unchanged so the claim's return statement stays one line.
func (q ReducerQueue) noteBackstopClaim(ctx context.Context, intent reducercontract.Intent) reducercontract.Intent {
	q.recordBackstopClaims(ctx, []reducercontract.Intent{intent})
	return intent
}
