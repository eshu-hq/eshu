package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SharedIntentAcceptanceWriter atomically persists shared projection intents
// and their authoritative bounded-unit acceptance rows when the backing
// database supports transactions.
type SharedIntentAcceptanceWriter struct {
	db          ExecQueryer
	beginner    Beginner
	instruments *telemetry.Instruments
}

// NewSharedIntentAcceptanceWriter creates a writer backed by the provided
// database handle.
func NewSharedIntentAcceptanceWriter(db ExecQueryer) *SharedIntentAcceptanceWriter {
	return NewSharedIntentAcceptanceWriterWithInstruments(db, nil)
}

// NewSharedIntentAcceptanceWriterWithInstruments creates a writer backed by
// the provided database handle and optional metrics instruments.
func NewSharedIntentAcceptanceWriterWithInstruments(
	db ExecQueryer,
	instruments *telemetry.Instruments,
) *SharedIntentAcceptanceWriter {
	writer := &SharedIntentAcceptanceWriter{
		db:          db,
		instruments: instruments,
	}
	if beginner, ok := db.(Beginner); ok {
		writer.beginner = beginner
	}
	return writer
}

// UpsertIntents persists shared intents and acceptance rows together.
func (w *SharedIntentAcceptanceWriter) UpsertIntents(
	ctx context.Context,
	rows []reducer.SharedProjectionIntentRow,
) error {
	if len(rows) == 0 {
		return nil
	}

	acceptanceRows, err := buildSharedProjectionAcceptanceRows(rows)
	if err != nil {
		return err
	}

	if w.beginner == nil {
		return upsertSharedIntentArtifacts(ctx, w.db, rows, acceptanceRows, w.instruments)
	}

	tx, err := w.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin shared intent transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := upsertSharedIntentArtifacts(ctx, tx, rows, acceptanceRows, w.instruments); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shared intent transaction: %w", err)
	}
	return nil
}

func upsertSharedIntentArtifacts(
	ctx context.Context,
	db ExecQueryer,
	intentRows []reducer.SharedProjectionIntentRow,
	acceptanceRows []SharedProjectionAcceptance,
	instruments *telemetry.Instruments,
) error {
	if err := NewSharedIntentStore(db).UpsertIntents(ctx, intentRows); err != nil {
		return fmt.Errorf("upsert shared intents: %w", err)
	}

	start := time.Now()
	if err := NewSharedProjectionAcceptanceStore(db).Upsert(ctx, acceptanceRows); err != nil {
		return fmt.Errorf("upsert shared projection acceptance: %w", err)
	}
	recordSharedAcceptanceUpsertMetrics(ctx, instruments, len(acceptanceRows), time.Since(start))
	return nil
}

func buildSharedProjectionAcceptanceRows(
	rows []reducer.SharedProjectionIntentRow,
) ([]SharedProjectionAcceptance, error) {
	byKey := make(map[reducer.SharedProjectionAcceptanceKey]SharedProjectionAcceptance, len(rows))

	for _, row := range rows {
		key, ok := sharedProjectionAcceptanceKey(row)
		if !ok {
			return nil, fmt.Errorf("shared intent %q is missing acceptance identity", row.IntentID)
		}
		generationID := strings.TrimSpace(row.GenerationID)
		if generationID == "" {
			return nil, fmt.Errorf("shared intent %q is missing generation_id", row.IntentID)
		}

		acceptedAt := row.CreatedAt.UTC()
		if acceptedAt.IsZero() {
			acceptedAt = time.Now().UTC()
		}

		current, exists := byKey[key]
		if exists {
			if current.GenerationID != generationID {
				return nil, fmt.Errorf(
					"acceptance key %q/%q/%q has mixed generations %q and %q",
					key.ScopeID,
					key.AcceptanceUnitID,
					key.SourceRunID,
					current.GenerationID,
					generationID,
				)
			}
			if acceptedAt.After(current.UpdatedAt) {
				current.AcceptedAt = acceptedAt
				current.UpdatedAt = acceptedAt
				byKey[key] = current
			}
			continue
		}

		byKey[key] = SharedProjectionAcceptance{
			ScopeID:          key.ScopeID,
			AcceptanceUnitID: key.AcceptanceUnitID,
			SourceRunID:      key.SourceRunID,
			GenerationID:     generationID,
			AcceptedAt:       acceptedAt,
			UpdatedAt:        acceptedAt,
		}
	}

	acceptanceRows := make([]SharedProjectionAcceptance, 0, len(byKey))
	for _, row := range byKey {
		acceptanceRows = append(acceptanceRows, row)
	}
	return acceptanceRows, nil
}

func sharedProjectionAcceptanceKey(
	row reducer.SharedProjectionIntentRow,
) (reducer.SharedProjectionAcceptanceKey, bool) {
	if key, ok := row.AcceptanceKey(); ok {
		return key, true
	}

	scopeID := sharedIntentScopeID(row)
	acceptanceUnitID := sharedIntentAcceptanceUnitID(row)
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(acceptanceUnitID) == "" {
		return reducer.SharedProjectionAcceptanceKey{}, false
	}

	return reducer.SharedProjectionAcceptanceKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      row.SourceRunID,
	}, strings.TrimSpace(row.SourceRunID) != ""
}
