// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SharedIntentAcceptanceWriter atomically persists shared projection intents
// and their authoritative bounded-unit acceptance rows when the backing
// database supports transactions.
type SharedIntentAcceptanceWriter struct {
	database    db.ExecQueryer
	beginner    db.Beginner
	instruments *telemetry.Instruments
}

// NewSharedIntentAcceptanceWriter creates a writer backed by the provided
// database handle.
func NewSharedIntentAcceptanceWriter(database db.ExecQueryer) *SharedIntentAcceptanceWriter {
	return NewSharedIntentAcceptanceWriterWithInstruments(database, nil)
}

// NewSharedIntentAcceptanceWriterWithInstruments creates a writer backed by
// the provided database handle and optional metrics instruments.
func NewSharedIntentAcceptanceWriterWithInstruments(
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
) *SharedIntentAcceptanceWriter {
	writer := &SharedIntentAcceptanceWriter{
		database:    database,
		instruments: instruments,
	}
	if beginner, ok := database.(db.Beginner); ok {
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
	repoLockKeys := repoDependencyAcceptanceUnitIDs(rows)

	if w.beginner == nil {
		if len(repoLockKeys) > 0 {
			return fmt.Errorf("repo-dependency shared intent acceptance requires transactions")
		}
		return upsertSharedIntentArtifacts(ctx, w.database, rows, acceptanceRows, w.instruments)
	}

	tx, err := w.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin shared intent transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, repoKey := range lockstore.SortedUniqueRepoKeys(repoLockKeys) {
		if err := lockstore.AcquireDeferredMaintenanceRepoSharedLock(ctx, tx, repoKey); err != nil {
			return fmt.Errorf("lock repo-dependency acceptance unit %q: %w", repoKey, err)
		}
	}
	if err := upsertSharedIntentArtifacts(ctx, tx, rows, acceptanceRows, w.instruments); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shared intent transaction: %w", err)
	}
	return nil
}

func repoDependencyAcceptanceUnitIDs(rows []reducer.SharedProjectionIntentRow) []string {
	repoIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ProjectionDomain != reducer.DomainRepoDependency {
			continue
		}
		key, ok := sharedProjectionAcceptanceKey(row)
		if !ok {
			continue
		}
		repoIDs = append(repoIDs, key.AcceptanceUnitID)
	}
	return repoIDs
}

func upsertSharedIntentArtifacts(
	ctx context.Context,
	database db.ExecQueryer,
	intentRows []reducer.SharedProjectionIntentRow,
	acceptanceRows []SharedProjectionAcceptance,
	instruments *telemetry.Instruments,
) error {
	if err := NewSharedIntentStore(database).UpsertIntents(ctx, intentRows); err != nil {
		return fmt.Errorf("upsert shared intents: %w", err)
	}

	start := time.Now()
	stale, err := NewSharedProjectionAcceptanceStore(database).UpsertReportingStale(ctx, acceptanceRows)
	if err != nil {
		return fmt.Errorf("upsert shared projection acceptance: %w", err)
	}
	recordSharedAcceptanceUpsertMetrics(ctx, instruments, len(acceptanceRows), time.Since(start))
	recordSharedAcceptanceStaleWrites(ctx, instruments, intentRows, stale)
	return nil
}

// sharedAcceptanceStaleUnknownDomain labels a stale acceptance write whose key
// maps to no intent domain, so the counter's domain label stays bounded.
const sharedAcceptanceStaleUnknownDomain = "unknown"

// recordSharedAcceptanceStaleWrites counts acceptance rows the advance-only
// guard skipped (#6679), labeled by projection domain, and emits one bounded
// WARN line per write call naming the first skipped key and the total. The
// skip itself is correct behavior — the newer generation was kept — so the
// transaction still commits; the signal exists so operators can see
// out-of-order acceptance writers.
func recordSharedAcceptanceStaleWrites(
	ctx context.Context,
	instruments *telemetry.Instruments,
	intentRows []reducer.SharedProjectionIntentRow,
	stale []SharedProjectionAcceptance,
) {
	if len(stale) == 0 {
		return
	}
	if instruments != nil {
		domainByKey := make(map[reducer.SharedProjectionAcceptanceKey]string, len(intentRows))
		for _, row := range intentRows {
			key, ok := sharedProjectionAcceptanceKey(row)
			if !ok {
				continue
			}
			if _, seen := domainByKey[key]; !seen {
				domainByKey[key] = row.ProjectionDomain
			}
		}
		countByDomain := make(map[string]int64, 1)
		for _, row := range stale {
			domain, ok := domainByKey[reducer.SharedProjectionAcceptanceKey{
				ScopeID:          row.ScopeID,
				AcceptanceUnitID: row.AcceptanceUnitID,
				SourceRunID:      row.SourceRunID,
			}]
			if !ok || strings.TrimSpace(domain) == "" {
				// Unreachable while every acceptance row is built from these
				// intents; keeps the label set closed if that ever breaks.
				domain = sharedAcceptanceStaleUnknownDomain
			}
			countByDomain[domain]++
		}
		for domain, count := range countByDomain {
			instruments.SharedAcceptanceStaleWrites.Add(
				ctx,
				count,
				metric.WithAttributes(telemetry.AttrDomain(domain)),
			)
		}
	}

	first := stale[0]
	slog.WarnContext(
		ctx,
		"shared acceptance stale write skipped; stored generation is newer",
		slog.String(telemetry.LogKeyAcceptanceScopeID, first.ScopeID),
		slog.String(telemetry.LogKeyAcceptanceUnitID, first.AcceptanceUnitID),
		slog.String(telemetry.LogKeyAcceptanceSourceRunID, first.SourceRunID),
		slog.String(telemetry.LogKeyAcceptanceGenerationID, first.GenerationID),
		telemetry.AcceptanceStaleCountAttr(len(stale)),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
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
	// Sort by the acceptance primary key so every writer locks conflicting
	// rows in one global order. Map iteration is random, and two concurrent
	// batches that lock the same keys in opposite orders deadlock (40P01),
	// aborting the whole intents+acceptance transaction (#6679).
	slices.SortFunc(acceptanceRows, compareSharedProjectionAcceptanceKeys)
	return acceptanceRows, nil
}

// compareSharedProjectionAcceptanceKeys orders acceptance rows by
// (scope_id, acceptance_unit_id, source_run_id), the table's primary key and
// the lock order the upsert statement enforces with its ORDER BY.
func compareSharedProjectionAcceptanceKeys(a, b SharedProjectionAcceptance) int {
	return cmp.Or(
		strings.Compare(a.ScopeID, b.ScopeID),
		strings.Compare(a.AcceptanceUnitID, b.AcceptanceUnitID),
		strings.Compare(a.SourceRunID, b.SourceRunID),
	)
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
