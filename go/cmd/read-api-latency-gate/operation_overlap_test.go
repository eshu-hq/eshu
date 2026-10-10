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

func TestConcurrentPilotOptionBoundsFailBeforeSeeding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		workers  int
		requests int
		valid    bool
	}{
		{name: "disabled", workers: 0, requests: 0, valid: true},
		{name: "minimum", workers: 2, requests: 2, valid: true},
		{name: "maximum", workers: 16, requests: 1000, valid: true},
		{name: "negative workers", workers: -1, requests: 20},
		{name: "one worker", workers: 1, requests: 20},
		{name: "too many workers", workers: 17, requests: 20},
		{name: "too few requests", workers: 4, requests: 3},
		{name: "too many requests", workers: 4, requests: 1001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := runOptions{postgresDSN: "fixture", mcpBaseURL: "http://fixture", concurrentWorkers: tc.workers, concurrentRequests: tc.requests}
			err := validateRunOptions(opts)
			if tc.valid && err != nil {
				t.Fatalf("valid flags rejected before seeding: %v", err)
			}
			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "concurrent")) {
				t.Fatalf("invalid flags reached seeding: %v", err)
			}
		})
	}
}
