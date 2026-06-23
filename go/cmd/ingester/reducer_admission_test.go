package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestReducerAdmissionDefersAtHighWaterAndResumesBelow(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": 7, "retrying": 1, "in_flight": 2}},
			{"reducer": {"pending": 10}},
			{"reducer": {"pending": 4}},
		},
	}
	writer := &recordingReducerIntentWriter{}
	sleeps := 0
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10,
			PollInterval:  time.Second,
		},
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}
	intents := []projector.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

	result, err := admission.Enqueue(context.Background(), intents)
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if result.Count != len(intents) {
		t.Fatalf("Enqueue() count = %d, want %d", result.Count, len(intents))
	}
	if got, want := sleeps, 2; got != want {
		t.Fatalf("sleep count = %d, want %d", got, want)
	}
	if got, want := reader.calls, 3; got != want {
		t.Fatalf("depth read count = %d, want %d", got, want)
	}
	if got, want := writer.calls, 1; got != want {
		t.Fatalf("inner enqueue count = %d, want %d", got, want)
	}
}

func TestReducerAdmissionDefaultConfigDefersAtDefaultHighWaterAndResumesBelow(t *testing.T) {
	t.Parallel()

	config, err := loadReducerAdmissionConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": defaultReducerAdmissionHighWaterMark}},
			{"reducer": {"pending": defaultReducerAdmissionHighWaterMark - 1}},
		},
	}
	writer := &recordingReducerIntentWriter{}
	sleeps := 0
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config:      config,
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}

	result, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if result.Count != 1 {
		t.Fatalf("Enqueue() count = %d, want 1", result.Count)
	}
	if got, want := sleeps, 1; got != want {
		t.Fatalf("sleep count = %d, want %d", got, want)
	}
	if got, want := reader.calls, 2; got != want {
		t.Fatalf("depth read count = %d, want %d", got, want)
	}
	if got, want := writer.calls, 1; got != want {
		t.Fatalf("inner enqueue count = %d, want %d", got, want)
	}
}

func TestReducerAdmissionContextCancellationStopsBeforeEnqueue(t *testing.T) {
	t.Parallel()

	expectedErr := context.Canceled
	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 10}}},
	}
	writer := &recordingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10,
			PollInterval:  time.Second,
		},
		sleep: func(context.Context, time.Duration) error {
			return expectedErr
		},
	}

	_, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Enqueue() error = %v, want %v", err, expectedErr)
	}
	if writer.calls != 0 {
		t.Fatalf("inner enqueue count = %d, want 0", writer.calls)
	}
}

func TestReducerAdmissionDisabledSkipsDepthRead(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 100}}},
	}
	writer := &recordingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config:      reducerAdmissionConfig{},
		sleep: func(context.Context, time.Duration) error {
			t.Fatal("sleep should not run when admission is disabled")
			return nil
		},
	}

	if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if reader.calls != 0 {
		t.Fatalf("depth read count = %d, want 0", reader.calls)
	}
	if writer.calls != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", writer.calls)
	}
}

func TestLoadReducerAdmissionConfig(t *testing.T) {
	t.Parallel()

	config, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "250",
		"ESHU_REDUCER_ADMISSION_POLL_INTERVAL":   "250ms",
	}))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	if got, want := config.HighWaterMark, int64(250); got != want {
		t.Fatalf("HighWaterMark = %d, want %d", got, want)
	}
	if got, want := config.PollInterval, 250*time.Millisecond; got != want {
		t.Fatalf("PollInterval = %s, want %s", got, want)
	}
}

func TestLoadReducerAdmissionConfigDefaultsToEnabledHighWaterMark(t *testing.T) {
	t.Parallel()

	const wantDefaultHighWaterMark int64 = 10000

	config, err := loadReducerAdmissionConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	if got := config.HighWaterMark; got != wantDefaultHighWaterMark {
		t.Fatalf("HighWaterMark = %d, want default %d", got, wantDefaultHighWaterMark)
	}
	if got, want := config.PollInterval, defaultReducerAdmissionPoll; got != want {
		t.Fatalf("PollInterval = %s, want %s", got, want)
	}
	if !config.enabled() {
		t.Fatal("default reducer admission config is disabled, want enabled")
	}
}

func TestLoadReducerAdmissionConfigExplicitZeroDisablesTotalDepthGate(t *testing.T) {
	t.Parallel()

	// Explicit zero on the total-depth mark disables the total-depth gate, but
	// the writer stays enabled because the graph-write pressure gate defaults on.
	config, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "0",
	}))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
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

func TestLoadReducerAdmissionConfigBothGatesExplicitZeroDisablesWriter(t *testing.T) {
	t.Parallel()

	config, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK":          "0",
		"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "0",
	}))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	if config.enabled() {
		t.Fatal("both gates explicit zero is enabled, want disabled")
	}
}

func TestLoadReducerAdmissionConfigRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "-1",
	}))
	if err == nil {
		t.Fatal("loadReducerAdmissionConfig() error = nil, want invalid config error")
	}
}

func TestReducerIntentWriterWithAdmissionUsesDefaultHighWaterMark(t *testing.T) {
	t.Parallel()

	const wantDefaultHighWaterMark int64 = 10000

	writer, err := reducerIntentWriterWithAdmission(
		postgres.SQLDB{},
		&recordingReducerIntentWriter{},
		mapGetenv(nil),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("reducerIntentWriterWithAdmission() error = %v, want nil", err)
	}
	admission, ok := writer.(reducerAdmissionWriter)
	if !ok {
		t.Fatalf("writer type = %T, want reducerAdmissionWriter", writer)
	}
	if got := admission.config.HighWaterMark; got != wantDefaultHighWaterMark {
		t.Fatalf("HighWaterMark = %d, want default %d", got, wantDefaultHighWaterMark)
	}
}

func TestReducerIntentWriterWithAdmissionWrapsRealWriter(t *testing.T) {
	t.Parallel()

	writer, err := reducerIntentWriterWithAdmission(
		postgres.SQLDB{},
		&recordingReducerIntentWriter{},
		mapGetenv(map[string]string{
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("reducerIntentWriterWithAdmission() error = %v, want nil", err)
	}
	if _, ok := writer.(reducerAdmissionWriter); !ok {
		t.Fatalf("writer type = %T, want reducerAdmissionWriter", writer)
	}
}

func TestReducerIntentWriterWithAdmissionSkipsLocalLightweight(t *testing.T) {
	t.Parallel()

	writer, err := reducerIntentWriterWithAdmission(
		postgres.SQLDB{},
		lightweightReducerIntentWriter{},
		mapGetenv(map[string]string{
			"ESHU_QUERY_PROFILE":                     "local_lightweight",
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("reducerIntentWriterWithAdmission() error = %v, want nil", err)
	}
	if _, ok := writer.(lightweightReducerIntentWriter); !ok {
		t.Fatalf("writer type = %T, want lightweightReducerIntentWriter", writer)
	}
}

func BenchmarkReducerAdmissionDisabled(b *testing.B) {
	ctx := context.Background()
	writer := &countingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner:  writer,
		config: reducerAdmissionConfig{},
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
	writer := &countingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner: writer,
		depthReader: fixedReducerAdmissionDepthReader{
			depth: map[string]map[string]int64{"reducer": {"pending": 4}},
		},
		config: reducerAdmissionConfig{
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
	config, err := loadReducerAdmissionConfig(mapGetenv(nil))
	if err != nil {
		b.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	writer := &countingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner: writer,
		depthReader: fixedReducerAdmissionDepthReader{
			depth: map[string]map[string]int64{"reducer": {"pending": defaultReducerAdmissionHighWaterMark - 1}},
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
	writer := &countingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner: writer,
		depthReader: &alternatingReducerAdmissionDepthReader{
			high: map[string]map[string]int64{"reducer": {"pending": 10}},
			low:  map[string]map[string]int64{"reducer": {"pending": 4}},
		},
		config: reducerAdmissionConfig{
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

type fakeReducerAdmissionDepthReader struct {
	depths []map[string]map[string]int64
	calls  int
}

func (f *fakeReducerAdmissionDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	if f.calls >= len(f.depths) {
		return f.depths[len(f.depths)-1], nil
	}
	depth := f.depths[f.calls]
	f.calls++
	return depth, nil
}

// ReducerGraphWriteTimeoutDepth maps the next depth map's retrying bucket to the
// graph-write-timeout depth so the high-water-only tests (graph-write pressure
// disabled, this method unused) and any pressure test that drives the retrying
// bucket both observe consistent per-iteration depth. The gate calls exactly one
// of QueueDepths / ReducerGraphWriteTimeoutDepth per loop iteration, so the
// shared call cursor advances once per iteration either way.
func (f *fakeReducerAdmissionDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	if f.calls >= len(f.depths) {
		return f.depths[len(f.depths)-1]["reducer"]["retrying"], nil
	}
	depth := f.depths[f.calls]["reducer"]["retrying"]
	f.calls++
	return depth, nil
}

type recordingReducerIntentWriter struct {
	calls   int
	intents []projector.ReducerIntent
}

func (w *recordingReducerIntentWriter) Enqueue(
	_ context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	w.calls++
	w.intents = append(w.intents, intents...)
	return projector.IntentResult{Count: len(intents)}, nil
}

type countingReducerIntentWriter struct {
	count int
}

func (w *countingReducerIntentWriter) Enqueue(
	_ context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	w.count += len(intents)
	return projector.IntentResult{Count: len(intents)}, nil
}

type fixedReducerAdmissionDepthReader struct {
	depth map[string]map[string]int64
}

func (f fixedReducerAdmissionDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	return f.depth, nil
}

func (f fixedReducerAdmissionDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	return f.depth["reducer"]["retrying"], nil
}

type alternatingReducerAdmissionDepthReader struct {
	calls int
	high  map[string]map[string]int64
	low   map[string]map[string]int64
}

func (f *alternatingReducerAdmissionDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	f.calls++
	if f.calls%2 == 1 {
		return f.high, nil
	}
	return f.low, nil
}

func (f *alternatingReducerAdmissionDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	f.calls++
	if f.calls%2 == 1 {
		return f.high["reducer"]["retrying"], nil
	}
	return f.low["reducer"]["retrying"], nil
}
