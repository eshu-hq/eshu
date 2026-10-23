// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// TestSecurityAlertCoverageProbeErrorClass pins the #8803 F4 operator signal:
// a failed collection-state probe still answers collector_state_unavailable,
// and its stage completion event names a bounded error_class. The three
// store-side classes log at WARN and count the answer; a caller cancel logs at
// INFO and skips the counter, because the caller walked away and the answer
// says nothing about collector state.
func TestSecurityAlertCoverageProbeErrorClass(t *testing.T) {
	t.Parallel()

	boundErr := fmt.Errorf("security alert targets exceed the read bound: %w", ErrSecurityAlertCollectionStateBound)
	cases := []struct {
		name      string
		store     *fakeSecurityAlertCollectionState
		cancel    bool
		wantClass string
		wantLevel string
		wantCount int64
	}{
		{
			name:      "query failed",
			store:     &fakeSecurityAlertCollectionState{configErr: errors.New("config read failed")},
			wantClass: "query_failed", wantLevel: "WARN", wantCount: 1,
		},
		{
			name:      "bound exceeded on the configuration read",
			store:     &fakeSecurityAlertCollectionState{configErr: boundErr},
			wantClass: "bound_exceeded", wantLevel: "WARN", wantCount: 1,
		},
		{
			name:      "bound exceeded on the target-state read",
			store:     &fakeSecurityAlertCollectionState{configs: []string{coverageTestRepoTarget}, stateErr: boundErr},
			wantClass: "bound_exceeded", wantLevel: "WARN", wantCount: 1,
		},
		{
			name:      "probe deadline exceeded",
			store:     &fakeSecurityAlertCollectionState{block: true},
			wantClass: "deadline_exceeded", wantLevel: "WARN", wantCount: 1,
		},
		{
			name:      "caller canceled",
			store:     &fakeSecurityAlertCollectionState{block: true},
			cancel:    true,
			wantClass: "canceled", wantLevel: "INFO", wantCount: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			signals := &coverageSignals{}
			h := signals.instrument(t, coverageTestHandler(tc.store, 0))
			code, body, resp := serveCoverage(ctx, t, h, coverageRoutes(coverageTestRepositoryID)["list"])
			if code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", code, body)
			}
			assertCoverage(t, resp, coverageWant{
				"target_incomplete", []string{"collector_state_unavailable"}, "fallback", "unavailable", "", "readiness_unavailable",
			})
			events := signals.collectionStateEvents(t)
			if len(events) != 1 {
				t.Fatalf("collection-state stage events = %d, want 1", len(events))
			}
			event := events[0]
			if got := event["error_class"]; got != tc.wantClass {
				t.Fatalf("error_class = %v, want %s", got, tc.wantClass)
			}
			if got := event["level"]; got != tc.wantLevel {
				t.Fatalf("level = %v, want %s", got, tc.wantLevel)
			}
			if got := event["error"]; got != true {
				t.Fatalf("error = %v, want true", got)
			}
			counts := signals.emptyAnswerCounts(t)
			if got := counts["target_incomplete|collector_state_unavailable"]; got != tc.wantCount {
				t.Fatalf("empty answer counter = %v, want %d target_incomplete|collector_state_unavailable", counts, tc.wantCount)
			}
		})
	}
}

// TestSecurityAlertCoverageProbeSuccessSignal keeps a classified empty answer
// at INFO with no error_class, and counts it once under its own reason.
func TestSecurityAlertCoverageProbeSuccessSignal(t *testing.T) {
	t.Parallel()

	store := &fakeSecurityAlertCollectionState{
		configs: []string{coverageTestRepoTarget},
		states:  map[string]SecurityAlertTargetState{"sa-target-repo": {LatestWorkItemStatus: "pending"}},
	}
	signals := &coverageSignals{}
	h := signals.instrument(t, coverageTestHandler(store, 0))
	code, body, _ := serveCoverage(context.Background(), t, h, coverageRoutes(coverageTestRepositoryID)["count"])
	if code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", code, body)
	}
	events := signals.collectionStateEvents(t)
	if len(events) != 1 {
		t.Fatalf("collection-state stage events = %d, want 1", len(events))
	}
	if _, ok := events[0]["error_class"]; ok {
		t.Fatalf("successful probe carries error_class %v", events[0]["error_class"])
	}
	if got := events[0]["level"]; got != "INFO" {
		t.Fatalf("level = %v, want INFO", got)
	}
	if got := signals.emptyAnswerCounts(t)["target_incomplete|no_committed_generation"]; got != 1 {
		t.Fatalf("empty answer counter for no_committed_generation = %d, want 1", got)
	}
}
