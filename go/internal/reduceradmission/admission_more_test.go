// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "250",
		"ESHU_REDUCER_ADMISSION_POLL_INTERVAL":   "250ms",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := config.HighWaterMark, int64(250); got != want {
		t.Fatalf("HighWaterMark = %d, want %d", got, want)
	}
	if got, want := config.PollInterval, 250*time.Millisecond; got != want {
		t.Fatalf("PollInterval = %s, want %s", got, want)
	}
}

func TestLoadConfigDefaultsToEnabledHighWaterMark(t *testing.T) {
	t.Parallel()

	const wantDefaultHighWaterMark int64 = 10000

	config, err := LoadConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got := config.HighWaterMark; got != wantDefaultHighWaterMark {
		t.Fatalf("HighWaterMark = %d, want default %d", got, wantDefaultHighWaterMark)
	}
	if got, want := config.PollInterval, defaultPollInterval; got != want {
		t.Fatalf("PollInterval = %s, want %s", got, want)
	}
	if !config.enabled() {
		t.Fatal("default reducer admission config is disabled, want enabled")
	}
}

func TestLoadConfigExplicitZeroDisablesTotalDepthGate(t *testing.T) {
	t.Parallel()

	// Explicit zero on the total-depth mark disables the total-depth gate, but
	// the writer stays enabled because the graph-write pressure gate defaults on.
	config, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "0",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got := config.HighWaterMark; got != 0 {
		t.Fatalf("HighWaterMark = %d, want explicit disabled value 0", got)
	}
	if !config.enabled() {
		t.Fatal("writer disabled with total-depth gate off, want enabled via graph-write gate")
	}
	if !config.graphWritePressureEnabled() {
		t.Fatal("graph-write pressure gate disabled, want enabled by default")
	}
}

func TestLoadConfigBothGatesExplicitZeroDisablesWriter(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK":          "0",
		"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "0",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if config.enabled() {
		t.Fatal("both gates explicit zero is enabled, want disabled")
	}
}

func TestLoadConfigRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "-1",
	}))
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want invalid config error")
	}
}

func TestWrapIntentWriterUsesDefaultHighWaterMark(t *testing.T) {
	t.Parallel()

	const wantDefaultHighWaterMark int64 = 10000

	wrapped, err := WrapIntentWriter(
		postgres.SQLDB{},
		&recordingIntentWriter{},
		mapGetenv(nil),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("WrapIntentWriter() error = %v, want nil", err)
	}
	admission, ok := wrapped.(writer)
	if !ok {
		t.Fatalf("writer type = %T, want reduceradmission.writer", wrapped)
	}
	if got := admission.config.HighWaterMark; got != wantDefaultHighWaterMark {
		t.Fatalf("HighWaterMark = %d, want default %d", got, wantDefaultHighWaterMark)
	}
}

func TestWrapIntentWriterWrapsRealWriter(t *testing.T) {
	t.Parallel()

	wrapped, err := WrapIntentWriter(
		postgres.SQLDB{},
		&recordingIntentWriter{},
		mapGetenv(map[string]string{
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("WrapIntentWriter() error = %v, want nil", err)
	}
	if _, ok := wrapped.(writer); !ok {
		t.Fatalf("writer type = %T, want reduceradmission.writer", wrapped)
	}
}

func TestWrapIntentWriterRejectsNilDatabase(t *testing.T) {
	t.Parallel()

	_, err := WrapIntentWriter(
		nil,
		&recordingIntentWriter{},
		mapGetenv(map[string]string{
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("WrapIntentWriter() error = nil, want error for nil database with gate enabled")
	}
}

func TestWrapIntentWriterDisabledReturnsInnerUnchanged(t *testing.T) {
	t.Parallel()

	inner := &recordingIntentWriter{}
	wrapped, err := WrapIntentWriter(
		nil,
		inner,
		mapGetenv(map[string]string{
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK":          "0",
			"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "0",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("WrapIntentWriter() error = %v, want nil", err)
	}
	if wrapped != projector.ReducerIntentWriter(inner) {
		t.Fatalf("writer = %v, want inner writer unchanged when gate is disabled", wrapped)
	}
}

func BenchmarkReducerAdmissionDisabled(b *testing.B) {
	ctx := context.Background()
	inner := &countingIntentWriter{}
	admission := writer{
		inner:  inner,
		config: Config{},
	}
	intents := []projector.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := admission.Enqueue(ctx, intents); err != nil {
			b.Fatalf("Enqueue() error = %v, want nil", err)
		}
	}
}

func BenchmarkReducerAdmissionBelowHighWater(b *testing.B) {
	ctx := context.Background()
	inner := &countingIntentWriter{}
	admission := writer{
		inner: inner,
		depthReader: fixedDepthReader{
			depth: map[string]map[string]int64{"reducer": {"pending": 4}},
		},
		config: Config{
			HighWaterMark: 10,
			PollInterval:  time.Second,
		},
	}
	intents := []projector.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := admission.Enqueue(ctx, intents); err != nil {
			b.Fatalf("Enqueue() error = %v, want nil", err)
		}
	}
}

func BenchmarkReducerAdmissionDefaultBelowHighWater(b *testing.B) {
	ctx := context.Background()
	config, err := LoadConfig(mapGetenv(nil))
	if err != nil {
		b.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	inner := &countingIntentWriter{}
	admission := writer{
		inner: inner,
		depthReader: fixedDepthReader{
			depth: map[string]map[string]int64{"reducer": {"pending": defaultHighWaterMark - 1}},
		},
		config: config,
	}
	intents := []projector.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := admission.Enqueue(ctx, intents); err != nil {
			b.Fatalf("Enqueue() error = %v, want nil", err)
		}
	}
}

func BenchmarkReducerAdmissionOneDeferral(b *testing.B) {
	ctx := context.Background()
	inner := &countingIntentWriter{}
	admission := writer{
		inner: inner,
		depthReader: &alternatingDepthReader{
			high: map[string]map[string]int64{"reducer": {"pending": 10}},
			low:  map[string]map[string]int64{"reducer": {"pending": 4}},
		},
		config: Config{
			HighWaterMark: 10,
			PollInterval:  time.Second,
		},
		sleep: func(context.Context, time.Duration) error {
			return nil
		},
	}
	intents := []projector.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := admission.Enqueue(ctx, intents); err != nil {
			b.Fatalf("Enqueue() error = %v, want nil", err)
		}
	}
}
