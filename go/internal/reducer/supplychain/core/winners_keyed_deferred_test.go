// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestWinnersMaintainerDefersAStaleAuditDistinctFromSkipped proves the #10416
// slice-1 deferred outcome: a refused stale candidate scan ends the tick with
// no error and no fallback, and it is neither a skip (the lock was never
// attempted) nor a failure. It records no duration, no repairs, no capture,
// and no skipped-audit count.
func TestWinnersMaintainerDefersAStaleAuditDistinctFromSkipped(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		result SupplyChainImpactWinnersAudit
	}{
		{"deferred", SupplyChainImpactWinnersAudit{Deferred: true}},
		// A defensive both-true result still defers: the scan never reached
		// the writer, so the lock was never attempted.
		{"deferred wins over skipped", SupplyChainImpactWinnersAudit{Deferred: true, Skipped: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inst, reader := winnersSignalsHarness(t)
			var logs bytes.Buffer
			rebuilder := &fakeKeyedRebuilder{}
			auditor := &fakeWinnersAuditor{result: tc.result}
			m := SupplyChainImpactWinnersMaintainer{
				Rebuilder: rebuilder, KeyedRebuilder: rebuilder,
				LeaseManager: &fakeWinnersLeaseManager{claim: true},
				Auditor:      auditor, Instruments: inst,
				Logger: slog.New(slog.NewTextHandler(&logs, nil)),
			}
			if err := m.runAuditOnce(context.Background()); err != nil {
				t.Fatalf("runAuditOnce() error = %v, want nil: a deferral is an outcome, not a failure", err)
			}
			if auditor.count() != 1 {
				t.Fatalf("auditor calls = %d, want 1: no retry, no fallback", auditor.count())
			}
			got := collectKeyedMetrics(t, reader)
			if got.durationByPath["audit"] != 0 {
				t.Errorf("audit duration samples = %d, want none: no audit ran", got.durationByPath["audit"])
			}
			if got.repairs != 0 || len(got.captureBySrc) != 0 {
				t.Errorf("deferred audit recorded repairs=%d capture=%v, want none", got.repairs, got.captureBySrc)
			}
			if got.auditSkipped != 0 {
				t.Errorf("audit skipped = %d, want 0: a deferral is distinct from a skip", got.auditSkipped)
			}
			if !strings.Contains(logs.String(), "deferred") {
				t.Errorf("deferred audit logged nothing with 'deferred':\n%s", logs.String())
			}
		})
	}
}
