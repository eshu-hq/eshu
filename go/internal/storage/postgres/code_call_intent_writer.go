// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CodeCallIntentWriter preserves the reducer-facing constructor and method
// surface for code-call intent persistence while delegating the atomic storage
// work to the generic shared intent acceptance writer.
type CodeCallIntentWriter = SharedIntentAcceptanceWriter

// NewCodeCallIntentWriter creates a code-call writer backed by the provided
// database handle.
func NewCodeCallIntentWriter(database db.ExecQueryer) *CodeCallIntentWriter {
	return NewSharedIntentAcceptanceWriter(database)
}

// NewCodeCallIntentWriterWithInstruments creates a code-call writer backed by
// the provided database handle and optional metrics instruments.
func NewCodeCallIntentWriterWithInstruments(database db.ExecQueryer, instruments *telemetry.Instruments) *CodeCallIntentWriter {
	return NewSharedIntentAcceptanceWriterWithInstruments(database, instruments)
}

func recordSharedAcceptanceUpsertMetrics(
	ctx context.Context,
	instruments *telemetry.Instruments,
	rowCount int,
	duration time.Duration,
) {
	if instruments == nil || rowCount <= 0 {
		return
	}

	instruments.SharedAcceptanceUpserts.Add(ctx, int64(rowCount))
	instruments.SharedAcceptanceUpsertDuration.Record(ctx, duration.Seconds())
}
