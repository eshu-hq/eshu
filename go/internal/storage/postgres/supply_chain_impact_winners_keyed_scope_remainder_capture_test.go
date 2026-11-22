// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"strings"
	"testing"
)

// convertCarriesScopeCapture reports why stmt does not keep the scope row's
// capture time on a converted remainder row (#11145): the INSERT must name
// captured_at, the SELECT arm must feed MIN(c.captured_at) from the admitted
// scope rows, and the conflict arm must keep the earlier capture with LEAST.
// A nil error means the statement holds the contract.
func convertCarriesScopeCapture(stmt string) error {
	header, _, ok := strings.Cut(stmt, "SELECT 'fact'")
	if !ok {
		return errors.New("no SELECT arm")
	}
	if !strings.Contains(header, "advisory_id, captured_at)") {
		return errors.New("INSERT column list does not name captured_at: the row takes a fresh clock_timestamp() default")
	}
	if !strings.Contains(stmt, "si.advisory_id, MIN(c.captured_at)\n") {
		return errors.New("SELECT arm does not carry MIN(c.captured_at) of the scope rows")
	}
	_, tail, ok := strings.Cut(stmt, "DO UPDATE SET")
	if !ok {
		return errors.New("no conflict arm")
	}
	if !strings.Contains(tail, "captured_at = LEAST(supply_chain_impact_winners_dirty_log.captured_at, EXCLUDED.captured_at)") {
		return errors.New("conflict arm does not keep the earlier capture with LEAST")
	}
	return nil
}

// TestWinnersConvertRemainderKeepsTheScopeCapture pins the #11145 contract on
// the shipped convert, and its seeded-violation half: each planted regression
// (a fresh capture, a conversion-time arm, a later-wins conflict, an
// overwrite) fails the same check.
func TestWinnersConvertRemainderKeepsTheScopeCapture(t *testing.T) {
	t.Parallel()

	if err := convertCarriesScopeCapture(convertWinnersKeyedScopeRemainderSQL); err != nil {
		t.Fatalf("shipped convert: %v", err)
	}
	least := "captured_at = LEAST(supply_chain_impact_winners_dirty_log.captured_at, EXCLUDED.captured_at)"
	for name, seeded := range map[string]string{
		"fresh capture": strings.Replace(convertWinnersKeyedScopeRemainderSQL, "advisory_id, captured_at)", "advisory_id)", 1),
		"conversion-time arm": strings.Replace(convertWinnersKeyedScopeRemainderSQL,
			"MIN(c.captured_at)\n", "clock_timestamp()\n", 1),
		"later wins": strings.Replace(convertWinnersKeyedScopeRemainderSQL, least,
			"captured_at = GREATEST(supply_chain_impact_winners_dirty_log.captured_at, EXCLUDED.captured_at)", 1),
		"overwrite": strings.Replace(convertWinnersKeyedScopeRemainderSQL, least, "captured_at = EXCLUDED.captured_at", 1),
	} {
		if seeded == convertWinnersKeyedScopeRemainderSQL {
			t.Fatalf("%s: the seed did not change the statement", name)
		}
		if err := convertCarriesScopeCapture(seeded); err == nil {
			t.Errorf("%s: the seeded regression passed the capture check", name)
		}
	}
}
