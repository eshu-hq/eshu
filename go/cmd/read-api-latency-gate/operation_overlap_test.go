// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"
)

func TestConcurrentPilotRequiresObservedOverlap(t *testing.T) {
	result := RouteLatency{Route: "GET /pilot", Exercised: true, Requested: 20, Succeeded: 20, Workers: 4, PeakInFlight: 1, P95: time.Millisecond}
	budgets := RouteBudgets{def: time.Second}
	if err := validateConcurrentPilotResult(result, budgets); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("serialized evidence accepted: %v", err)
	}
	result.PeakInFlight = 4
	if err := validateConcurrentPilotResult(result, budgets); err != nil {
		t.Fatalf("overlapping evidence rejected: %v", err)
	}
}
