// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

type reducerClassifiedFailure interface {
	FailureClass() string
}

func reducerExecutionFailureClass(err error) string {
	var classified reducerClassifiedFailure
	if errors.As(err, &classified) {
		if failureClass := strings.TrimSpace(classified.FailureClass()); failureClass != "" {
			return failureClass
		}
	}
	return "reducer_failure"
}

type reducerHeartbeatStop func() error

// reducerPreHeartbeatFailure marks a heartbeat failure that happened before
// Executor.Execute ever ran, so the caller can tell it apart from a failure
// during or after real handler work. No handler work ran under this claim,
// so the intent must never be routed through WorkSink.Fail (which can
// dead-letter it): the correct recovery is to leave the lease unrenewed so
// the expired-lease reclaim path (#4464) picks it back up, or a retry.
type reducerPreHeartbeatFailure struct {
	err error
}

func (e *reducerPreHeartbeatFailure) Error() string { return e.err.Error() }
func (e *reducerPreHeartbeatFailure) Unwrap() error { return e.err }

// startHeartbeat starts the reducer lease heartbeat loop for a claimed
// intent. It emits one heartbeat synchronously, before returning, so a
// worker that stalls (GC pause, slow first graph write) immediately after
// claim cannot let the lease expire before any heartbeat has landed (#4447).
// HeartbeatInterval = LeaseDuration/2 and the periodic ticker only fires
// after a full interval has elapsed, leaving that startup window open
// without this immediate pre-heartbeat.
//
// If the immediate pre-heartbeat itself fails, the returned stop function's
// error is wrapped in reducerPreHeartbeatFailure so executeWithTelemetry can
// skip Executor.Execute and WorkSink.Fail entirely (#4447 follow-up): no
// handler work has run yet, so there is nothing to execute or dead-letter.
func (s Service) startHeartbeat(
	ctx context.Context,
	intent Intent,
	workerID int,
) (context.Context, reducerHeartbeatStop) {
	if s.Heartbeater == nil || s.HeartbeatInterval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)

	if err := s.Heartbeater.Heartbeat(heartbeatCtx, intent); err != nil {
		heartbeatErr := fmt.Errorf("heartbeat reducer work: %w", err)
		s.recordReducerHeartbeatMissed(heartbeatCtx, intent, workerID, heartbeatErr)
		cancel()
		preFailure := &reducerPreHeartbeatFailure{err: heartbeatErr}
		return heartbeatCtx, func() error { return preFailure }
	}

	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(s.HeartbeatInterval)
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				if err := s.Heartbeater.Heartbeat(heartbeatCtx, intent); err != nil {
					heartbeatErr = fmt.Errorf("heartbeat reducer work: %w", err)
					s.recordReducerHeartbeatMissed(heartbeatCtx, intent, workerID, heartbeatErr)
					cancel()
				}
			}
		}
	}()

	var once sync.Once
	return heartbeatCtx, func() error {
		var heartbeatErr error
		once.Do(func() {
			cancel()
			heartbeatErr = <-done
		})
		return heartbeatErr
	}
}

// recordReducerHeartbeatMissed logs and increments the operator-facing
// missed-heartbeat signal for a reducer lease heartbeat failure, whether it
// came from the immediate pre-heartbeat or a later periodic tick.
func (s Service) recordReducerHeartbeatMissed(
	ctx context.Context,
	intent Intent,
	workerID int,
	heartbeatErr error,
) {
	if s.Instruments != nil {
		s.Instruments.ReducerHeartbeatMissed.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
	}
	if s.Logger != nil {
		domainAttrs := telemetry.DomainAttrs(string(intent.Domain), firstReducerPartitionKey(intent))
		logAttrs := make([]any, 0, len(domainAttrs)+5)
		for _, attribute := range domainAttrs {
			logAttrs = append(logAttrs, attribute)
		}
		logAttrs = append(
			logAttrs,
			log.Queue("reducer"),
			log.IntentID(intent.IntentID),
			log.WorkerID(fmt.Sprintf("%d", workerID)),
			slog.Duration("heartbeat_interval", s.HeartbeatInterval),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
			telemetry.FailureClassAttr("lease_heartbeat_failure"),
			log.Err(heartbeatErr),
		)
		s.Logger.ErrorContext(ctx, "reducer lease heartbeat failed", logAttrs...)
	}
}

func firstReducerPartitionKey(intent Intent) string {
	if len(intent.EntityKeys) == 0 {
		return ""
	}
	keys := append([]string(nil), intent.EntityKeys...)
	slices.Sort(keys)
	return keys[0]
}
