// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"testing"
	"time"
)

func TestGHSAWindowOutcomeIsClosed(t *testing.T) {
	t.Parallel()
	outcomes := GHSAWindowOutcomes()
	if got := outcomes[len(outcomes)-1]; got != GHSAWindowOutcomeOther {
		t.Fatalf("last outcome = %q, want %q", got, GHSAWindowOutcomeOther)
	}
	for _, outcome := range outcomes {
		if got := GHSAWindowOutcome(outcome); got != outcome {
			t.Fatalf("GHSAWindowOutcome(%q) = %q, want it unchanged", outcome, got)
		}
	}
	for _, unknown := range []string{"", "ADVANCED", "skipped"} {
		if got := GHSAWindowOutcome(unknown); got != GHSAWindowOutcomeOther {
			t.Fatalf("GHSAWindowOutcome(%q) = %q, want %q", unknown, got, GHSAWindowOutcomeOther)
		}
	}
	outcomes[0] = "mutated"
	if GHSAWindowOutcomes()[0] == "mutated" {
		t.Fatal("GHSAWindowOutcomes returned the backing array, want a copy")
	}
}

func TestGHSACursorLagTrackerKeepsMaxAndFloorsLag(t *testing.T) {
	t.Parallel()
	tracker := NewGHSACursorLagTracker()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tracker.RecordCursorPosition("reviewed", now.Add(-2*time.Hour).UnixMicro())
	// A stale out-of-order record must not move the series backward.
	tracker.RecordCursorPosition("reviewed", now.Add(-3*time.Hour).UnixMicro())
	tracker.RecordCursorPosition("", now.UnixMicro())
	lags := tracker.LagSecondsByAdvisoryType(now)
	if got := lags["reviewed"]; got != 7200 {
		t.Fatalf("reviewed lag = %v, want 7200", got)
	}
	if len(lags) != 1 {
		t.Fatalf("lags = %v, want only the reviewed series", lags)
	}
	// A cursor ahead of the scrape clock floors at zero, never negative.
	tracker.RecordCursorPosition("malware", now.Add(time.Hour).UnixMicro())
	if got := tracker.LagSecondsByAdvisoryType(now)["malware"]; got != 0 {
		t.Fatalf("malware lag = %v, want 0", got)
	}
	var nilTracker *GHSACursorLagTracker
	nilTracker.RecordCursorPosition("reviewed", 1)
	if lags := nilTracker.LagSecondsByAdvisoryType(now); len(lags) != 0 {
		t.Fatalf("nil tracker lags = %v, want empty", lags)
	}
}
