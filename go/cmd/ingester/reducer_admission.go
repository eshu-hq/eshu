package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	reducerAdmissionHighWaterMarkEnv                   = "ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK"
	reducerAdmissionPollIntervalEnv                    = "ESHU_REDUCER_ADMISSION_POLL_INTERVAL"
	reducerAdmissionRetryingHighWaterMarkEnv           = "ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK"
	reducerAdmissionRetryingLowWaterMarkEnv            = "ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK"
	defaultReducerAdmissionHighWaterMark         int64 = 10_000
	defaultReducerAdmissionRetryingHighWaterMark int64 = 500
	defaultReducerAdmissionRetryingLowWaterMark  int64 = 100
	defaultReducerAdmissionPoll                        = time.Second
)

const (
	// admissionDeferralReasonHighWater labels a deferral caused by total
	// outstanding reducer queue depth crossing the high-water mark.
	admissionDeferralReasonHighWater = "high_water"
	// admissionDeferralReasonGraphWritePressure labels a deferral caused by
	// retrying-state reducer work crossing the graph-write-pressure mark. It is
	// the durable signal that the graph backend is timing out and that
	// recoverable work would otherwise drift toward retry-exhaustion dead
	// letters if the producer kept pushing.
	admissionDeferralReasonGraphWritePressure = "graph_write_pressure"
)

type reducerAdmissionConfig struct {
	HighWaterMark int64
	PollInterval  time.Duration
	// RetryingHighWaterMark defers the producer once retrying-state reducer
	// depth reaches this value. Zero disables the graph-write-pressure gate.
	RetryingHighWaterMark int64
	// RetryingLowWaterMark releases the producer only after retrying-state depth
	// falls below this value. The gap between high and low marks is hysteresis
	// that stops the producer from flapping on every partial recovery.
	RetryingLowWaterMark int64
}

func (c reducerAdmissionConfig) enabled() bool {
	return c.HighWaterMark > 0 || c.graphWritePressureEnabled()
}

// graphWritePressureEnabled reports whether the retrying-depth (graph-write
// timeout) backpressure gate is active.
func (c reducerAdmissionConfig) graphWritePressureEnabled() bool {
	return c.RetryingHighWaterMark > 0
}

// admissionDeferralState carries the hysteresis flag shared across concurrent
// producer Enqueue calls. The ingester runs projection workers concurrently and
// they share one admission writer value, so the deferring flag must be
// pointer-shared and mutex-guarded.
type admissionDeferralState struct {
	mu        sync.Mutex
	deferring bool
}

func newAdmissionDeferralState() *admissionDeferralState {
	return &admissionDeferralState{}
}

// graphWritePressure decides, with hysteresis, whether retrying-state depth
// should defer the producer. Once deferring, it stays deferring until depth
// falls below the low-water mark; once released, it re-engages only at or above
// the high-water mark.
func (s *admissionDeferralState) graphWritePressure(retrying, highWater, lowWater int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deferring {
		if retrying < lowWater {
			s.deferring = false
		}
		return s.deferring
	}
	if retrying >= highWater {
		s.deferring = true
	}
	return s.deferring
}

// admissionDeferralReasonSink records why a producer deferral happened. It lets
// tests assert the operator-facing reason without a real meter and keeps the
// metric/log emission in one place.
type admissionDeferralReasonSink func(ctx context.Context, reason string, depth int64, intentCount int)

type reducerAdmissionDepthReader interface {
	QueueDepths(context.Context) (map[string]map[string]int64, error)
}

func ingesterReducerIntentWriter(
	database postgres.ExecQueryer,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.ReducerIntentWriter, error) {
	writer := reducerIntentWriterForProfile(getenv, postgres.NewReducerQueue(database, "ingester", time.Minute))
	return reducerIntentWriterWithAdmission(database, writer, getenv, instruments, logger)
}

type reducerAdmissionWriter struct {
	inner       projector.ReducerIntentWriter
	depthReader reducerAdmissionDepthReader
	config      reducerAdmissionConfig
	instruments *telemetry.Instruments
	logger      *slog.Logger
	sleep       func(context.Context, time.Duration) error
	// deferral holds the shared graph-write-pressure hysteresis flag. It is a
	// pointer so copies of the writer value share one state.
	deferral *admissionDeferralState
	// reasonSink is an optional override for deferral telemetry; when nil the
	// writer records the deferral counter and structured log directly.
	reasonSink admissionDeferralReasonSink
}

func reducerIntentWriterWithAdmission(
	database postgres.Queryer,
	inner projector.ReducerIntentWriter,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.ReducerIntentWriter, error) {
	if ingesterLocalLightweight(getenv) {
		return inner, nil
	}

	config, err := loadReducerAdmissionConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !config.enabled() {
		return inner, nil
	}
	if inner == nil {
		return nil, errors.New("reducer admission inner writer is required")
	}
	if database == nil {
		return nil, errors.New("reducer admission queue depth reader database is required")
	}

	return reducerAdmissionWriter{
		inner:       inner,
		depthReader: postgres.NewQueueObserverStore(database),
		config:      config,
		instruments: instruments,
		logger:      logger,
		sleep:       sleepContext,
		deferral:    newAdmissionDeferralState(),
	}, nil
}

func loadReducerAdmissionConfig(getenv func(string) string) (reducerAdmissionConfig, error) {
	config := reducerAdmissionConfig{
		HighWaterMark:         defaultReducerAdmissionHighWaterMark,
		PollInterval:          defaultReducerAdmissionPoll,
		RetryingHighWaterMark: defaultReducerAdmissionRetryingHighWaterMark,
		RetryingLowWaterMark:  defaultReducerAdmissionRetryingLowWaterMark,
	}
	rawHighWater := strings.TrimSpace(getenv(reducerAdmissionHighWaterMarkEnv))
	if rawHighWater != "" {
		highWaterMark, err := strconv.ParseInt(rawHighWater, 10, 64)
		if err != nil {
			return reducerAdmissionConfig{}, fmt.Errorf("parse %s: %w", reducerAdmissionHighWaterMarkEnv, err)
		}
		if highWaterMark < 0 {
			return reducerAdmissionConfig{}, fmt.Errorf("%s must be zero or greater", reducerAdmissionHighWaterMarkEnv)
		}
		config.HighWaterMark = highWaterMark
	}

	if err := loadReducerAdmissionRetryingMarks(getenv, &config); err != nil {
		return reducerAdmissionConfig{}, err
	}

	rawPollInterval := strings.TrimSpace(getenv(reducerAdmissionPollIntervalEnv))
	if rawPollInterval == "" {
		return config, nil
	}
	pollInterval, err := time.ParseDuration(rawPollInterval)
	if err != nil {
		return reducerAdmissionConfig{}, fmt.Errorf("parse %s: %w", reducerAdmissionPollIntervalEnv, err)
	}
	if pollInterval <= 0 {
		return reducerAdmissionConfig{}, fmt.Errorf("%s must be greater than zero", reducerAdmissionPollIntervalEnv)
	}
	config.PollInterval = pollInterval

	return config, nil
}

// loadReducerAdmissionRetryingMarks parses the graph-write-pressure high/low
// marks and validates the hysteresis invariant (low < high when the gate is
// enabled). A low mark of zero is clamped to the default fraction of the high
// mark so an operator who only sets the high mark still gets sane hysteresis.
func loadReducerAdmissionRetryingMarks(getenv func(string) string, config *reducerAdmissionConfig) error {
	rawHigh := strings.TrimSpace(getenv(reducerAdmissionRetryingHighWaterMarkEnv))
	if rawHigh != "" {
		high, err := strconv.ParseInt(rawHigh, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", reducerAdmissionRetryingHighWaterMarkEnv, err)
		}
		if high < 0 {
			return fmt.Errorf("%s must be zero or greater", reducerAdmissionRetryingHighWaterMarkEnv)
		}
		config.RetryingHighWaterMark = high
	}

	rawLow := strings.TrimSpace(getenv(reducerAdmissionRetryingLowWaterMarkEnv))
	if rawLow != "" {
		low, err := strconv.ParseInt(rawLow, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", reducerAdmissionRetryingLowWaterMarkEnv, err)
		}
		if low < 0 {
			return fmt.Errorf("%s must be zero or greater", reducerAdmissionRetryingLowWaterMarkEnv)
		}
		config.RetryingLowWaterMark = low
	}

	if !config.graphWritePressureEnabled() {
		return nil
	}
	if config.RetryingLowWaterMark <= 0 || config.RetryingLowWaterMark > config.RetryingHighWaterMark {
		// Default or out-of-range low mark: clamp below the high mark so the
		// hysteresis gap is always valid. An explicit low mark above the high
		// mark is an operator error.
		if rawLow != "" && config.RetryingLowWaterMark > config.RetryingHighWaterMark {
			return fmt.Errorf("%s (%d) must be less than %s (%d)",
				reducerAdmissionRetryingLowWaterMarkEnv, config.RetryingLowWaterMark,
				reducerAdmissionRetryingHighWaterMarkEnv, config.RetryingHighWaterMark)
		}
		config.RetryingLowWaterMark = clampRetryingLowWaterMark(config.RetryingHighWaterMark)
	}
	return nil
}

// clampRetryingLowWaterMark derives a default low-water mark from the high-water
// mark: one fifth of the high mark, with a floor of 1 so the gate can always
// release.
func clampRetryingLowWaterMark(high int64) int64 {
	low := high / 5
	if low < 1 {
		low = 1
	}
	return low
}

func (w reducerAdmissionWriter) Enqueue(
	ctx context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	if len(intents) == 0 {
		return projector.IntentResult{Count: 0}, nil
	}
	if !w.config.enabled() {
		return w.inner.Enqueue(ctx, intents)
	}
	if w.inner == nil {
		return projector.IntentResult{}, errors.New("reducer admission inner writer is required")
	}
	if w.depthReader == nil {
		return projector.IntentResult{}, errors.New("reducer admission depth reader is required")
	}

	for {
		reason, depth, err := w.admissionDecision(ctx)
		if err != nil {
			return projector.IntentResult{}, err
		}
		if reason == "" {
			return w.inner.Enqueue(ctx, intents)
		}
		w.recordDeferral(ctx, reason, depth, len(intents))
		if err := w.wait(ctx); err != nil {
			return projector.IntentResult{}, err
		}
	}
}

// admissionDecision reads queue depths and returns the deferral reason and the
// depth that triggered it. An empty reason means admission may proceed. The
// graph-write-pressure gate (retrying depth) is checked first because it is the
// leading indicator that the backend is timing out; the total-depth gate is the
// trailing safeguard.
func (w reducerAdmissionWriter) admissionDecision(ctx context.Context) (string, int64, error) {
	depths, err := w.depthReader.QueueDepths(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("read reducer admission queue depth: %w", err)
	}
	reducerDepths := depths["reducer"]

	if w.config.graphWritePressureEnabled() && w.deferral != nil {
		retrying := reducerDepths["retrying"]
		if w.deferral.graphWritePressure(retrying, w.config.RetryingHighWaterMark, w.config.RetryingLowWaterMark) {
			return admissionDeferralReasonGraphWritePressure, retrying, nil
		}
	}

	if w.config.HighWaterMark > 0 {
		var total int64
		for _, count := range reducerDepths {
			total += count
		}
		if total >= w.config.HighWaterMark {
			return admissionDeferralReasonHighWater, total, nil
		}
	}

	return "", 0, nil
}

func (w reducerAdmissionWriter) recordDeferral(ctx context.Context, reason string, depth int64, intentCount int) {
	if w.reasonSink != nil {
		w.reasonSink(ctx, reason, depth, intentCount)
		return
	}
	if w.instruments != nil {
		w.instruments.ReducerAdmissionDeferrals.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrReason(reason),
		))
	}
	if w.logger != nil {
		w.logger.WarnContext(ctx, "reducer admission deferring enqueue",
			slog.String("reason", reason),
			slog.Int64("queue_depth", depth),
			slog.Int64("high_water_mark", w.config.HighWaterMark),
			slog.Int64("retrying_high_water_mark", w.config.RetryingHighWaterMark),
			slog.Int64("retrying_low_water_mark", w.config.RetryingLowWaterMark),
			slog.Duration("poll_interval", w.config.PollInterval),
			slog.Int("intent_count", intentCount),
		)
	}
}

func (w reducerAdmissionWriter) wait(ctx context.Context) error {
	sleep := w.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	return sleep(ctx, w.config.PollInterval)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
