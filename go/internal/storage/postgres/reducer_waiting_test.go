// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestWaitingDeferralMapCoversEveryNonCountingClass fails when a readiness
// class exempt from the retry budget has no waiting-wake map row (#10876,
// acceptance criterion 1). Every parked wait needs a named waits-for and a
// providing save, or the set-aside cannot wake it.
func TestWaitingDeferralMapCoversEveryNonCountingClass(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for _, entry := range WaitingDeferralMap() {
		seen[entry.FailureClass]++
	}
	for _, class := range NonCountingReducerRetryFailureClasses() {
		if seen[class] != 1 {
			t.Errorf("non-counting class %q has %d deferral map rows, want exactly 1", class, seen[class])
		}
	}
}

// TestWaitingDeferralMapCoversOwnGateCountingClasses pins the counting side
// of the map: own-gate counting classes are never parked, and the map must
// say so explicitly so a later enrollment cannot silently park a loud dead
// letter (#10876 decision 2).
func TestWaitingDeferralMapCoversOwnGateCountingClasses(t *testing.T) {
	t.Parallel()

	byClass := map[string]WaitingDeferral{}
	for _, entry := range WaitingDeferralMap() {
		byClass[entry.FailureClass] = entry
	}
	for _, class := range ownGateCountingClasses() {
		entry, ok := byClass[class]
		if !ok {
			t.Errorf("own-gate counting class %q has no deferral map row", class)
			continue
		}
		if entry.WakeSite != WaitingWakeSiteCounting {
			t.Errorf("own-gate counting class %q wake site = %q, want %q (never parked)",
				class, entry.WakeSite, WaitingWakeSiteCounting)
		}
	}
}

// TestWaitingWakeKindIsClosed pins the waits_for_kind enum (#10876 decision
// 5a): every map row names one of the seven kinds, so #10877 can narrow the
// corpus fence to per-item read sets without a schema change.
func TestWaitingWakeKindIsClosed(t *testing.T) {
	t.Parallel()

	for _, entry := range WaitingDeferralMap() {
		if !entry.WaitsForKind.Valid() {
			t.Errorf("class %q names invalid waits_for_kind %q", entry.FailureClass, string(entry.WaitsForKind))
		}
		if entry.WaitsForKind == "" {
			t.Errorf("class %q has an empty waits_for_kind", entry.FailureClass)
		}
	}
}

// TestWaitingWriteSupersededClassesAreNotWaits pins that fresher-admission
// losses are not waits: no providing save will ever arrive for that
// generation, so parking them would wait forever (#10876 decision 1vii).
func TestWaitingWriteSupersededClassesAreNotWaits(t *testing.T) {
	t.Parallel()

	byClass := map[string]WaitingDeferral{}
	for _, entry := range WaitingDeferralMap() {
		byClass[entry.FailureClass] = entry
	}
	for _, class := range []string{
		"aws_cloud_runtime_drift_write_superseded",
		"aws_cloud_runtime_drift_state_pending",
		"supply_chain_impact_write_superseded",
	} {
		entry, ok := byClass[class]
		if !ok {
			t.Errorf("write-superseded class %q has no deferral map row", class)
			continue
		}
		if entry.WakeSite != WaitingWakeSiteNotAWait {
			t.Errorf("write-superseded class %q wake site = %q, want %q",
				class, entry.WakeSite, WaitingWakeSiteNotAWait)
		}
	}
}

// TestWinnersWakeQueryAlignsCycleStartToCoveringStamp pins the #10876 bound
// alignment: the winners range wake sets reopened_at to the covering
// watermark ($2), never to the wake wall time ($1). A woken row whose cycle
// start is the wake time sits epsilon past the stamp that woke it and
// re-parks on its next claim; with the bound aligned to the stamp the
// handler recheck passes at once.
func TestWinnersWakeQueryAlignsCycleStartToCoveringStamp(t *testing.T) {
	t.Parallel()

	if !strings.Contains(wakeWinnersWatermarkQuery, "reopened_at = $2::timestamptz") {
		t.Errorf("winners wake query does not align reopened_at to the covering watermark:\n%s", wakeWinnersWatermarkQuery)
	}
	if strings.Contains(wakeWinnersWatermarkQuery, waitingWakeReopenSetClause) {
		t.Errorf("winners wake query embeds the shared set clause, whose reopened_at = $1 reintroduces the re-park ratchet")
	}
}

// winnersWakeContentionTestError wraps err the way the pgx driver surfaces
// statement failures, so the classifier proof exercises errors.As.
type winnersWakeContentionTestError struct{ err error }

func (e winnersWakeContentionTestError) Error() string { return e.err.Error() }

func (e winnersWakeContentionTestError) Unwrap() error { return e.err }

// TestClassifyWinnersWakeError pins the #7597 contention contract for the
// winners in-tx wake: lock_timeout (55P03) and deadlock (40P01) roll back
// retryably with no failure class, and every other error passes through
// unchanged.
func TestClassifyWinnersWakeError(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"55P03", "40P01"} {
		pgErr := &pgconn.PgError{Code: code, Message: "contention"}
		got := classifyWinnersWakeError(winnersWakeContentionTestError{err: pgErr})
		var contention WinnersWakeContentionError
		if !errors.As(got, &contention) {
			t.Errorf("code %s classified as %T (%v), want WinnersWakeContentionError", code, got, got)
			continue
		}
		if !contention.Retryable() {
			t.Errorf("code %s contention error is not Retryable", code)
		}
		if _, ok := any(contention).(interface{ FailureClass() string }); ok {
			t.Errorf("code %s contention error carries a FailureClass: the tick must never burn a queue budget", code)
		}
		if !errors.Is(got, pgErr) {
			t.Errorf("code %s contention error does not unwrap to the driver error", code)
		}
	}

	other := errors.New("connection reset")
	if got := classifyWinnersWakeError(winnersWakeContentionTestError{err: other}); !errors.Is(got, other) {
		t.Errorf("non-contention error = %v, want it passed through unchanged", got)
	}
	if got := classifyWinnersWakeError(other); got != other {
		t.Errorf("plain error = %v, want the identical value", got)
	}
}
