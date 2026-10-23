// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// StatusTelemetry is the production StatusRecorder and ResultRecorder for the
// component-extension worker (#8908). Extracted collectors emit no telemetry
// of their own; their SDK status rows are the operator contract, and this
// adapter turns them into the eshu_dp_component_extension_* instruments plus
// one structured log line per claim result.
//
// Metric labels are collector_kind, state, status_class, and failure_class
// only, folded by telemetry.ComponentExtensionStatusRecorder. The log line
// carries the same bounded fields plus counts. Neither ever carries the scope
// id, instance id, work item id, generation id, source URL, or any payload,
// even though StatusRecord holds some of those for other recorders.
//
// It is safe for concurrent use and never fails a claim: RecordExtensionStatus
// always returns nil. Retryable, terminal, and host-verdict results log at
// WARN; complete, unchanged, and partial SDK results log at INFO.
type StatusTelemetry struct {
	recorder *telemetry.ComponentExtensionStatusRecorder
	logger   *slog.Logger
}

// NewStatusTelemetry builds the adapter. A nil inst disables the metrics and a
// nil logger disables the log line.
func NewStatusTelemetry(inst *telemetry.Instruments, logger *slog.Logger) *StatusTelemetry {
	return &StatusTelemetry{
		recorder: telemetry.NewComponentExtensionStatusRecorder(inst),
		logger:   logger,
	}
}

// RecordExtensionStatus implements StatusRecorder for one validated SDK
// status row. It records metrics only; the log line is per result.
func (t *StatusTelemetry) RecordExtensionStatus(ctx context.Context, record StatusRecord) error {
	if t == nil {
		return nil
	}
	t.recorder.RecordStatus(
		ctx,
		record.CollectorKind,
		string(record.State),
		string(record.Class),
		record.FailureClass,
		record.FactCount,
		record.WarningCount,
		record.SourceLatencyMS,
	)
	return nil
}

// RecordExtensionResult implements ResultRecorder for one claim result.
func (t *StatusTelemetry) RecordExtensionResult(ctx context.Context, record ResultRecord) {
	if t == nil {
		return
	}
	t.recorder.RecordResult(ctx, record.CollectorKind, string(record.State), record.FailureClass, record.RetryAfter)
	t.log(ctx, record)
}

func (t *StatusTelemetry) log(ctx context.Context, record ResultRecord) {
	if t.logger == nil {
		return
	}
	level := slog.LevelInfo
	if record.Origin == ResultOriginHost ||
		record.State == sdkcollector.ResultRetryable ||
		record.State == sdkcollector.ResultTerminal {
		level = slog.LevelWarn
	}
	if !t.logger.Enabled(ctx, level) {
		return
	}
	t.logger.LogAttrs(ctx, level, "component extension result",
		slog.String(telemetry.LogKeyCollectorKind, record.CollectorKind),
		slog.String(telemetry.LogKeyFailureClass, record.FailureClass),
		slog.String(contract.LogKeyExtensionStatusOrigin, string(record.Origin)),
		slog.String(contract.LogKeyExtensionStatusState, string(record.State)),
		slog.Int(contract.LogKeyExtensionStatusCount, record.StatusCount),
		slog.Int(contract.LogKeyExtensionStatusFactCount, record.FactCount),
		slog.Int(contract.LogKeyExtensionStatusWarningCount, record.WarningCount),
		slog.Int(contract.LogKeyExtensionStatusSourceLatencyMS, record.SourceLatencyMS),
		slog.Bool(contract.LogKeyExtensionStatusPartial, record.Partial),
		slog.Float64(contract.LogKeyExtensionStatusRetryAfterSeconds, record.RetryAfter.Seconds()),
		slog.Int(contract.LogKeyExtensionStatusRetryAfterRequestedSeconds, record.RetryAfterRequestedSeconds),
	)
}

var (
	_ StatusRecorder = (*StatusTelemetry)(nil)
	_ ResultRecorder = (*StatusTelemetry)(nil)
)
