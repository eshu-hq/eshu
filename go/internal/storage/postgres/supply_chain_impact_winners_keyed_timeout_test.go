// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestWinnersResweepTimeoutClass pins the Dec 7 guard's distinct failure
// class: a server-side statement-timeout abort (SQLSTATE 57014) classifies as
// statement_timeout, including through the tick's error wrapping; every other
// error classifies as nothing here (deadlock and serialization_failure keep
// their winnersWriteConflictClass lane).
func TestWinnersResweepTimeoutClass(t *testing.T) {
	t.Parallel()

	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	if got := winnersResweepTimeoutClass(timeout); got != "statement_timeout" {
		t.Errorf("winnersResweepTimeoutClass(57014) = %q, want statement_timeout", got)
	}
	wrapped := &pgconn.PgError{Code: "57014"}
	if got := winnersResweepTimeoutClass(wrapResweepTimeout(wrapped)); got != "statement_timeout" {
		t.Errorf("winnersResweepTimeoutClass(wrapped 57014) = %q, want statement_timeout", got)
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"deadlock", &pgconn.PgError{Code: "40P01"}},
		{"serialization failure", &pgconn.PgError{Code: "40001"}},
		{"lock timeout", &pgconn.PgError{Code: "55P03"}},
		{"unique violation", &pgconn.PgError{Code: "23505"}},
		{"non-pg error", errors.New("boom")},
	} {
		if got := winnersResweepTimeoutClass(tc.err); got != "" {
			t.Errorf("winnersResweepTimeoutClass(%s) = %q, want no class", tc.name, got)
		}
	}
}

// wrapResweepTimeout wraps err the way the tick wraps a failed phase
// statement, so the classifier proof covers the wrapped shape.
func wrapResweepTimeout(err error) error {
	return errors.Join(errors.New("build supply chain impact winners keyed narrow probe"), err)
}

// TestWinnersResweepStatementTimeoutDefault pins the Dec 7 guard sizing: the
// zero-value store resolves the default (120 s, about 6x above the largest
// healthy post-fix statement and below the incident class), and an explicit
// value overrides it. A zero or negative value never disables the guard.
func TestWinnersResweepStatementTimeoutDefault(t *testing.T) {
	t.Parallel()

	var empty SupplyChainImpactWinnersStore
	if got := empty.resweepStatementTimeout(); got != 120*time.Second {
		t.Fatalf("zero-value resweepStatementTimeout() = %v, want 120s", got)
	}
	custom := SupplyChainImpactWinnersStore{ResweepStatementTimeout: 30 * time.Second}
	if got := custom.resweepStatementTimeout(); got != 30*time.Second {
		t.Fatalf("custom resweepStatementTimeout() = %v, want 30s", got)
	}
	negative := SupplyChainImpactWinnersStore{ResweepStatementTimeout: -time.Second}
	if got := negative.resweepStatementTimeout(); got != 120*time.Second {
		t.Fatalf("negative resweepStatementTimeout() = %v, want the 120s default (no off switch)", got)
	}
}

// TestResweepWinnersKeyedPinsStatementTimeout pins that the keyed tick pins
// the Dec 7 guard as its transaction's first statement, before the claim:
// no keyed statement runs unguarded.
func TestResweepWinnersKeyedPinsStatementTimeout(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true}
	store, _ := newPhaseStore(t, tx)
	if _, err := store.ResweepWinnersKeyed(t.Context()); err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v", err)
	}
	for i, q := range tx.queries {
		if q != pinWinnersResweepStatementTimeoutSQL {
			continue
		}
		if i != 0 {
			t.Fatalf("statement-timeout pin ran at position %d, want first (before the claim)", i)
		}
		return
	}
	t.Fatalf("statement-timeout pin %q not issued; tick queries: %q", pinWinnersResweepStatementTimeoutSQL, tx.queries)
}

// TestWinnersResweepTimeoutFormatMatchesSetConfig pins the guard value
// rendering: milliseconds into set_config, following the winners wake
// lock_timeout precedent.
func TestWinnersResweepTimeoutFormatMatchesSetConfig(t *testing.T) {
	t.Parallel()

	if got := formatWinnersResweepTimeout(120 * time.Second); got != "120000ms" {
		t.Errorf("formatWinnersResweepTimeout(120s) = %q, want 120000ms", got)
	}
	// A positive sub-millisecond timeout clamps to 1 ms: Postgres reads
	// "0ms" as no timeout, so rendering one would silently drop the guard.
	if got := formatWinnersResweepTimeout(500 * time.Microsecond); got != "1ms" {
		t.Errorf("formatWinnersResweepTimeout(500us) = %q, want 1ms", got)
	}
	if !strings.Contains(pinWinnersResweepStatementTimeoutSQL, "set_config('statement_timeout'") {
		t.Errorf("pin statement does not set_config statement_timeout: %q", pinWinnersResweepStatementTimeoutSQL)
	}
}
