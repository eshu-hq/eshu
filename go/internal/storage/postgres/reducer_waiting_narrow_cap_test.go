// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #11297 arbiter conditions 1-3 (hermetic): a narrowing-carrying deferral that
// cannot be represented parks nothing, the whole index row is bounded, and a
// park that Postgres still rejects with SQLSTATE 54000 never ends the process.

// narrowCapCause is a retryable workload resolution deferral that carries the
// verdict-time sets, as the real error does.
type narrowCapCause struct {
	repos    []string
	blocking []string
}

func (e narrowCapCause) Error() string { return "narrow cap cause" }

func (e narrowCapCause) Retryable() bool { return true }

func (e narrowCapCause) FailureClass() string {
	return "workload_materialization_resolution_not_ready"
}

func (e narrowCapCause) CorpusWaitRepoIDs() []string { return e.repos }

func (e narrowCapCause) CorpusWaitBlockingScopeIDs() []string { return e.blocking }

// TestNarrowedWaitUnrepresentableParksNothing: when the first blocking id alone
// cannot fit the key budget, the deferral takes the queue's timer path. It must
// not fall to the coarse {scope_id, generation_id} key: that wait is already
// satisfied at park time (the own generation is active) and would reopen the
// waiter in a tight loop while the fence stays closed.
func TestNarrowedWaitUnrepresentableParksNothing(t *testing.T) {
	t.Parallel()
	database := &fakeExecQueryer{}
	cause := narrowCapCause{blocking: []string{strings.Repeat("s", 5000)}}
	parked, err := setAsideWaitingForCause(context.Background(), database, nil,
		narrowUnitIntent(), narrowUnitEntry(), cause, nil, time.Now().UTC())
	if err != nil || parked {
		t.Fatalf("setAsideWaitingForCause() = (%v, %v), want (false, nil)", parked, err)
	}
	if len(database.execs) != 0 || len(database.queries) != 0 {
		t.Fatalf("an unrepresentable narrowed wait touched storage: execs=%d queries=%d, want none (no coarse fallback key)",
			len(database.execs), len(database.queries))
	}
}

// TestNarrowedWaitBoundsTheWholeIndexRow: the btree row holds work_item_id and
// the kind next to the key, and the work_item_id is unbounded text. The total
// must stay under the cap whatever the id length.
func TestNarrowedWaitBoundsTheWholeIndexRow(t *testing.T) {
	t.Parallel()
	const indexRowCap = 2000 // counted row cap; 376 bytes of real headroom under Postgres's 2704-byte btree limit
	for _, idLen := range []int{140, 600, 1500, 3000} {
		idLen := idLen
		t.Run(fmt.Sprintf("work_item_id_%d", idLen), func(t *testing.T) {
			t.Parallel()
			database := &boundWaitingDB{}
			intent := narrowUnitIntent()
			intent.IntentID = strings.Repeat("w", idLen)
			cause := narrowCapCause{repos: manyScopeIDs(400), blocking: manyScopeIDs(600)}
			if _, err := setAsideWaitingForCause(context.Background(), database, nil,
				intent, narrowUnitEntry(), cause, nil, time.Now().UTC()); err != nil {
				t.Fatalf("setAsideWaitingForCause() error = %v", err)
			}
			for _, key := range database.insertedKeys {
				row := len(intent.IntentID) + len(WaitingKindRelationshipGenerationActive) + len(key)
				if row > indexRowCap {
					t.Fatalf("index row is %d bytes (work_item_id %d), want <= %d", row, idLen, indexRowCap)
				}
			}
		})
	}
}

func keyBoundedCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "eshu_dp_reducer_waiting_key_bounded_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("key bounded metric data is %T, want Sum[int64]", metric.Data)
			}
			for _, point := range sum.DataPoints {
				reason, _ := point.Attributes.Value("reason")
				out[reason.AsString()] += point.Value
			}
		}
	}
	return out
}

// TestReducerQueueFailSurvivesAnIndexRowRejection: a park INSERT rejected with
// SQLSTATE 54000 (btree row too large) leaves the retry UPDATE committed and
// the backstop bounding the wait, so Fail logs, counts, and returns nil. Any
// other park error keeps today's behaviour and reaches the caller.
func TestReducerQueueFailSurvivesAnIndexRowRejection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pgCode  string
		wantErr bool
		want    map[string]int64
	}{
		{name: "54000 is survived and counted", pgCode: "54000", want: map[string]int64{"index_row_rejected": 1}},
		{name: "another SQLSTATE still fails", pgCode: "40001", wantErr: true, want: map[string]int64{}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("cap"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			database := &fakeExecQueryer{execErrors: []error{&pgconn.PgError{Code: tc.pgCode, Message: "park insert rejected"}}}
			queue := ReducerQueue{
				database: database, LeaseOwner: "reducer-1", LeaseDuration: time.Minute,
				RetryDelay: time.Minute, MaxAttempts: 3, Instruments: instruments,
				Now: func() time.Time { return time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC) },
			}
			intent := reducercontract.Intent{
				IntentID: "wi-cap", ScopeID: "scope-cap", GenerationID: "gen-cap",
				Domain: "workload_materialization", AttemptCount: 1,
			}
			err = queue.Fail(context.Background(), intent, narrowCapCause{blocking: []string{"scope-a"}})
			if tc.wantErr {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != tc.pgCode {
					t.Fatalf("Fail() error = %v, want the %s park error to reach the caller", err, tc.pgCode)
				}
			} else if err != nil {
				t.Fatalf("Fail() error = %v, want nil: a rejected park must not end the process", err)
			}
			got := keyBoundedCounts(t, reader)
			for reason, want := range tc.want {
				if got[reason] != want {
					t.Fatalf("key bounded counts = %v, want %v", got, tc.want)
				}
			}
			if len(tc.want) == 0 && len(got) != 0 {
				t.Fatalf("key bounded counts = %v, want none", got)
			}
		})
	}
}
