// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type otelObserver struct {
	duration       metric.Float64Histogram
	memberAttempts metric.Int64Counter
	tracer         trace.Tracer
}

var (
	_ Observer        = (*otelObserver)(nil)
	_ ContextObserver = (*otelObserver)(nil)
)

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
	memberAttempts, err := meter.Int64Counter("eshu_dp_postgres_reader_member_attempts_total",
		metric.WithDescription("Physical reader fleet attempts by original inventory ordinal and outcome"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader member attempts: %w", err)
	}
	return &otelObserver{duration: duration, memberAttempts: memberAttempts, tracer: tracer}, nil
}

// ObserveMemberAttempt records a closed outcome under the member's original
// inventory ordinal. It never puts a host, member ID, or driver error in labels.
func (o *otelObserver) ObserveMemberAttempt(ordinal int, outcome Outcome) {
	if o == nil {
		return
	}
	member := readerUnknown
	if ordinal >= 0 {
		member = strconv.Itoa(ordinal)
	}
	o.memberAttempts.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String(readerAttributeMemberOrdinal, member),
		attribute.String(readerAttributeOutcome, closedReaderOutcome(outcome)),
	))
}

// Observe is the legacy, request-less entry point. It records the observation
// as ObserveContext would with context.Background(), so the stage span is a
// root. Access prefers ObserveContext, so this path serves only a caller that
// holds no request context.
func (o *otelObserver) Observe(role string, stage Stage, outcome Outcome, duration time.Duration) {
	o.ObserveContext(context.Background(), role, stage, outcome, duration)
}

// ObserveContext records only closed role, stage, and outcome values; invalid
// values collapse to unknown rather than becoming a metric or trace
// cardinality leak. The stage span starts from ctx, so it is a child of
// whatever recording span is active on ctx (the API server span or the query
// handler span; the writer checkpoint runs before a handler span exists, so on
// MCP, which has no server span, it stays a root) and a slow request names the
// stage that paid for it (#7545). The span keeps the retro-fitted start timestamp, so
// it covers the stage, not the callback.
func (o *otelObserver) ObserveContext(ctx context.Context, role string, stage Stage, outcome Outcome, duration time.Duration) {
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
	o.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	ended := time.Now()
	_, span := o.tracer.Start(ctx, readerAccessSpanName,
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
	if len(access.readerMembers) > 0 {
		if access.allocator == nil || len(access.allocator.capacity) != len(access.readerMembers) || access.readerInventoryCount < len(access.readerMembers) {
			return nil, errors.New("PostgreSQL reader member metrics require a valid fleet inventory and allocator")
		}
		ordinals := make([]bool, access.readerInventoryCount)
		for _, member := range access.readerMembers {
			if member.pool == nil || member.ordinal < 0 || member.ordinal >= len(ordinals) || ordinals[member.ordinal] {
				return nil, errors.New("PostgreSQL reader member metrics require unique valid pool ordinals")
			}
			ordinals[member.ordinal] = true
		}
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
	qualified, err := meter.Int64ObservableGauge("eshu_dp_postgres_reader_member_qualified",
		metric.WithDescription("Configured physical reader member qualified at bootstrap (0 or 1)"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader member qualification: %w", err)
	}
	memberConnections, err := meter.Int64ObservableGauge("eshu_dp_postgres_reader_member_connections",
		metric.WithDescription("Physical reader member pool connections by state"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader member connections: %w", err)
	}
	reservations, err := meter.Int64ObservableGauge("eshu_dp_postgres_reader_member_reservations",
		metric.WithDescription("Physical reader member connection slots reserved by the fleet allocator"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader member reservations: %w", err)
	}
	waiters, err := meter.Int64ObservableGauge("eshu_dp_postgres_reader_member_waiters",
		metric.WithDescription("Requests waiting on a physical reader member allocator queue"))
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader member waiters: %w", err)
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
		if access.allocator != nil {
			observed := make([]bool, access.readerInventoryCount)
			reserved, waiting := access.allocator.pressure()
			for index, member := range access.readerMembers {
				ordinal := attribute.String(readerAttributeMemberOrdinal, strconv.Itoa(member.ordinal))
				observed[member.ordinal] = true
				stats := member.pool.Stats()
				for _, state := range []struct {
					name  string
					value int
				}{
					{"max_open", stats.MaxOpenConnections},
					{"open", stats.OpenConnections},
					{"in_use", stats.InUse},
				} {
					observer.ObserveInt64(memberConnections, int64(state.value), metric.WithAttributes(ordinal, attribute.String(readerAttributeState, state.name)))
				}
				observer.ObserveInt64(reservations, int64(reserved[index]), metric.WithAttributes(ordinal))
				observer.ObserveInt64(waiters, int64(waiting[index]), metric.WithAttributes(ordinal))
			}
			for index, present := range observed {
				value := int64(0)
				if present {
					value = 1
				}
				observer.ObserveInt64(qualified, value, metric.WithAttributes(attribute.String(readerAttributeMemberOrdinal, strconv.Itoa(index))))
			}
		}
		return nil
	}, connections, waits, waitTime, qualified, memberConnections, reservations, waiters)
	if err != nil {
		return nil, fmt.Errorf("register PostgreSQL reader pool callback: %w", err)
	}
	return registration, nil
}
