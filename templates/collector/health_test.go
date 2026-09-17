// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"
)

// TestOperatorHealth proves operators can inspect liveness, freshness,
// retries, failures, and bounded resource use.
func TestOperatorHealth(t *testing.T) {
	t.Parallel()

	monitor := NewMonitor()
	health, resource := monitor.Snapshot()
	if !health.Alive {
		t.Fatal("Alive = false, want true")
	}
	if resource.MaxRecordsPerClaim <= 0 || resource.MaxPayloadBytes <= 0 || resource.ClaimTimeoutSeconds <= 0 {
		t.Fatalf("resource bounds unset: %+v", resource)
	}
	at := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	monitor.ObserveSuccess("digest-1", at)
	monitor.ObserveFailure("transient source timeout")
	health, _ = monitor.Snapshot()
	if !health.LastSuccessAt.Equal(at) {
		t.Fatalf("LastSuccessAt = %v, want %v", health.LastSuccessAt, at)
	}
	if health.LastDigest != "digest-1" {
		t.Fatalf("LastDigest = %q, want digest-1", health.LastDigest)
	}
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", health.ConsecutiveFailures)
	}
	if health.LastError == "" {
		t.Fatal("LastError empty after failure")
	}
	for i := 0; i < 8; i++ {
		if err := monitor.ObserveRetryNevertheless(); err != nil {
			t.Fatalf("ObserveRetryNevertheless() error = %v on iteration %d", err, i)
		}
	}
	if err := monitor.ObserveRetryNevertheless(); err == nil {
		t.Fatal("retry past the bound must fail terminal")
	}
}
