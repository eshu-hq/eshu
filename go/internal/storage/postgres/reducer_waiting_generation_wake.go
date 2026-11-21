// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// terminalGenerationActiveWaitKeysQuery lists the parked generation_active
// wait keys whose generation already reached terminal truth (superseded by
// a newer generation or retired by a terminal projector Fail). The probe is
// scope-free (#11016): one claim sweep retires generations in every scope
// it scans, so scoping to the claimed scope misses the sweep's other
// victims. The predicate leads with waits_for_kind on
// reducer_waiting_items_wake_idx and the parked-waits table holds only
// deferred intents, so the widened probe stays a cheap index probe; see the
// #11016 evidence note for the measured plan.
const terminalGenerationActiveWaitKeysQuery = `
SELECT DISTINCT wait.waits_for_key
FROM reducer_waiting_items AS wait
JOIN scope_generations AS gen
  ON gen.scope_id = wait.scope_id AND gen.generation_id = wait.generation_id
WHERE wait.waits_for_kind = 'generation_active'
  AND gen.status IN ('superseded', 'failed')
`

// wakeTerminalGenerationActiveWaits reopens the parked generation_active
// waits whose generation already resolved terminally
// (#11015, widened #11016). The projector Claim sweep retires superseded
// generations in the same statement that claims the newer work, so the
// sweep's victims never see an activation wake; without this post-claim
// wake they sit out the 10-minute backstop even though their handler could
// already observe the terminal generation. Like every #10876 wake this is a
// hint: the claim gate and the handler re-check, so a concurrent park or a
// spurious match only re-parks. A waiter that parks after this wake is
// covered by the set-aside re-check, which shares the terminal predicate
// below. A probe failure is metered here (the per-key wakes meter
// themselves inside wakeWaitingByKey) so every failure path counts exactly
// once on eshu_dp_reducer_waiting_wake_total{outcome=error}.
func wakeTerminalGenerationActiveWaits(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	now time.Time,
) error {
	rows, err := database.QueryContext(ctx, terminalGenerationActiveWaitKeysQuery)
	if err != nil {
		recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, telemetry.WaitingWakeOutcomeError)
		return fmt.Errorf("list terminal generation waits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, telemetry.WaitingWakeOutcomeError)
			return fmt.Errorf("scan terminal generation wait key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		recordWaitingWake(ctx, instruments, WaitingWakeSiteInTx, telemetry.WaitingWakeOutcomeError)
		return fmt.Errorf("list terminal generation waits: %w", err)
	}
	for _, key := range keys {
		if _, _, err := wakeWaitingByKey(ctx, database, instruments, WaitingWakeSiteInTx,
			WaitingKindGenerationActive, key, now); err != nil {
			return err
		}
	}
	return nil
}

// wakeTerminalGenerationActiveWaitsBestEffort runs the terminal-wait probe
// after a projector commit (Claim, including an empty sweep). A failure is
// logged and metered, never returned (#11016 F3): the claim already
// committed, so failing here would orphan the claimed work and misread a
// probe 40P01 as a claim conflict. The audit and the backstop still bound
// a missed wake.
func wakeTerminalGenerationActiveWaitsBestEffort(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	op, scopeID string,
	now time.Time,
) {
	if err := wakeTerminalGenerationActiveWaits(ctx, database, instruments, now); err != nil {
		logPostCommitWakeError(ctx, op, scopeID, "", err)
	}
}

// wakeGenerationActiveWaitsBestEffort reopens one scope generation's parked
// generation_active waits after its terminal save committed (Fail,
// heartbeat supersede). A failure is logged and metered, never returned
// (#11016 F3): the generation already resolved, so the caller keeps its
// committed outcome. The audit and the backstop still bound a missed wake.
func wakeGenerationActiveWaitsBestEffort(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	op, scopeID, generationID string,
	now time.Time,
) {
	if err := wakeGenerationActiveWaits(ctx, database, instruments, WaitingWakeSiteInTx,
		scopeID, generationID, now); err != nil {
		logPostCommitWakeError(ctx, op, scopeID, generationID, err)
	}
}

// logPostCommitWakeError records a post-commit generation_active wake
// failure at ERROR: the hint never fired, but the terminal truth stands and
// the audit plus the 10-minute backstop still bound the wait. Scope and
// generation ride along only when the caller names them (an empty sweep
// probes every scope).
func logPostCommitWakeError(ctx context.Context, op, scopeID, generationID string, err error) {
	attrs := []any{
		slog.String("queue", "projector"),
		slog.String("op", op),
		slog.String("error", err.Error()),
	}
	if scopeID != "" {
		attrs = append(attrs, slog.String(telemetry.LogKeyScopeID, scopeID))
	}
	if generationID != "" {
		attrs = append(attrs, slog.String(telemetry.LogKeyGenerationID, generationID))
	}
	attrs = append(attrs, slog.String(telemetry.LogKeyOutcome, telemetry.WaitingWakeOutcomeError))
	slog.ErrorContext(ctx,
		"projector post-commit generation_active wake failed; the audit and backstop still bound the miss",
		attrs...)
}
