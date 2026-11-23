// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// massProbeTx adapts the shared exec/query fake to a read-only transaction.
type massProbeTx struct{ *fakeExecQueryer }

func (massProbeTx) Commit() error   { return nil }
func (massProbeTx) Rollback() error { return nil }

type massProbeBeginner struct{ tx massProbeTx }

func (b massProbeBeginner) Begin(context.Context) (db.Transaction, error) { return b.tx, nil }

// TestMassProbeTransactionTurnsOffSeqScanBeforeItsStatement pins the
// arbiter-b2b-gin-covering decision 3 setting: the mass-probe transaction sets
// enable_seqscan = off (transaction-local) after its statement timeout and before
// the readiness check and the probe, and no other holder read sets it. The probe's
// per-id LIMIT lets a sequential scan beat the GIN scan on cost whenever the GIN
// scan is dear (an unflushed pending list, a larger table), and a probe for an id
// that matches nothing then reads the whole supports table.
func TestMassProbeTransactionTurnsOffSeqScanBeforeItsStatement(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{}, // statement_timeout
		{}, // enable_seqscan
		{rows: [][]any{{true}}},
		{rows: [][]any{{"CVE-2026-1", int64(7)}}},
	}}
	store := SupplyChainImpactFanOutStore{Beginner: massProbeBeginner{massProbeTx{fake}}, DB: fake}
	masses, err := store.EstimateHolderMasses(context.Background(), []string{"CVE-2026-1"})
	if err != nil || masses["CVE-2026-1"] != 7 {
		t.Fatalf("EstimateHolderMasses() = %v, %v, want mass 7", masses, err)
	}
	seqScanAt, probeAt := -1, -1
	for i, q := range fake.queries {
		switch {
		case strings.Contains(q.query, "set_config('enable_seqscan', 'off', true)"):
			seqScanAt = i
		case strings.Contains(q.query, "LEAST(COALESCE(cve.mass"):
			probeAt = i
		}
	}
	if seqScanAt < 0 || probeAt < 0 || seqScanAt > probeAt {
		t.Fatalf("enable_seqscan at %d, probe at %d: the transaction-local setting must precede the probe (queries: %d)", seqScanAt, probeAt, len(fake.queries))
	}
	if !strings.Contains(fake.queries[0].query, "statement_timeout") {
		t.Fatalf("first statement = %q, want the statement timeout", fake.queries[0].query)
	}
}

// TestHolderStatementsNeverSetEnableSeqscan pins that the setting stays on the
// mass probe only: the holder and key-range statements keep the planner's own
// choice (arbiter-hotkey-page decision 10).
func TestHolderStatementsNeverSetEnableSeqscan(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"holders":     FindSupplyChainImpactHoldersQuery,
		"key range":   FindSupplyChainImpactHolderKeyRangeQuery,
		"open range":  FindSupplyChainImpactHolderOpenKeyRangeQuery,
		"set timeout": setSupplyChainImpactFanOutReadTimeoutQuery,
		"plan mode":   setHolderKeyRangePlanModeQuery,
		"snapshot":    setHolderKeyRangeSnapshotQuery,
	} {
		if strings.Contains(text, "enable_seqscan") {
			t.Errorf("%s statement text mentions enable_seqscan", name)
		}
	}
}
