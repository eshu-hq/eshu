// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Exact-phase waits (#11104). A phase_committed deferral whose map row names
// a keyspace and phase parks on the exact key {scope_id, generation_id,
// keyspace, phase} instead of the coarse {scope_id, generation_id}. The key
// stands for one graph_projection_phase_state row: the generation's own phase,
// with acceptance_unit_id = scope_id and source_run_id = generation_id, the
// row the cross-repo handler gate reads (crossRepoBackwardEvidenceReadinessKey)
// and the deferred-backfill fan-in publishes. The coarse Upsert wake matches
// waits_for_key by JSONB equality, so it never touches an exact key; the exact
// wake below is the only in-tx path that reopens one, plus the post-commit
// re-wake, the audit, and the backstop.

// waitingPhaseRequirement is the (keyspace, phase) a phase-exact wait names.
// The zero value means a coarse wait.
type waitingPhaseRequirement struct {
	Keyspace string
	Phase    string
}

// exact reports whether the requirement names one phase row.
func (r waitingPhaseRequirement) exact() bool {
	return r.Keyspace != "" && r.Phase != ""
}

// phaseRequirement returns the exact requirement a map row enrolls, or the
// zero value for a coarse row.
func (d WaitingDeferral) phaseRequirement() waitingPhaseRequirement {
	if d.WaitsForKind != WaitingKindPhaseCommitted || d.Keyspace == "" || d.Phase == "" {
		return waitingPhaseRequirement{}
	}
	return waitingPhaseRequirement{Keyspace: string(d.Keyspace), Phase: string(d.Phase)}
}

// waitingExactPhaseKey renders the waits_for key of a phase-exact wait. It
// reports false when the scope or generation is blank or the requirement is
// not exact.
func waitingExactPhaseKey(scopeID, generationID string, req waitingPhaseRequirement) (string, bool) {
	scope := strings.TrimSpace(scopeID)
	gen := strings.TrimSpace(generationID)
	if scope == "" || gen == "" || !req.exact() {
		return "", false
	}
	raw, err := json.Marshal(waitingKey{
		ScopeID: scope, GenerationID: gen, Keyspace: req.Keyspace, Phase: req.Phase,
	})
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// exactPhaseWaitSatisfiedQuery reports whether the generation's own phase row
// committed: the activation phaseReadyQuery shape with all six primary-key
// columns pinned, so a phase another generation, scope, keyspace, or phase
// published never satisfies it (a coarse any-phase check would satisfy at
// park time and spin). $1 scope_id, $2 generation_id, $3 keyspace, $4 phase.
const exactPhaseWaitSatisfiedQuery = `
SELECT EXISTS (
    SELECT 1 FROM graph_projection_phase_state
    WHERE scope_id = $1 AND acceptance_unit_id = $1
      AND source_run_id = $2 AND generation_id = $2
      AND keyspace = $3 AND phase = $4
)
`

// exactPhaseWaitSatisfied evaluates exactPhaseWaitSatisfiedQuery.
func exactPhaseWaitSatisfied(ctx context.Context, database db.ExecQueryer, scopeID, generationID string, req waitingPhaseRequirement) (bool, error) {
	var satisfied bool
	if err := querySingleRow(ctx, database, exactPhaseWaitSatisfiedQuery,
		[]any{scopeID, generationID, req.Keyspace, req.Phase}, &satisfied); err != nil {
		return false, fmt.Errorf("check exact phase waiting satisfaction: %w", err)
	}
	return satisfied, nil
}

// exactPhaseEnrollment is the set of (keyspace, phase) pairs some deferral map
// row parks on, derived once from WaitingDeferralMap so Upsert never sends an
// exact wake for a phase nobody waits on.
func exactPhaseEnrollment() map[waitingPhaseRequirement]bool {
	exactPhaseEnrollmentOnce.Do(func() {
		cachedExactPhaseEnrollment = exactPhaseEnrollmentFrom(WaitingDeferralMap())
	})
	return cachedExactPhaseEnrollment
}

// exactPhaseEnrollmentFrom derives the enrollment set from a map.
func exactPhaseEnrollmentFrom(entries []WaitingDeferral) map[waitingPhaseRequirement]bool {
	out := map[waitingPhaseRequirement]bool{}
	for _, entry := range entries {
		if req := entry.phaseRequirement(); req.exact() {
			out[req] = true
		}
	}
	return out
}

var (
	exactPhaseEnrollmentOnce   sync.Once
	cachedExactPhaseEnrollment map[waitingPhaseRequirement]bool
)

// exactPhaseWakeKeys returns the distinct exact wait keys the published rows
// satisfy: enrolled (keyspace, phase) pairs on a generation's own row
// (acceptance_unit_id = scope_id, source_run_id = generation_id). Rows of any
// other phase yield nothing, so a non-enrolled publish sends no extra
// statement.
func exactPhaseWakeKeys(rows []gpphase.PhaseState, enrolled map[waitingPhaseRequirement]bool) []exactPhaseWakeKey {
	var out []exactPhaseWakeKey
	seen := map[string]bool{}
	for _, row := range rows {
		req := waitingPhaseRequirement{Keyspace: string(row.Key.Keyspace), Phase: string(row.Phase)}
		if !enrolled[req] {
			continue
		}
		if row.Key.AcceptanceUnitID != row.Key.ScopeID || row.Key.SourceRunID != row.Key.GenerationID {
			continue
		}
		key, ok := waitingExactPhaseKey(row.Key.ScopeID, row.Key.GenerationID, req)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, exactPhaseWakeKey{
			scopeID: strings.TrimSpace(row.Key.ScopeID), generationID: strings.TrimSpace(row.Key.GenerationID),
			req: req, keyJSON: key,
		})
	}
	return out
}

// exactPhaseWakeKey is one exact wake target.
type exactPhaseWakeKey struct {
	scopeID, generationID string
	req                   waitingPhaseRequirement
	keyJSON               string
}

// wakeExactPhaseWaits reopens the phase-exact waits the published rows
// satisfy. site is telemetry.WaitingWakeSitePhaseExactInTx inside the
// publishing transaction or telemetry.WaitingWakeSitePhaseExactPostCommit
// after it committed. Each statement logs one structured line with the
// reopened count: INFO when it reopened a waiter, DEBUG for a no-op, so a
// fan-in that publishes many partitions with nobody waiting stays quiet at
// INFO. The counter records the outcome of every statement.
func wakeExactPhaseWaits(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	site string,
	rows []gpphase.PhaseState,
	now time.Time,
) error {
	for _, target := range exactPhaseWakeKeys(rows, exactPhaseEnrollment()) {
		reopened, _, err := wakeWaitingByKey(ctx, database, instruments, site,
			WaitingKindPhaseCommitted, target.keyJSON, now)
		if err != nil {
			return fmt.Errorf("wake exact phase waits %s/%s: %w", target.req.Keyspace, target.req.Phase, err)
		}
		level := slog.LevelDebug
		if reopened > 0 {
			level = slog.LevelInfo
		}
		slog.Log(ctx, level, "reducer exact phase wake",
			slog.String(telemetry.LogKeyScopeID, target.scopeID),
			slog.String(telemetry.LogKeyGenerationID, target.generationID),
			slog.String("keyspace", target.req.Keyspace),
			slog.String("phase", target.req.Phase),
			slog.String("wake_site", site),
			slog.Int64("reopened", reopened))
	}
	return nil
}

// PostCommitExactPhaseWakeTimeout bounds the post-commit exact-phase re-wake
// (#11104). The re-wake runs detached from the publisher's context (a
// committed partition still re-wakes when a sibling partition cancels the
// pass), so this is its only bound. A healthy wake is one index probe plus
// at most a few row updates (0.07 ms measured); five seconds covers a wait
// behind a concurrent wake's row locks while staying far below the 5-minute
// audit interval, which reopens anything a timed-out re-wake missed. It
// matches the package's other detached post-commit writes (the config
// artifact failure record).
const PostCommitExactPhaseWakeTimeout = 5 * time.Second

// WakeExactPhaseWaitsAfterCommit re-wakes the phase-exact waits the rows
// satisfy after the publishing transaction committed (#11104). A waiter that
// parked inside the publisher's open window (its park-time check could not
// see the uncommitted phase, and the in-tx wake ran before its wait row
// existed) is reopened here. It runs on the pool connection, never on the
// committed transaction, detached from the caller's cancellation and bounded
// by PostCommitExactPhaseWakeTimeout. A failure or timeout is logged at
// ERROR and metered (outcome=error), never returned: the phase already
// committed, and the audit plus the 10-minute backstop still bound the miss.
func WakeExactPhaseWaitsAfterCommit(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	rows []gpphase.PhaseState,
) {
	wakeExactPhaseWaitsAfterCommitWithin(ctx, database, instruments, rows, PostCommitExactPhaseWakeTimeout)
}

// wakeExactPhaseWaitsAfterCommitWithin is WakeExactPhaseWaitsAfterCommit with
// an explicit bound, so the timeout path is testable without a five-second
// wait.
func wakeExactPhaseWaitsAfterCommitWithin(
	ctx context.Context,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	rows []gpphase.PhaseState,
	timeout time.Duration,
) {
	wakeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := wakeExactPhaseWaits(wakeCtx, database, instruments,
		telemetry.WaitingWakeSitePhaseExactPostCommit, rows, time.Now().UTC()); err != nil {
		logPostCommitExactPhaseWakeError(context.WithoutCancel(ctx), rows, err)
	}
}

// logPostCommitExactPhaseWakeError records a failed post-commit exact-phase
// re-wake at ERROR, naming the first row's scope and generation (the fan-in
// publishes one partition per call).
func logPostCommitExactPhaseWakeError(ctx context.Context, rows []gpphase.PhaseState, err error) {
	attrs := []any{
		slog.String("queue", "reducer"),
		slog.String("op", "phase_publish_post_commit_wake"),
		slog.String("error", err.Error()),
	}
	if len(rows) > 0 {
		attrs = append(attrs,
			slog.String(telemetry.LogKeyScopeID, rows[0].Key.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, rows[0].Key.GenerationID))
	}
	attrs = append(attrs, slog.String(telemetry.LogKeyOutcome, telemetry.WaitingWakeOutcomeError))
	slog.ErrorContext(ctx,
		"reducer post-commit exact phase wake failed; the audit and backstop still bound the miss",
		attrs...)
}
