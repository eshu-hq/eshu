// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRecordBackstopClaimsProbesOnlyRowsPastTheirReopen pins the #11228
// candidate filter on a recording database: a parked-class row whose
// visible_at equals its reopened_at (every wake that stamps both columns with
// the wake time) sends no probe, and a row whose visible_at is past its
// reopened_at (a backstop release, or the winners wake, which stamps
// reopened_at with the covering watermark) sends exactly one primary-key
// probe on reducer_waiting_items.
func TestRecordBackstopClaimsProbesOnlyRowsPastTheirReopen(t *testing.T) {
	t.Parallel()

	wakeTime := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	parked := &reducercontract.FailureRecord{FailureClass: reducercontract.GenerationActivationNotReadyFailureClass}

	cases := []struct {
		name       string
		intent     reducercontract.Intent
		wantProbes int
	}{
		{
			name: "woken row with both stamps at the wake time sends no statement",
			intent: reducercontract.Intent{
				IntentID: "woken", Failure: parked,
				AvailableAt: wakeTime, CycleStartedAt: wakeTime,
			},
			wantProbes: 0,
		},
		{
			name: "winners-shaped row with visible_at past the covering watermark sends one probe",
			intent: reducercontract.Intent{
				IntentID: "winners", Failure: parked,
				AvailableAt: wakeTime, CycleStartedAt: wakeTime.Add(-time.Minute),
			},
			wantProbes: 1,
		},
		{
			name: "backstop-released row sends one probe",
			intent: reducercontract.Intent{
				IntentID: "backstop", Failure: parked,
				AvailableAt: wakeTime.Add(WaitingBackstopDelay), CycleStartedAt: wakeTime,
			},
			wantProbes: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instruments, err := telemetry.NewInstruments(
				sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("backstop-candidates"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			database := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
			queue := ReducerQueue{database: database, Instruments: instruments}

			queue.recordBackstopClaims(context.Background(), []reducercontract.Intent{tc.intent})

			if got := len(database.queries); got != tc.wantProbes {
				t.Fatalf("probe statements = %d, want %d: %+v", got, tc.wantProbes, database.queries)
			}
			if tc.wantProbes == 1 {
				if got := database.queries[0].query; got != selectBackstopClaimWaitRowsQuery {
					t.Fatalf("probe query = %q, want the reducer_waiting_items primary-key probe", got)
				}
			}
		})
	}
}
