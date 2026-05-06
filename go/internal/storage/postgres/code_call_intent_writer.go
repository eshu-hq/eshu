package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CodeCallIntentWriter preserves the reducer-facing constructor and method
// surface for code-call intent persistence while delegating the atomic storage
// work to the generic shared intent acceptance writer.
type CodeCallIntentWriter = SharedIntentAcceptanceWriter

// NewCodeCallIntentWriter creates a code-call writer backed by the provided
// database handle.
func NewCodeCallIntentWriter(db ExecQueryer) *CodeCallIntentWriter {
	return NewSharedIntentAcceptanceWriter(db)
}

// NewCodeCallIntentWriterWithInstruments creates a code-call writer backed by
// the provided database handle and optional metrics instruments.
func NewCodeCallIntentWriterWithInstruments(db ExecQueryer, instruments *telemetry.Instruments) *CodeCallIntentWriter {
	return NewSharedIntentAcceptanceWriterWithInstruments(db, instruments)
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
