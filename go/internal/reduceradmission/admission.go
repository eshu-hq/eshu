// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reduceradmission gates reducer intent enqueue behind reducer queue
// depth so a producer slows admission instead of piling recoverable work into
// an already-overloaded reducer queue or a timing-out graph backend.
//
// See the package README and AGENTS.md for the two-gate model (total-depth
// high-water mark and graph-write-timeout-scoped pressure with hysteresis) and
// the parity contract between the ingester and bootstrap-index producers.
package reduceradmission

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Env names for the admission gate's operator knobs. Both the ingester and
// bootstrap-index producers read these so the two runtimes share one
// configuration surface (parity requirement, issue #4515).
const (
	HighWaterMarkEnv         = "ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK"
	PollIntervalEnv          = "ESHU_REDUCER_ADMISSION_POLL_INTERVAL"
	RetryingHighWaterMarkEnv = "ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK"
	RetryingLowWaterMarkEnv  = "ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK"

	defaultHighWaterMark         int64 = 10_000
	defaultRetryingHighWaterMark int64 = 500
	defaultRetryingLowWaterMark  int64 = 100
	defaultPollInterval                = time.Second
)

// Deferral reason labels recorded on the ReducerAdmissionDeferrals telemetry
// counter and structured log so an operator can distinguish a deep-but-healthy
// queue from a timing-out graph backend at 3 AM.
const (
	// DeferralReasonHighWater labels a deferral caused by total outstanding
	// reducer queue depth crossing the high-water mark.
	DeferralReasonHighWater = "high_water"
	// DeferralReasonGraphWritePressure labels a deferral caused by
	// graph-write-timeout retrying reducer work crossing the graph-write-pressure
	// mark. It is the durable signal that the graph backend is timing out and
	// that recoverable work would otherwise drift toward retry-exhaustion dead
	// letters if the producer kept pushing.
	DeferralReasonGraphWritePressure = "graph_write_pressure"
)

// GraphWriteTimeoutFailureClass is the durable failure_class of a reducer row
// that is retrying because a bounded graph write timed out. The pressure gate
// counts only rows in this class, so readiness-not-ready retrying backlogs
// (secrets_iam_endpoint_not_ready and other *_n classes) never false-throttle
// admission (#3560). It mirrors cypher.GraphWriteTimeoutFailureClass; the
// depth query in the postgres queue observer is the single enforcement point
// and owns the canonical reference.
const GraphWriteTimeoutFailureClass = "graph_write_timeout"

// Config holds the admission gate's operator-tunable thresholds.
type Config struct {
	HighWaterMark int64
	PollInterval  time.Duration
	// RetryingHighWaterMark defers the producer once retrying-state reducer
	// depth reaches this value. Zero disables the graph-write-pressure gate.
	RetryingHighWaterMark int64
	// RetryingLowWaterMark releases the producer only after retrying-state
	// depth falls below this value. The gap between high and low marks is
	// hysteresis that stops the producer from flapping on every partial
	// recovery.
	RetryingLowWaterMark int64
}

func (c Config) enabled() bool {
	return c.HighWaterMark > 0 || c.graphWritePressureEnabled()
}

// graphWritePressureEnabled reports whether the retrying-depth (graph-write
// timeout) backpressure gate is active.
func (c Config) graphWritePressureEnabled() bool {
	return c.RetryingHighWaterMark > 0
}

// deferralState carries the hysteresis flag shared across concurrent producer
// Enqueue calls. Both the ingester and bootstrap-index run projection workers
// concurrently and share one admission writer value, so the deferring flag
// must be pointer-shared and mutex-guarded.
type deferralState struct {
	mu        sync.Mutex
	deferring bool
}

func newDeferralState() *deferralState {
	return &deferralState{}
}

// graphWritePressure decides, with hysteresis, whether retrying-state depth
// should defer the producer. Once deferring, it stays deferring until depth
// falls below the low-water mark; once released, it re-engages only at or
// above the high-water mark.
func (s *deferralState) graphWritePressure(retrying, highWater, lowWater int64) bool {
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

// deferralReasonSink records why a producer deferral happened, including the
// failure class that drove a graph-write-pressure deferral (empty for
// total-depth high-water deferrals). It lets tests assert the operator-facing
// reason and failure class without a real meter and keeps the metric/log
// emission in one place.
type deferralReasonSink func(ctx context.Context, reason, failureClass string, depth int64, intentCount int)

// depthReader reads the queue-depth signals the admission gate consumes.
// QueueDepths feeds the total-depth high-water gate; the failure-class scoped
// ReducerGraphWriteTimeoutDepth feeds the graph-write-pressure gate so a
// readiness backlog of retrying rows never false-throttles admission (#3560).
type depthReader interface {
	QueueDepths(context.Context) (map[string]map[string]int64, error)
	ReducerGraphWriteTimeoutDepth(context.Context) (int64, error)
}

// writer wraps an inner projector.ReducerIntentWriter with the two-gate
// admission policy.
type writer struct {
	inner       projector.ReducerIntentWriter
	depthReader depthReader
	config      Config
	instruments *telemetry.Instruments
	logger      *slog.Logger
	sleep       func(context.Context, time.Duration) error
	// deferral holds the shared graph-write-pressure hysteresis flag. It is a
	// pointer so copies of the writer value share one state.
	deferral *deferralState
	// failureClassSink is an optional override for deferral telemetry; when nil
	// the writer records the deferral counter and structured log directly. It
	// receives the failure class that drove a graph-write-pressure deferral.
	failureClassSink deferralReasonSink
}

// WrapIntentWriter wraps inner with the reducer admission gate when the gate
// is enabled by getenv, returning inner unchanged when the gate is disabled.
// database is the narrow read-only queue-depth surface (postgres.Queryer);
// callers that also need to construct the inner writer's own postgres access
// do so separately and pass only the depth-reading dependency here.
//
// Both the ingester and bootstrap-index producers call this so admission
// backpressure behavior is identical across both binaries (issue #4515
// parity).
func WrapIntentWriter(
	database postgres.Queryer,
	inner projector.ReducerIntentWriter,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.ReducerIntentWriter, error) {
	config, err := LoadConfig(getenv)
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

	return writer{
		inner:       inner,
		depthReader: postgres.NewQueueObserverStore(database),
		config:      config,
		instruments: instruments,
		logger:      logger,
		sleep:       sleepContext,
		deferral:    newDeferralState(),
	}, nil
}

// LoadConfig parses the admission gate's operator knobs from the environment,
// applying defaults and validating the hysteresis invariant.
func LoadConfig(getenv func(string) string) (Config, error) {
	config := Config{
		HighWaterMark:         defaultHighWaterMark,
		PollInterval:          defaultPollInterval,
		RetryingHighWaterMark: defaultRetryingHighWaterMark,
		RetryingLowWaterMark:  defaultRetryingLowWaterMark,
	}
	rawHighWater := strings.TrimSpace(getenv(HighWaterMarkEnv))
	if rawHighWater != "" {
		highWaterMark, err := strconv.ParseInt(rawHighWater, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", HighWaterMarkEnv, err)
		}
		if highWaterMark < 0 {
			return Config{}, fmt.Errorf("%s must be zero or greater", HighWaterMarkEnv)
		}
		config.HighWaterMark = highWaterMark
	}

	if err := loadRetryingMarks(getenv, &config); err != nil {
		return Config{}, err
	}

	rawPollInterval := strings.TrimSpace(getenv(PollIntervalEnv))
	if rawPollInterval == "" {
		return config, nil
	}
	pollInterval, err := time.ParseDuration(rawPollInterval)
	if err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", PollIntervalEnv, err)
	}
	if pollInterval <= 0 {
		return Config{}, fmt.Errorf("%s must be greater than zero", PollIntervalEnv)
	}
	config.PollInterval = pollInterval

	return config, nil
}

// loadRetryingMarks parses the graph-write-pressure high/low marks and
// validates the hysteresis invariant (low < high when the gate is enabled). A
// low mark of zero is clamped to the default fraction of the high mark so an
// operator who only sets the high mark still gets sane hysteresis.
func loadRetryingMarks(getenv func(string) string, config *Config) error {
	rawHigh := strings.TrimSpace(getenv(RetryingHighWaterMarkEnv))
	if rawHigh != "" {
		high, err := strconv.ParseInt(rawHigh, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", RetryingHighWaterMarkEnv, err)
		}
		if high < 0 {
			return fmt.Errorf("%s must be zero or greater", RetryingHighWaterMarkEnv)
		}
		config.RetryingHighWaterMark = high
	}

	rawLow := strings.TrimSpace(getenv(RetryingLowWaterMarkEnv))
	if rawLow != "" {
		low, err := strconv.ParseInt(rawLow, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", RetryingLowWaterMarkEnv, err)
		}
		if low < 0 {
			return fmt.Errorf("%s must be zero or greater", RetryingLowWaterMarkEnv)
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
				RetryingLowWaterMarkEnv, config.RetryingLowWaterMark,
				RetryingHighWaterMarkEnv, config.RetryingHighWaterMark)
		}
		config.RetryingLowWaterMark = clampRetryingLowWaterMark(config.RetryingHighWaterMark)
	}
	return nil
}

// clampRetryingLowWaterMark derives a default low-water mark from the
// high-water mark: one fifth of the high mark, with a floor of 1 so the gate
// can always release.
func clampRetryingLowWaterMark(high int64) int64 {
	low := high / 5
	if low < 1 {
		low = 1
	}
	return low
}

func (w writer) Enqueue(
	ctx context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	if len(intents) == 0 {
		return projector.IntentResult{Count: 0}, nil
	}
	if !w.config.enabled() {
		//nolint:wrapcheck // Decorator passthrough: return the inner writer's error verbatim to keep byte-identical behavior with the pre-extraction ingester path.
		return w.inner.Enqueue(ctx, intents)
	}
	if w.inner == nil {
		return projector.IntentResult{}, errors.New("reducer admission inner writer is required")
	}
	if w.depthReader == nil {
		return projector.IntentResult{}, errors.New("reducer admission depth reader is required")
	}

	for {
		reason, failureClass, depth, err := w.admissionDecision(ctx)
		if err != nil {
			return projector.IntentResult{}, err
		}
		if reason == "" {
			//nolint:wrapcheck // Decorator passthrough: return the inner writer's error verbatim to keep byte-identical behavior with the pre-extraction ingester path.
			return w.inner.Enqueue(ctx, intents)
		}
		w.recordDeferral(ctx, reason, failureClass, depth, len(intents))
		if err := w.wait(ctx); err != nil {
			return projector.IntentResult{}, err
		}
	}
}

// admissionDecision returns the deferral reason, the failure class that drove
// a graph-write-pressure deferral (empty otherwise), and the depth that
// triggered it. An empty reason means admission may proceed. The
// graph-write-pressure gate is checked first because it is the leading
// indicator that the backend is timing out; the total-depth gate is the
// trailing safeguard.
//
// The pressure gate counts only retrying reducer rows whose failure_class is
// graph_write_timeout, read from a failure-class-scoped query rather than the
// stage/status-only QueueDepths bucket. This is the #3560 fix: a backlog of
// readiness-not-ready retrying rows (secrets_iam_endpoint_not_ready and other
// *_n classes) reports zero graph-write-timeout depth and therefore never
// false-throttles unrelated reducer admission.
func (w writer) admissionDecision(ctx context.Context) (string, string, int64, error) {
	if w.config.graphWritePressureEnabled() && w.deferral != nil {
		graphWriteTimeoutDepth, err := w.depthReader.ReducerGraphWriteTimeoutDepth(ctx)
		if err != nil {
			return "", "", 0, fmt.Errorf("read reducer graph-write-timeout depth: %w", err)
		}
		if w.deferral.graphWritePressure(graphWriteTimeoutDepth, w.config.RetryingHighWaterMark, w.config.RetryingLowWaterMark) {
			return DeferralReasonGraphWritePressure, GraphWriteTimeoutFailureClass, graphWriteTimeoutDepth, nil
		}
	}

	if w.config.HighWaterMark > 0 {
		depths, err := w.depthReader.QueueDepths(ctx)
		if err != nil {
			return "", "", 0, fmt.Errorf("read reducer admission queue depth: %w", err)
		}
		var total int64
		for _, count := range depths["reducer"] {
			total += count
		}
		if total >= w.config.HighWaterMark {
			return DeferralReasonHighWater, "", total, nil
		}
	}

	return "", "", 0, nil
}

func (w writer) recordDeferral(ctx context.Context, reason, failureClass string, depth int64, intentCount int) {
	if w.failureClassSink != nil {
		w.failureClassSink(ctx, reason, failureClass, depth, intentCount)
		return
	}
	if w.instruments != nil {
		attrs := []attribute.KeyValue{telemetry.AttrReason(reason)}
		if failureClass != "" {
			attrs = append(attrs, telemetry.AttrFailureClass(failureClass))
		}
		w.instruments.ReducerAdmissionDeferrals.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if w.logger != nil {
		w.logger.WarnContext(
			ctx, "reducer admission deferring enqueue",
			slog.String("reason", reason),
			log.FailureClass(failureClass),
			slog.Int64("queue_depth", depth),
			slog.Int64("high_water_mark", w.config.HighWaterMark),
			slog.Int64("retrying_high_water_mark", w.config.RetryingHighWaterMark),
			slog.Int64("retrying_low_water_mark", w.config.RetryingLowWaterMark),
			slog.Duration("poll_interval", w.config.PollInterval),
			slog.Int("intent_count", intentCount),
		)
	}
}

func (w writer) wait(ctx context.Context) error {
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
		//nolint:wrapcheck // Return the context cancellation error verbatim so callers can errors.Is(context.Canceled/DeadlineExceeded).
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
