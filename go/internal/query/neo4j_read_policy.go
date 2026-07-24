// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultGraphReadTimeout        = 10 * time.Second
	defaultGraphReadSlowThreshold  = 2 * time.Second
	defaultGraphReadRetryDelay     = 25 * time.Millisecond
	graphReadSessionCloseTimeout   = time.Second
	maxGraphReadAttempts           = 2
	neo4jTransactionTimedOutCode   = "Neo.ClientError.Transaction.TransactionTimedOut"
	neo4jTransactionTerminatedCode = "Neo.ClientError.Transaction.Terminated"
)

var (
	// ErrGraphReadDeadline reports that the bounded graph-read budget expired.
	ErrGraphReadDeadline = errors.New("graph query exceeded its deadline")
	// ErrGraphUnavailable reports that the graph backend could not serve a read.
	ErrGraphUnavailable = errors.New("graph temporarily unavailable; retry after graph health is restored")
)

type graphReadOutcome string

const (
	graphReadOutcomeSuccess        graphReadOutcome = "success"
	graphReadOutcomeSlow           graphReadOutcome = "slow"
	graphReadOutcomeRecovered      graphReadOutcome = "recovered"
	graphReadOutcomeDeadline       graphReadOutcome = "deadline"
	graphReadOutcomeCallerDeadline graphReadOutcome = "caller_deadline"
	graphReadOutcomeUnavailable    graphReadOutcome = "unavailable"
	graphReadOutcomeCanceled       graphReadOutcome = "canceled"
	graphReadOutcomeError          graphReadOutcome = "error"
)

// Neo4jReaderOption configures immutable reader dependencies before first use.
type Neo4jReaderOption func(*Neo4jReader)

// WithNeo4jReaderObservability wires the process logger and shared instruments.
func WithNeo4jReaderObservability(logger *slog.Logger, instruments *telemetry.Instruments) Neo4jReaderOption {
	return func(reader *Neo4jReader) {
		if logger != nil {
			reader.policy.logger = logger
		}
		reader.policy.instruments = instruments
	}
}

type neo4jReadPolicy struct {
	readTimeout   time.Duration
	slowThreshold time.Duration
	retryDelay    time.Duration
	logger        *slog.Logger
	instruments   *telemetry.Instruments
}

func defaultNeo4jReadPolicy() neo4jReadPolicy {
	return neo4jReadPolicy{
		readTimeout:   defaultGraphReadTimeout,
		slowThreshold: defaultGraphReadSlowThreshold,
		retryDelay:    defaultGraphReadRetryDelay,
		logger:        slog.Default(),
	}
}

type neo4jReadSession interface {
	Run(context.Context, string, map[string]any, ...func(*neo4jdriver.TransactionConfig)) (neo4jReadResult, error)
	Close(context.Context) error
}

type neo4jReadResult interface {
	Collect(context.Context) ([]*neo4jdriver.Record, error)
}

type neo4jReadSessionFactory func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession

type neo4jReadSessionAdapter struct {
	session neo4jdriver.SessionWithContext
}

func (a neo4jReadSessionAdapter) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
	configurers ...func(*neo4jdriver.TransactionConfig),
) (neo4jReadResult, error) {
	return a.session.Run(ctx, cypher, params, configurers...)
}

func (a neo4jReadSessionAdapter) Close(ctx context.Context) error {
	return a.session.Close(ctx)
}

type graphReadError struct {
	public error
	cause  error
}

func (e *graphReadError) Error() string { return e.public.Error() }
func (e *graphReadError) Unwrap() error { return e.cause }
func (e *graphReadError) Is(target error) bool {
	return target == e.public || errors.Is(e.cause, target)
}

func (r *Neo4jReader) runRead(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	parentCtx := ctx
	ctx, span := r.tracer.Start(
		ctx,
		"neo4j.query",
		trace.WithAttributes(
			attribute.String("db.system", "neo4j"),
			attribute.String("db.name", r.database),
			attribute.Int64(telemetry.SpanAttrGraphReadConfiguredDeadlineMS, r.policy.readTimeout.Milliseconds()),
		),
	)
	defer span.End()

	started := time.Now()
	readCtx, cancel := context.WithTimeout(ctx, r.policy.readTimeout)
	defer cancel()

	rows, attempts, err := r.runReadAttempts(readCtx, cypher, params)
	duration := time.Since(started)
	outcome, publicErr := graphReadResult(parentCtx, readCtx, err, attempts, duration, r.policy.slowThreshold)
	r.recordGraphReadTelemetry(parentCtx, span, outcome, attempts, duration, publicErr)
	if publicErr != nil {
		return nil, publicErr
	}
	return rows, nil
}

func (r *Neo4jReader) runReadAttempts(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, int, error) {
	if r.driver == nil && r.sessionFactory == nil {
		return nil, 0, errors.New("neo4j driver is required")
	}

	for attempt := 1; attempt <= maxGraphReadAttempts; attempt++ {
		rows, err := r.runReadAttempt(ctx, cypher, params)
		if err == nil {
			return rows, attempt, nil
		}
		if attempt == maxGraphReadAttempts || !isRetryableGraphAvailabilityError(err) {
			return nil, attempt, err
		}
		if err := waitForGraphReadRetry(ctx, r.policy.retryDelay); err != nil {
			return nil, attempt, err
		}
	}

	return nil, maxGraphReadAttempts, errors.New("graph read attempts exhausted")
}

func (r *Neo4jReader) runReadAttempt(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session := r.newReadSession(ctx)
	if session == nil {
		return nil, errors.New("neo4j read session is required")
	}
	defer r.closeNeo4jReadSession(ctx, session)

	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("neo4j read context is missing a deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, context.DeadlineExceeded
	}

	result, err := session.Run(ctx, cypher, params, neo4jdriver.WithTxTimeout(remaining))
	if err != nil {
		return nil, fmt.Errorf("neo4j query: %w", err)
	}
	if result == nil {
		return nil, errors.New("neo4j query returned no result cursor")
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("neo4j collect: %w", err)
	}

	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for index, key := range record.Keys {
			row[key] = record.Values[index]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *Neo4jReader) newReadSession(ctx context.Context) neo4jReadSession {
	config := neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.database,
	}
	if r.sessionFactory != nil {
		return r.sessionFactory(ctx, config)
	}
	return neo4jReadSessionAdapter{session: r.driver.NewSession(ctx, config)}
}

func (r *Neo4jReader) closeNeo4jReadSession(readCtx context.Context, session neo4jReadSession) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(readCtx), graphReadSessionCloseTimeout)
	defer cancel()
	if err := session.Close(cleanupCtx); err != nil {
		logger := r.policy.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(
			cleanupCtx,
			"graph read session cleanup failed",
			telemetry.EventAttr("query.graph_read.session_close_failed"),
			telemetry.PhaseAttr(telemetry.PhaseQuery),
			telemetry.FailureClassAttr("session_close_error"),
		)
	}
}

func waitForGraphReadRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableGraphAvailabilityError(err error) bool {
	var connectivityErr *neo4jdriver.ConnectivityError
	if errors.As(err, &connectivityErr) && connectivityErr.Inner != nil {
		return neo4jdriver.IsRetryable(connectivityErr)
	}
	var databaseErr *neo4jdriver.Neo4jError
	if !errors.As(err, &databaseErr) || !neo4jdriver.IsRetryable(databaseErr) {
		return false
	}
	if strings.HasPrefix(databaseErr.Code, "Neo.TransientError.") {
		return true
	}
	return databaseErr.Code == "Neo.ClientError.Cluster.NotALeader" ||
		databaseErr.Code == "Neo.ClientError.General.ForbiddenOnReadOnlyDatabase"
}

func isGraphUnavailableError(err error) bool {
	var connectivityErr *neo4jdriver.ConnectivityError
	if errors.As(err, &connectivityErr) {
		return true
	}
	return isRetryableGraphAvailabilityError(err)
}

func graphReadResult(
	parentCtx context.Context,
	readCtx context.Context,
	err error,
	attempts int,
	duration time.Duration,
	slowThreshold time.Duration,
) (graphReadOutcome, error) {
	if parentErr := parentCtx.Err(); parentErr != nil {
		if errors.Is(parentErr, context.DeadlineExceeded) {
			return graphReadOutcomeCallerDeadline, parentErr
		}
		return graphReadOutcomeCanceled, parentErr
	}
	if err == nil {
		if attempts > 1 {
			return graphReadOutcomeRecovered, nil
		}
		if slowThreshold > 0 && duration >= slowThreshold {
			return graphReadOutcomeSlow, nil
		}
		return graphReadOutcomeSuccess, nil
	}
	if isGraphReadDeadlineError(readCtx, err) {
		cause := err
		if errors.Is(readCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			cause = context.DeadlineExceeded
		}
		return graphReadOutcomeDeadline, &graphReadError{public: ErrGraphReadDeadline, cause: cause}
	}
	if errors.Is(readCtx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return graphReadOutcomeCanceled, fmt.Errorf("graph query canceled: %w", context.Canceled)
	}
	if isGraphUnavailableError(err) {
		return graphReadOutcomeUnavailable, &graphReadError{public: ErrGraphUnavailable, cause: err}
	}
	return graphReadOutcomeError, err
}

func isGraphReadDeadlineError(ctx context.Context, err error) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var databaseErr *neo4jdriver.Neo4jError
	if !errors.As(err, &databaseErr) {
		return false
	}
	// WithTxTimeout can surface either code when the server enforces the
	// transaction budget, depending on backend and driver timing.
	return databaseErr.Code == neo4jTransactionTimedOutCode || databaseErr.Code == neo4jTransactionTerminatedCode
}

func (r *Neo4jReader) recordGraphReadTelemetry(
	ctx context.Context,
	span trace.Span,
	outcome graphReadOutcome,
	attempts int,
	duration time.Duration,
	err error,
) {
	span.SetAttributes(
		attribute.String(telemetry.SpanAttrGraphReadOutcome, string(outcome)),
		attribute.Int(telemetry.SpanAttrGraphReadAttempts, attempts),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	recordCtx := context.WithoutCancel(ctx)
	if r.policy.instruments != nil && r.policy.instruments.Neo4jQueryDuration != nil {
		r.policy.instruments.Neo4jQueryDuration.Record(
			recordCtx,
			duration.Seconds(),
			metric.WithAttributes(
				telemetry.AttrOperation("read"),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}

	if outcome != graphReadOutcomeSlow && outcome != graphReadOutcomeDeadline && outcome != graphReadOutcomeUnavailable {
		return
	}
	logger := r.policy.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(
		ctx,
		"bounded graph read completed with warning",
		telemetry.EventAttr("query.graph_read.warning"),
		telemetry.PhaseAttr(telemetry.PhaseQuery),
		telemetry.FailureClassAttr(string(outcome)),
		slog.Float64("duration_seconds", duration.Seconds()),
	)
}
