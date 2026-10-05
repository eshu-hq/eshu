// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

type memberAttemptRecorder struct {
	attempts []struct {
		ordinal int
		outcome Outcome
	}
}

func (*memberAttemptRecorder) Observe(string, Stage, Outcome, time.Duration) {}

func (r *memberAttemptRecorder) ObserveMemberAttempt(ordinal int, outcome Outcome) {
	r.attempts = append(r.attempts, struct {
		ordinal int
		outcome Outcome
	}{ordinal: ordinal, outcome: outcome})
}

func TestFleetMemberAttemptSignalsKeepOriginalOrdinal(t *testing.T) {
	recorder := &memberAttemptRecorder{}
	access := &Access{
		readerMembers: []physicalReaderMember{{ordinal: 1, maxOpen: 4}, {ordinal: 2, maxOpen: 4}},
		allocator:     newReaderAllocator([]int{4, 4}, 8), replayTimeout: time.Second,
		observer: recorder, lineage: newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(context.Background(), checkpointKey{}, checkpoint{
		owner: access, lsn: "0/1", systemID: "1", database: "postgres", incarnation: "1",
	})
	result, err := runFleet(access, ctx, 1, func(_ context.Context, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
		if reservation.member == 0 {
			return 0, syscall.ECONNREFUSED
		}
		return 42, nil
	})
	if err != nil || result != 42 {
		t.Fatalf("fleet result = %d, %v", result, err)
	}
	if len(recorder.attempts) != 2 || recorder.attempts[0].ordinal != 1 || recorder.attempts[0].outcome != OutcomeError ||
		recorder.attempts[1].ordinal != 2 || recorder.attempts[1].outcome != OutcomeOK {
		t.Fatalf("member attempt outcomes = %+v", recorder.attempts)
	}
}

func TestMemberAttemptFatalCauseDominatesJoinedDeadline(t *testing.T) {
	for _, contextErr := range []error{context.DeadlineExceeded, context.Canceled} {
		err := errors.Join(contextErr, &pgconn.PgError{Code: "28P01"})
		recorder := &memberAttemptRecorder{}
		access := &Access{observer: recorder}
		access.observeMemberAttempt(1, err)
		if len(recorder.attempts) != 1 || recorder.attempts[0].outcome != OutcomeError {
			t.Fatalf("joined auth and %v outcome = %+v, want error", contextErr, recorder.attempts)
		}
	}
}

func TestMemberAttemptCounterHasOnlyOrdinalAndClosedOutcome(t *testing.T) {
	manual := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual))
	observer, err := NewObserver(provider.Meter("test"), noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	memberObserver, ok := observer.(interface{ ObserveMemberAttempt(int, Outcome) })
	if !ok {
		t.Fatal("member attempt observer unavailable")
	}
	memberObserver.ObserveMemberAttempt(2, OutcomeError)
	memberObserver.ObserveMemberAttempt(2, Outcome("secret-reason"))
	var rm metricdata.ResourceMetrics
	if err := manual.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	points := memberAttemptPoints(t, rm)
	if len(points) != 2 || points["2/error"] != 1 || points["2/unknown"] != 1 {
		t.Fatalf("member attempt points = %v", points)
	}
}

func memberAttemptPoints(t *testing.T, rm metricdata.ResourceMetrics) map[string]int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_postgres_reader_member_attempts_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("member attempts data = %T", m.Data)
			}
			points := map[string]int64{}
			for _, point := range sum.DataPoints {
				ordinal, outcome := "", ""
				for _, attr := range point.Attributes.ToSlice() {
					switch string(attr.Key) {
					case "member_ordinal":
						ordinal = attr.Value.AsString()
					case "outcome":
						outcome = attr.Value.AsString()
					default:
						t.Fatalf("member attempts unexpected label %s", attr.Key)
					}
				}
				points[ordinal+"/"+outcome] = point.Value
			}
			return points
		}
	}
	t.Fatal("member attempt metric missing")
	return nil
}

func TestReaderMemberMetricsShowMissingOrdinalAndAllocatorPressure(t *testing.T) {
	manual := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual))
	writer, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	first, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	second, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	first.SetMaxOpenConns(4)
	second.SetMaxOpenConns(4)
	access := &Access{
		writer: writer, reader: first, readerInventoryCount: 3,
		readerMembers: []physicalReaderMember{{ordinal: 1, pool: first}, {ordinal: 2, pool: second}},
		allocator:     newReaderAllocator([]int{4, 4}, 8),
	}
	t.Cleanup(func() { _ = access.Close() })
	access.allocator.used[0] = 2
	access.allocator.total = 2
	access.allocator.waiting[1] = []*readerWaiter{{count: 4}}
	registration, err := RegisterPoolMetrics(provider.Meter("test"), access)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Unregister() })
	var rm metricdata.ResourceMetrics
	if err := manual.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]map[string]int64{
		"eshu_dp_postgres_reader_member_qualified":    {"0": 0, "1": 1, "2": 1},
		"eshu_dp_postgres_reader_member_reservations": {"1": 2, "2": 0},
		"eshu_dp_postgres_reader_member_waiters":      {"1": 0, "2": 1},
	} {
		got := memberGaugeValues(t, rm, name, "")
		if len(got) != len(want) {
			t.Fatalf("%s points = %v, want %v", name, got, want)
		}
		for ordinal, expected := range want {
			if got[ordinal] != expected {
				t.Fatalf("%s ordinal %s = %d, want %d", name, ordinal, got[ordinal], expected)
			}
		}
	}
	maxOpen := memberGaugeValues(t, rm, "eshu_dp_postgres_reader_member_connections", "max_open")
	if len(maxOpen) != 2 || maxOpen["1"] != 4 || maxOpen["2"] != 4 {
		t.Fatalf("per-member max-open gauge = %v", maxOpen)
	}
}

func TestRegisterPoolMetricsRejectsInvalidMemberOrdinal(t *testing.T) {
	manual := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual))
	writer, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
	access := &Access{
		writer: writer, reader: reader, readerInventoryCount: 1,
		readerMembers: []physicalReaderMember{{ordinal: 1, pool: reader}},
		allocator:     newReaderAllocator([]int{4}, 4),
	}
	registration, err := RegisterPoolMetrics(provider.Meter("test"), access)
	if registration != nil {
		_ = registration.Unregister()
	}
	if err == nil {
		t.Fatal("out-of-range member ordinal accepted; scrape would panic")
	}
}

func memberGaugeValues(t *testing.T, rm metricdata.ResourceMetrics, name, state string) map[string]int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("%s data = %T", name, m.Data)
			}
			values := map[string]int64{}
			for _, point := range gauge.DataPoints {
				ordinal := ""
				pointState := ""
				for _, attr := range point.Attributes.ToSlice() {
					switch string(attr.Key) {
					case "member_ordinal":
						ordinal = attr.Value.AsString()
					case "state":
						pointState = attr.Value.AsString()
					default:
						t.Fatalf("%s unexpected label %s", name, attr.Key)
					}
				}
				if ordinal == "" {
					t.Fatalf("%s point without member ordinal", name)
				}
				if state == pointState {
					values[ordinal] = point.Value
				}
			}
			return values
		}
	}
	t.Fatalf("metric %s missing", name)
	return nil
}
