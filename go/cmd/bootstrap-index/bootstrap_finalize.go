// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// secretLinesFinalizationTimeout bounds the hardcoded-secret side table rebuild.
// The rebuild is about 0.3 ms of Postgres CPU per file split across the
// finalizer's workers; the timeout is a stuck-run bound, not a budget.
const secretLinesFinalizationTimeout = 30 * time.Minute

// secretLinesLifecycle is the bulk-load lifecycle of the hardcoded-secret side
// table (#7125): begin turns readiness off before the first deferred write, and
// finalize rebuilds the table and publishes ready after the last one.
//
// lock takes the run-scoped bulk-load lock (secretlines.AcquireBulkLoadLock) and
// returns its release. It is held from before the schema apply until after
// finalize, so two bootstrap-index runs never overlap between begin and
// finalize: the epoch fence alone cannot stop the first finalizer from
// publishing ready over rows the second run is still writing without
// derivation. Its wait is the schema ownership wait, and the schema apply then
// waits again with the same bound, so the worst case is twice that value.
type secretLinesLifecycle struct {
	lock     func(context.Context, bootstrapDB, *slog.Logger, time.Duration) (func(context.Context) error, error)
	begin    func(context.Context, bootstrapDB) (int64, error)
	finalize func(context.Context, bootstrapDB, int64, *slog.Logger, *telemetry.Instruments) error
}

// productionSecretLines wires secretlines.BeginDeferral and secretlines.Finalize.
func productionSecretLines() secretLinesLifecycle {
	return secretLinesLifecycle{
		lock: func(ctx context.Context, database bootstrapDB, logger *slog.Logger, wait time.Duration) (func(context.Context) error, error) {
			conner, ok := database.(secretlines.Conner)
			if !ok {
				return nil, errors.New("bootstrap database cannot pin a connection for the secret lines bulk load lock")
			}
			held, err := secretlines.AcquireBulkLoadLock(ctx, conner, secretlines.LockOptions{Wait: wait, Logger: logger})
			if err != nil {
				return nil, err
			}
			return held.Release, nil
		},
		begin: func(ctx context.Context, database bootstrapDB) (int64, error) {
			return secretlines.BeginDeferral(ctx, database)
		},
		finalize: func(ctx context.Context, database bootstrapDB, epoch int64, logger *slog.Logger, instruments *telemetry.Instruments) error {
			beginner, ok := database.(db.Beginner)
			if !ok {
				return errors.New("bootstrap database does not support transactions")
			}
			_, err := secretlines.Finalize(ctx, secretLinesDatabase{ExecQueryer: database, Beginner: beginner}, epoch, secretlines.Options{
				Logger:      logger,
				Instruments: instruments,
			})
			return err
		},
	}
}

// secretLinesDatabase adapts the bootstrap database to secretlines.Database.
type secretLinesDatabase struct {
	db.ExecQueryer
	db.Beginner
}

// finalizeBootstrapContent runs the two post-drain content finalizers
// concurrently: the exact substring index build and the hardcoded-secret side
// table rebuild. They are independent (the rebuild takes row locks and writes
// only its own table; CREATE INDEX takes a table SHARE lock that row locks do
// not conflict with), and running them side by side keeps the rebuild off the
// bootstrap critical path. Both always run to completion; their errors join.
func finalizeBootstrapContent(
	ctx context.Context,
	database bootstrapDB,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	finalizeContentSearchIndexes finalizeContentSearchIndexesFn,
	secretLines secretLinesLifecycle,
	secretEpoch int64,
) error {
	var (
		wg        sync.WaitGroup
		indexErr  error
		secretErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		indexErr = finalizeContentSearchIndexesPhase(ctx, database, logger, instruments, finalizeContentSearchIndexes)
	}()
	go func() {
		defer wg.Done()
		secretErr = finalizeSecretLinesPhase(ctx, database, logger, instruments, secretLines, secretEpoch)
	}()
	wg.Wait()
	return errors.Join(indexErr, secretErr)
}

func finalizeContentSearchIndexesPhase(
	ctx context.Context,
	database bootstrapDB,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	finalize finalizeContentSearchIndexesFn,
) error {
	start := time.Now()
	logger.InfoContext(ctx, "content substring index finalization started", "index_state", "building")
	finalizeCtx, cancel := context.WithTimeout(ctx, contentSearchIndexFinalizationTimeout)
	defer cancel()
	if err := finalize(finalizeCtx, database); err != nil {
		logger.ErrorContext(
			ctx,
			"content substring index finalization failed",
			"index_state", "failed",
			"duration_seconds", recordContentSearchIndexFinalizationDuration(ctx, instruments, start),
			telemetry.FailureClassAttr("content_substring_index_build_failure"),
			"error", err,
		)
		return err
	}
	logger.InfoContext(
		ctx,
		"content substring index finalization complete",
		"index_state", "ready",
		"duration_seconds", recordContentSearchIndexFinalizationDuration(ctx, instruments, start),
	)
	return nil
}

func finalizeSecretLinesPhase(
	ctx context.Context,
	database bootstrapDB,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	secretLines secretLinesLifecycle,
	epoch int64,
) error {
	start := time.Now()
	finalizeCtx, cancel := context.WithTimeout(ctx, secretLinesFinalizationTimeout)
	defer cancel()
	err := secretLines.finalize(finalizeCtx, database, epoch, logger, instruments)
	duration := time.Since(start).Seconds()
	if instruments != nil {
		instruments.BootstrapPipelinePhaseDuration.Record(
			ctx,
			duration,
			metric.WithAttributes(
				telemetry.AttrBootstrapPhase(telemetry.BootstrapPhaseSecretLinesFinalization),
				telemetry.AttrCollectorKind("bootstrap-index"),
			),
		)
	}
	if err != nil {
		logger.ErrorContext(
			ctx,
			"secret lines finalization failed",
			"event_name", "secret_lines.finalize_failed",
			"epoch", epoch,
			"duration_seconds", duration,
			telemetry.FailureClassAttr("secret_lines_backfill_failure"),
			"error", err,
		)
	}
	return err
}
