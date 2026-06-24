// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	scipSnapshotLanguageUnknown     = "unknown"
	scipSnapshotResultDisabled      = "disabled"
	scipSnapshotResultNoLanguage    = "no_supported_language"
	scipSnapshotResultBinaryMissing = "binary_unavailable"
	scipSnapshotResultIndexerFailed = "indexer_failed"
	scipSnapshotResultParseFailed   = "parse_failed"
	scipSnapshotResultEmpty         = "empty_result"
	scipSnapshotResultUsed          = "used"
)

func (s NativeRepositorySnapshotter) recordSCIPSnapshotAttempt(ctx context.Context, language string, result string) {
	if s.Instruments == nil {
		return
	}
	if strings.TrimSpace(language) == "" {
		language = scipSnapshotLanguageUnknown
	}
	s.Instruments.SCIPSnapshotAttempts.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrLanguage(language),
		telemetry.AttrResult(result),
	))
}

func (s NativeRepositorySnapshotter) recordSCIPProcessWait(ctx context.Context, language string, wait time.Duration) {
	if s.Instruments == nil {
		return
	}
	if strings.TrimSpace(language) == "" {
		language = scipSnapshotLanguageUnknown
	}
	s.Instruments.SCIPProcessWaitDuration.Record(ctx, wait.Seconds(), metric.WithAttributes(
		telemetry.AttrLanguage(language),
	))
}

func (s NativeRepositorySnapshotter) logSCIPProcessSlotAcquired(ctx context.Context, language string, wait time.Duration) {
	if s.Logger == nil {
		return
	}
	if strings.TrimSpace(language) == "" {
		language = scipSnapshotLanguageUnknown
	}
	s.Logger.DebugContext(
		ctx, "SCIP process slot acquired",
		slog.String("language", language),
		slog.Float64("wait_seconds", wait.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseParsing),
	)
}
