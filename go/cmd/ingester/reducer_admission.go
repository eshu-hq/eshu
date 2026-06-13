package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	reducerAdmissionHighWaterMarkEnv = "ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK"
	reducerAdmissionPollIntervalEnv  = "ESHU_REDUCER_ADMISSION_POLL_INTERVAL"
	defaultReducerAdmissionPoll      = time.Second
)

type reducerAdmissionConfig struct {
	HighWaterMark int64
	PollInterval  time.Duration
}

func (c reducerAdmissionConfig) enabled() bool {
	return c.HighWaterMark > 0
}

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
	}, nil
}

func loadReducerAdmissionConfig(getenv func(string) string) (reducerAdmissionConfig, error) {
	config := reducerAdmissionConfig{PollInterval: defaultReducerAdmissionPoll}
	rawHighWater := strings.TrimSpace(getenv(reducerAdmissionHighWaterMarkEnv))
	if rawHighWater == "" {
		return config, nil
	}

	highWaterMark, err := strconv.ParseInt(rawHighWater, 10, 64)
	if err != nil {
		return reducerAdmissionConfig{}, fmt.Errorf("parse %s: %w", reducerAdmissionHighWaterMarkEnv, err)
	}
	if highWaterMark < 0 {
		return reducerAdmissionConfig{}, fmt.Errorf("%s must be zero or greater", reducerAdmissionHighWaterMarkEnv)
	}
	config.HighWaterMark = highWaterMark

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
		depth, err := w.reducerDepth(ctx)
		if err != nil {
			return projector.IntentResult{}, err
		}
		if depth < w.config.HighWaterMark {
			return w.inner.Enqueue(ctx, intents)
		}
		w.recordDeferral(ctx, depth, len(intents))
		if err := w.wait(ctx); err != nil {
			return projector.IntentResult{}, err
		}
	}
}

func (w reducerAdmissionWriter) reducerDepth(ctx context.Context) (int64, error) {
	depths, err := w.depthReader.QueueDepths(ctx)
	if err != nil {
		return 0, fmt.Errorf("read reducer admission queue depth: %w", err)
	}
	var total int64
	for _, count := range depths["reducer"] {
		total += count
	}
	return total, nil
}

func (w reducerAdmissionWriter) recordDeferral(ctx context.Context, depth int64, intentCount int) {
	if w.instruments != nil {
		w.instruments.ReducerAdmissionDeferrals.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrReason("high_water"),
		))
	}
	if w.logger != nil {
		w.logger.DebugContext(ctx, "reducer admission deferring enqueue",
			slog.Int64("queue_depth", depth),
			slog.Int64("high_water_mark", w.config.HighWaterMark),
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
