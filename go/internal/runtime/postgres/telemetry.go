// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type otelObserver struct {
	duration metric.Float64Histogram
	tracer   trace.Tracer
}

var _ Observer = (*otelObserver)(nil)

// NewObserver emits a bounded stage duration histogram and a stage span for
// writer checkpoints, reader fences, and guarded business queries.
func NewObserver(meter metric.Meter, tracer trace.Tracer) (Observer, error) {
	if meter == nil || tracer == nil {
		return nil, errors.New("PostgreSQL reader observer requires a meter and tracer")
	}
	duration, err := meter.Float64Histogram("eshu_dp_postgres_reader_stage_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of a PostgreSQL reader access stage"),
		metric.WithExplicitBucketBoundaries(0, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader stage duration: %w", err)
	}
	return &otelObserver{duration: duration, tracer: tracer}, nil
}

// Observe records only closed role, stage, and outcome values; invalid values
// collapse to unknown rather than becoming a metric or trace cardinality leak.
func (o *otelObserver) Observe(role string, stage Stage, outcome Outcome, duration time.Duration) {
	if o == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	attrs := []attribute.KeyValue{
		attribute.String(readerAttributeRole, closedReaderRole(role)),
		attribute.String(readerAttributeStage, closedReaderStage(stage)),
		attribute.String(readerAttributeOutcome, closedReaderOutcome(outcome)),
	}
	o.duration.Record(context.Background(), duration.Seconds(), metric.WithAttributes(attrs...))
	ended := time.Now()
	_, span := o.tracer.Start(context.Background(), readerAccessSpanName,
		trace.WithTimestamp(ended.Add(-duration)), trace.WithAttributes(attrs...))
	if outcome != OutcomeOK {
		span.SetStatus(codes.Error, closedReaderOutcome(outcome))
	}
	span.End(trace.WithTimestamp(ended))
}

func (o *otelObserver) recordsReaderQueryStart(ctx context.Context) bool {
	return o != nil && trace.SpanFromContext(ctx).IsRecording()
}

func (o *otelObserver) recordReaderQueryStart(ctx context.Context, sequence int64, identity readerBackendIdentity) {
	attrs := []attribute.KeyValue{
		attribute.String(readerQueryRoleKey, "reader"),
		attribute.Int64(readerQuerySequenceKey, sequence),
	}
	if identity.available {
		attrs = append(attrs,
			attribute.String(readerQueryIdentityKey, "available"),
			attribute.Int64(readerQueryPIDKey, int64(identity.pid)),
			attribute.String(readerQueryRemoteKey, identity.remote),
		)
	} else {
		attrs = append(attrs, attribute.String(readerQueryIdentityKey, "unavailable"))
	}
	trace.SpanFromContext(ctx).AddEvent(readerQueryStartEventName, trace.WithAttributes(attrs...))
}

func closedReaderRole(role string) string {
	if role == "writer" || role == "reader" {
		return role
	}
	return readerUnknown
}

func closedReaderStage(stage Stage) string {
	switch stage {
	case StageWriterCheckpoint, StageReaderBorrow, StageReaderIdentity, StageReaderReplay, StageBusinessQuery:
		return string(stage)
	default:
		return readerUnknown
	}
}

func closedReaderOutcome(outcome Outcome) string {
	switch outcome {
	case OutcomeOK, OutcomeError, OutcomeDeadline, OutcomeCanceled:
		return string(outcome)
	default:
		return readerUnknown
	}
}

// RegisterPoolMetrics observes the aggregate writer and reader pool budgets,
// wait counts, and wait time on each scrape. Unregister before closing Access.
func RegisterPoolMetrics(meter metric.Meter, access *Access) (metric.Registration, error) {
	if meter == nil || access == nil || access.writer == nil || access.reader == nil {
		return nil, errors.New("PostgreSQL reader pool metrics require a meter and both pools")
	}
	connections, err := meter.Int64ObservableGauge("eshu_dp_postgres_reader_pool_connections",
		metric.WithDescription("PostgreSQL reader access pool connections by state"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL pool connections: %w", err)
	}
	waits, err := meter.Int64ObservableCounter("eshu_dp_postgres_reader_pool_waits_total",
		metric.WithDescription("Cumulative waits for a PostgreSQL reader access pool connection"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL pool waits: %w", err)
	}
	waitTime, err := meter.Float64ObservableCounter("eshu_dp_postgres_reader_pool_wait_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Cumulative PostgreSQL reader access pool wait duration"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL pool wait duration: %w", err)
	}
	registration, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		writer, reader := access.Stats()
		for _, pool := range []struct {
			role  string
			stats sql.DBStats
		}{
			{role: "writer", stats: writer},
			{role: "reader", stats: reader},
		} {
			roleAttr := attribute.String(readerAttributeRole, pool.role)
			for _, state := range []struct {
				name  string
				value int
			}{
				{"max_open", pool.stats.MaxOpenConnections},
				{"open", pool.stats.OpenConnections},
				{"in_use", pool.stats.InUse},
			} {
				observer.ObserveInt64(connections, int64(state.value), metric.WithAttributes(roleAttr, attribute.String(readerAttributeState, state.name)))
			}
			observer.ObserveInt64(waits, pool.stats.WaitCount, metric.WithAttributes(roleAttr))
			observer.ObserveFloat64(waitTime, pool.stats.WaitDuration.Seconds(), metric.WithAttributes(roleAttr))
		}
		return nil
	}, connections, waits, waitTime)
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader pool callback: %w", err)
	}
	return registration, nil
}
