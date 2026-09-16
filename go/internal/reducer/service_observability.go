// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// reducerQueueWaitSeconds returns how long reducer work was visible before a
// worker started executing it. Negative or missing timestamps are clamped so
// clock skew and legacy rows do not pollute latency histograms.
func reducerQueueWaitSeconds(start time.Time, availableAt time.Time) float64 {
	if availableAt.IsZero() {
		return 0
	}
	wait := start.Sub(availableAt)
	if wait < 0 {
		return 0
	}
	return wait.Seconds()
}

func (s Service) recordReducerResult(ctx context.Context, intent Intent, result Result, duration float64, queueWait float64, status string, workerID int, execErr error) {
	if s.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
			attribute.String("queue", "reducer"),
			attribute.String("status", status),
		)
		s.Instruments.ReducerRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
		s.Instruments.ReducerQueueWaitDuration.Record(ctx, queueWait, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
		s.Instruments.ReducerExecutions.Add(ctx, 1, attrs)
	}

	if s.Logger != nil {
		partitionKey := ""
		if len(intent.EntityKeys) > 0 {
			partitionKey = intent.EntityKeys[0]
		}
		domainAttrs := telemetry.DomainAttrs(string(intent.Domain), partitionKey)
		logAttrs := make([]any, 0, len(domainAttrs)+4)
		for _, a := range domainAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, log.Queue("reducer"))
		logAttrs = append(logAttrs, log.IntentID(intent.IntentID))
		logAttrs = append(logAttrs, log.Status(status))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("handler_duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("queue_wait_seconds", queueWait))
		// Emit per-phase sub-timings when the handler populated them. Keys match
		// the workload materialization log attribute names so operators can
		// correlate the service-level log line with the handler-level log line
		// without reading two separate log streams.
		for k, v := range result.SubDurations {
			logAttrs = append(logAttrs, slog.Float64("sub_duration_"+k+"_seconds", v))
		}
		// Emit non-duration diagnostic signals (counts and flags such as
		// input_ready and written_rows) under a separate sub_signal_<key> prefix
		// with NO _seconds suffix, so an operator never misreads a row count or a
		// boolean flag as a wall-time measurement.
		for k, v := range result.SubSignals {
			logAttrs = append(logAttrs, slog.Float64("sub_signal_"+k, v))
		}
		logAttrs = append(logAttrs, log.WorkerID(fmt.Sprintf("%d", workerID)))
		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseReduction))
		switch status {
		case "failed", "ack_failed":
			message := "reducer execution failed"
			failureClass := reducerExecutionFailureClass(execErr)
			if status == "ack_failed" {
				failureClass = "ack_failure"
				message = "reducer ack failed"
			}
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(failureClass))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.ErrorContext(ctx, message, logAttrs...)
		case "superseded":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("generation_superseded"))
			s.Logger.InfoContext(ctx, "reducer intent superseded", logAttrs...)
		case "lease_lost_before_start":
			// No handler work ran under this claim; the lease is left
			// unrenewed for the expired-lease reclaim path (#4464) rather
			// than dead-lettered, so this is an operator-visible warning, not
			// a terminal failure.
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("lease_heartbeat_failure"))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.WarnContext(ctx, "reducer claim lost its lease before handler start", logAttrs...)
		case "lease_lost_during_execution":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("execution_claim_rejected"))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.WarnContext(ctx, "reducer claim lost its lease during handler execution", logAttrs...)
		case "ack_claim_rejected":
			// The ACK path emits a separate warning with the stale-claim context.
		case "ack_outcome_unknown":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("ack_outcome_unknown"), log.Err(execErr))
			s.Logger.WarnContext(ctx, "reducer batch ack outcome unknown", logAttrs...)
		default:
			s.Logger.InfoContext(ctx, "reducer execution succeeded", logAttrs...)
		}
	}
}
