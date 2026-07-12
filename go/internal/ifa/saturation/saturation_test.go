// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package saturation_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/saturation"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestUngatedBackendFloodsDeadLetters is the failing-first #3560 regression: a
// permit pool that is disabled (PermitPool <= 0, the pre-#3560-fix world) lets
// every offered write hit a capacity-C backend at once, so the surplus
// (ordinal > capacity) times out with a real GraphWriteTimeoutError, and the
// items that time out on every attempt exhaust their retry budget and
// dead-letter recoverable work. With WorkItems > MaxAttempts*BackendCapacity the
// flood is deterministic: exactly WorkItems - MaxAttempts*BackendCapacity items
// dead-letter. This test PROVES the bug shape exists so the gated test below can
// prove the gate eliminates it; if this ever stops flooding, the regression has
// lost its teeth.
func TestUngatedBackendFloodsDeadLetters(t *testing.T) {
	t.Parallel()

	cfg := saturation.Config{
		WorkItems:       8,
		BackendCapacity: 2,
		PermitPool:      0, // gate disabled — the #3560 pre-fix control
		MaxAttempts:     3,
	}
	report, err := saturation.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run(ungated) error = %v, want nil", err)
	}
	if len(report.DeadLetters) == 0 {
		t.Fatalf("ungated saturation produced no dead letters; the #3560 flood shape did not reproduce (report=%+v)", report)
	}
	// The flood cardinality is deterministic: items whose concurrent ordinal
	// exceeds capacity time out every round until their attempt budget is spent,
	// so exactly WorkItems - MaxAttempts*BackendCapacity items dead-letter. Pin
	// the exact count so a future counting-retry change that shifts the flood
	// cardinality cannot slip through with a still-positive but wrong count.
	wantDL := cfg.WorkItems - cfg.MaxAttempts*cfg.BackendCapacity
	if len(report.DeadLetters) != wantDL {
		t.Fatalf("DeadLetters count = %d, want %d (WorkItems=%d, MaxAttempts=%d, BackendCapacity=%d)",
			len(report.DeadLetters), wantDL, cfg.WorkItems, cfg.MaxAttempts, cfg.BackendCapacity)
	}
	// The whole point of #3560: these dead letters are graph_write_timeout —
	// recoverable work thrown away purely because the backend was oversubscribed.
	for _, dl := range report.DeadLetters {
		if dl.FailureClass != sourcecypher.GraphWriteTimeoutFailureClass {
			t.Fatalf("dead letter %q failure_class = %q, want %q", dl.WorkItemID, dl.FailureClass, sourcecypher.GraphWriteTimeoutFailureClass)
		}
	}
}

// TestGatedBackendDrainsCleanNoDeadLetters proves the fix: with the real
// cypher.BackpressureGate bounding in-flight writes to a permit pool no larger
// than backend capacity, no write is ever oversubscribed, so none times out,
// none dead-letters, and the queue drains to the B-12 residual (zero
// non-terminal work) after the surplus waits its turn. Backpressure must engage
// (the gate's wait observer fires) because more work is offered than the pool
// admits. This is the permanent #3560 regression: revert the gate wiring and
// PeakInFlight exceeds capacity, timeouts return, and DeadLetters repopulate.
func TestGatedBackendDrainsCleanNoDeadLetters(t *testing.T) {
	t.Parallel()

	cfg := saturation.Config{
		WorkItems:       8,
		BackendCapacity: 2,
		PermitPool:      2, // gate bounds in-flight to backend capacity
		MaxAttempts:     3,
	}
	report, err := saturation.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run(gated) error = %v, want nil", err)
	}

	if report.BackpressureEngaged <= 0 {
		t.Fatalf("BackpressureEngaged = %d, want > 0 (offered %d work over a %d permit pool must make the gate wait)",
			report.BackpressureEngaged, cfg.WorkItems, cfg.PermitPool)
	}
	if !ifa.DeadLetterSetsEqual(report.DeadLetters, nil) {
		t.Fatalf("gated saturation dead-lettered recoverable work: %+v (want none — #3560 regression)", report.DeadLetters)
	}
	if report.Residual != 0 {
		t.Fatalf("Residual = %d, want 0 (queue must drain to the B-12 residual after pressure releases)", report.Residual)
	}
	if report.PeakInFlight > cfg.PermitPool {
		t.Fatalf("PeakInFlight = %d, want <= permit pool %d (the gate must bound concurrent graph writes)",
			report.PeakInFlight, cfg.PermitPool)
	}
	if report.Succeeded != cfg.WorkItems {
		t.Fatalf("Succeeded = %d, want %d (every recoverable write must drain)", report.Succeeded, cfg.WorkItems)
	}
}

// TestGatedRetriesThenDrainsUnderTransientPressure proves the retry path is
// real, not bypassed: when the permit pool is smaller than capacity the gate
// still admits work without oversubscribing, and a transient over-capacity
// backend window forces at least one graph_write_timeout retry that then
// succeeds within budget — draining clean with zero dead letters. This guards
// the "work retries with backoff" clause of the #3560 shape distinctly from the
// zero-oversubscription case above.
func TestGatedRetriesThenDrainsUnderTransientPressure(t *testing.T) {
	t.Parallel()

	cfg := saturation.Config{
		WorkItems:         6,
		BackendCapacity:   2,
		PermitPool:        2,
		MaxAttempts:       3,
		TransientTimeouts: 2, // inject two transient timeouts that must retry-then-succeed
	}
	report, err := saturation.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run(transient) error = %v, want nil", err)
	}
	if report.Retries < cfg.TransientTimeouts {
		t.Fatalf("Retries = %d, want >= %d (transient timeouts must retry)", report.Retries, cfg.TransientTimeouts)
	}
	if !ifa.DeadLetterSetsEqual(report.DeadLetters, nil) {
		t.Fatalf("transient-pressure saturation dead-lettered work: %+v (want none)", report.DeadLetters)
	}
	if report.Residual != 0 {
		t.Fatalf("Residual = %d, want 0", report.Residual)
	}
	if report.Succeeded != cfg.WorkItems {
		t.Fatalf("Succeeded = %d, want %d", report.Succeeded, cfg.WorkItems)
	}
}

// TestConfigValidation rejects a degenerate scenario fail-closed rather than
// reporting a meaningless clean drain over zero work.
func TestConfigValidation(t *testing.T) {
	t.Parallel()

	cases := []saturation.Config{
		{WorkItems: 0, BackendCapacity: 2, PermitPool: 2},
		{WorkItems: 4, BackendCapacity: 0, PermitPool: 2},
	}
	for i, cfg := range cases {
		if _, err := saturation.Run(context.Background(), cfg); err == nil {
			t.Fatalf("case %d: Run(%+v) error = nil, want a validation error", i, cfg)
		}
	}
}
