// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// timeRekeyStatement runs statement for scope in a rolled-back transaction at
// the link's settings with a 60 s statement timeout, and returns its wall
// time and whether the timeout cancelled it.
func (l *ledgerDB) timeRekeyStatement(t *testing.T, statement, scopeID string) (time.Duration, bool) {
	t.Helper()
	tx, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, s := range []string{
		`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`, `SET LOCAL statement_timeout = '60s'`,
	} {
		if _, err := tx.ExecContext(l.ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	began := time.Now()
	_, err = tx.ExecContext(l.ctx, statement, scopeID, scopeID+"-g1", scopeID+"-g0", linksfreshnessstore.DigestVersion, time.Now())
	elapsed := time.Since(began)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return elapsed, true
	}
	if err != nil {
		t.Fatalf("statement: %v", err)
	}
	return elapsed, false
}

func medianDuration(values []float64) float64 {
	sort.Float64s(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

// rekeyRatio times statement on a re-keyed scope with its statistics absent
// and fresh, rounds times, first mover alternating, and returns the paired
// median of absent over fresh and whether any absent run timed out.
func (l *ledgerDB) rekeyRatio(t *testing.T, statement, scopeID string, rounds int) (float64, bool) {
	t.Helper()
	var ratios []float64
	for round := range rounds {
		times := map[string]time.Duration{}
		order := []string{"absent", "fresh"}
		if round%2 == 1 {
			order = []string{"fresh", "absent"}
		}
		for _, state := range order {
			l.plantStats(t, state, scopeID, 0)
			elapsed, timedOut := l.timeRekeyStatement(t, statement, scopeID)
			if timedOut {
				return 0, true
			}
			times[state] = elapsed
		}
		ratio := times["absent"].Seconds() / times["fresh"].Seconds()
		t.Logf("round %d (%s first): absent %s, fresh %s, ratio %.2f", round, order[0], times["absent"], times["fresh"], ratio)
		ratios = append(ratios, ratio)
	}
	return medianDuration(ratios), false
}

// TestRekeyLinkIsBoundedUnderAbsentStatistics is P2 of arbiter ruling
// arb-7127-g8: the worst case, a link that drops every key of a 45,000-key
// scope and adds as many new ones, with the scope absent from the
// statistics. Gate: paired median of absent over fresh at most 2.0 over five
// interleaved rounds, first mover alternating, 60 s statement timeout. RED:
// the frozen shipped statement times out or breaks the ratio, and form (i)
// breaks the ratio.
func TestRekeyLinkIsBoundedUnderAbsentStatistics(t *testing.T) {
	l := openLedgerDB(t)
	l.exec(t, `ALTER TABLE changed_since_key_state SET (autovacuum_enabled = false)`)
	t.Cleanup(func() { _, _ = l.raw.ExecContext(l.ctx, `ANALYZE changed_since_key_state`) })
	for i := range 20 {
		l.exec(t, `
INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state)
SELECT $1, 'content_entities', 'ent:' || k, 'content_entity', sha256(($1 || k)::bytea)
FROM generate_series(1, 5000) AS k`, "filler-"+string(rune('a'+i)))
	}
	const keys = 45000
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRekeyScope(t, w, "rekey", keys, keys)

	ratio, timedOut := l.rekeyRatio(t, linksfreshnessstore.IncrementalLinkSQL, "rekey", 5)
	if timedOut {
		t.Fatal("the shipped statement timed out at 60s with the scope absent from the statistics")
	}
	t.Logf("P2: paired median absent/fresh = %.2f (gate 2.0)", ratio)
	if ratio > 2.0 {
		t.Errorf("P2: paired median absent/fresh = %.2f, want at most 2.0", ratio)
	}

	// RED: the frozen pre-fix statement must time out or break the ratio.
	if redRatio, redTimeout := l.rekeyRatio(t, shippedIncrementalLinkSQL, "rekey", 1); !redTimeout && redRatio <= 2.0 {
		t.Errorf("the frozen shipped statement stayed within the gate (ratio %.2f); P2 cannot see the stall", redRatio)
	} else {
		t.Logf("RED shipped: timed out %v, ratio %.2f", redTimeout, redRatio)
	}
	if f := formI(t); f != "" {
		if redRatio, redTimeout := l.rekeyRatio(t, f, "rekey", 1); !redTimeout && redRatio <= 2.0 {
			t.Errorf("form (i) stayed within the gate (ratio %.2f); P2 cannot see its per-row filter", redRatio)
		} else {
			t.Logf("RED form (i): timed out %v, ratio %.2f", redTimeout, redRatio)
		}
	}
}
