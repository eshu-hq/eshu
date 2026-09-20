// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// Fingerprint side-table writes (migration 111): narrow
// code_function_fingerprint rows plus one code_fingerprint_band row per LSH
// band. Split out of content_writer_upserts.go under the 500-line file cap;
// behavior is unchanged.

// preparedFingerprintRow is the narrow code_function_fingerprint side-table
// row decoded from one entity's fingerprint metadata keys. fpRenamed and
// fpSketch are nil for exact-only tiers.
type preparedFingerprintRow struct {
	entityID       string
	repoID         string
	fpExact        string
	fpRenamed      any
	fpSketch       any
	tokenCount     int
	hasFingerprint bool
}

// fingerprintRowFromMetadata extracts the #6835 fingerprint side-table row
// from entity metadata. It returns hasFingerprint=false when the entity
// carries no complete fingerprint (absent means "not fingerprinted", never
// "unique"): a missing exact hash or a non-integer token count yields no
// row rather than a partial one.
func fingerprintRowFromMetadata(entityID, repoID string, metadata map[string]any) preparedFingerprintRow {
	row := preparedFingerprintRow{entityID: entityID, repoID: repoID}
	exact, _ := metadata[fingerprint.KeyExact].(string)
	if strings.TrimSpace(exact) == "" {
		return row
	}
	tokens, ok := fingerprintTokenCount(metadata[fingerprint.KeyTokenCount])
	if !ok {
		return row
	}
	row.fpExact = exact
	row.tokenCount = tokens
	row.hasFingerprint = true
	if renamed, _ := metadata[fingerprint.KeyRenamed].(string); strings.TrimSpace(renamed) != "" {
		row.fpRenamed = renamed
	}
	if sketch, _ := metadata[fingerprint.KeySketch].(string); strings.TrimSpace(sketch) != "" {
		row.fpSketch = sketch
	}
	return row
}

func fingerprintTokenCount(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, n >= 0
	case int32:
		return int(n), n >= 0
	case int64:
		return int(n), n >= 0
	case float64:
		return int(n), n >= 0
	default:
		return 0, false
	}
}

// preparedFingerprintBandRow is one code_fingerprint_band row: one LSH band
// of one fingerprinted entity.
type preparedFingerprintBandRow struct {
	repoID   string
	bandNo   int
	bandHash string
	entityID string
}

// fingerprintBandRows expands the persisted sketch hex of each fingerprinted
// row into its LSHBands band rows. A row whose sketch is absent (exact-only
// tier) or undecodable yields no band rows; the fp row itself is unaffected.
func fingerprintBandRows(rows []preparedFingerprintRow) []preparedFingerprintBandRow {
	bands := make([]preparedFingerprintBandRow, 0)
	for _, row := range rows {
		sketchHex, _ := row.fpSketch.(string)
		if sketchHex == "" {
			continue
		}
		sketch, err := fingerprint.DecodeSketch(sketchHex)
		if err != nil {
			continue
		}
		for bandNo, bandHash := range fingerprint.BandHashes(sketch) {
			bands = append(bands, preparedFingerprintBandRow{
				repoID:   row.repoID,
				bandNo:   bandNo,
				bandHash: bandHash,
				entityID: row.entityID,
			})
		}
	}
	return bands
}

// upsertFingerprintBatches persists fingerprint side-table rows for this
// Write() call: one code_function_fingerprint row per fingerprinted entity
// plus one code_fingerprint_band row per sketch band. Entities rewritten
// without fingerprint keys (withdrawn) shed their stale fp and band rows in
// the same call: they survive in content_entities, so the stale-entity reap
// cannot converge them. Batches fan out on the same bounded worker pool as
// entity upserts; band rows for one entity never share a primary key, so
// concurrent batches do not contend on the same row. Withdrawn deletes run
// serially first on entity sets disjoint from the upserts, so no batch can
// delete a row another batch just inserted.
func (w ContentWriter) upsertFingerprintBatches(ctx context.Context, rows []preparedFingerprintRow, indexedAt time.Time) error {
	fpRows := make([]preparedFingerprintRow, 0, len(rows))
	withdrawn := make([]preparedFingerprintRow, 0)
	for _, row := range rows {
		if row.hasFingerprint {
			fpRows = append(fpRows, row)
		} else {
			withdrawn = append(withdrawn, row)
		}
	}
	if err := w.deleteWithdrawnFingerprints(ctx, withdrawn); err != nil {
		return err
	}
	if len(fpRows) == 0 {
		return nil
	}
	batchSize := w.effectiveEntityBatchSize()
	if err := runConcurrentBatches(ctx, len(fpRows), batchSize, w.effectiveBatchConcurrency(), func(c context.Context, start, end int) error {
		return w.upsertFingerprintBatch(c, fpRows[start:end], indexedAt)
	}); err != nil {
		return err
	}
	bands := fingerprintBandRows(fpRows)
	// Shed prior bands for exactly the entities rewritten here before the
	// fresh bands insert, even when this Write yields zero bands: an entity
	// rewritten from sketch-bearing to sketch-less keeps its fp row but
	// contributes no new bands, and without this invalidation its prior
	// bands would survive as current LSH truth. The delete runs once,
	// serially, ahead of the concurrent inserts; concurrent batches touch
	// disjoint entity sets, so no batch can delete a band another batch
	// just inserted.
	if err := w.deleteFingerprintBandsForEntities(ctx, fpRows); err != nil {
		return err
	}
	if len(bands) == 0 {
		return nil
	}
	return runConcurrentBatches(ctx, len(bands), batchSize, w.effectiveBatchConcurrency(), func(c context.Context, start, end int) error {
		return w.upsertFingerprintBandBatch(c, bands[start:end])
	})
}

// deleteFingerprintBandsForEntities removes every code_fingerprint_band row
// for the given freshly upserted fingerprint rows, grouped by repo. Band
// rows are a pure function of the persisted sketch: without this invalidation
// a re-fingerprinted entity keeps its prior bands alongside the new ones.
func (w ContentWriter) deleteFingerprintBandsForEntities(ctx context.Context, rows []preparedFingerprintRow) error {
	byRepo := map[string][]string{}
	for _, row := range rows {
		byRepo[row.repoID] = append(byRepo[row.repoID], row.entityID)
	}
	for repoID, entityIDs := range byRepo {
		if _, err := w.database.ExecContext(ctx, deleteFingerprintBandsForEntitiesSQL, repoID, pgarray.StringArray(entityIDs)); err != nil {
			return fmt.Errorf("delete code_fingerprint_band rows for %d rewritten entities: %w", len(entityIDs), err)
		}
	}
	return nil
}

// deleteWithdrawnFingerprints removes code_function_fingerprint and
// code_fingerprint_band rows for entities rewritten without fingerprint
// keys. Deleting for never-fingerprinted entities is a harmless no-op: the
// writer cannot distinguish "never had" from "just lost" without a read,
// and the delete is idempotent either way. Rows group by repo into one
// statement per table, matching the scoped-delete shape the band
// invalidation already uses.
func (w ContentWriter) deleteWithdrawnFingerprints(ctx context.Context, rows []preparedFingerprintRow) error {
	if len(rows) == 0 {
		return nil
	}
	byRepo := map[string][]string{}
	for _, row := range rows {
		byRepo[row.repoID] = append(byRepo[row.repoID], row.entityID)
	}
	for repoID, entityIDs := range byRepo {
		if _, err := w.database.ExecContext(ctx, deleteWithdrawnFingerprintSQL, repoID, pgarray.StringArray(entityIDs)); err != nil {
			return fmt.Errorf("delete withdrawn code_function_fingerprint rows for %d entities: %w", len(entityIDs), err)
		}
		if _, err := w.database.ExecContext(ctx, deleteFingerprintBandsForEntitiesSQL, repoID, pgarray.StringArray(entityIDs)); err != nil {
			return fmt.Errorf("delete withdrawn code_fingerprint_band rows for %d entities: %w", len(entityIDs), err)
		}
	}
	return nil
}

// upsertFingerprintBatch inserts one batch of fingerprint rows.
func (w ContentWriter) upsertFingerprintBatch(ctx context.Context, batch []preparedFingerprintRow, indexedAt time.Time) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*7)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 7
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7,
		)

		args = append(
			args,
			row.entityID,
			row.repoID,
			row.fpExact,
			row.fpRenamed,
			row.fpSketch,
			row.tokenCount,
			indexedAt,
		)
	}

	query := upsertFingerprintBatchPrefix + values.String() + upsertFingerprintBatchSuffix

	if _, err := w.database.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert code_function_fingerprint batch (%d rows): %w", len(batch), err)
	}

	return nil
}

// upsertFingerprintBandBatch inserts one batch of band rows. Re-upserts are
// ON CONFLICT DO NOTHING: bands are a pure function of the persisted sketch.
func (w ContentWriter) upsertFingerprintBandBatch(ctx context.Context, batch []preparedFingerprintBandRow) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*4)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 4
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4,
		)

		args = append(
			args,
			row.repoID,
			row.bandNo,
			row.bandHash,
			row.entityID,
		)
	}

	query := upsertFingerprintBandBatchPrefix + values.String() + upsertFingerprintBandBatchSuffix

	if _, err := w.database.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert code_fingerprint_band batch (%d rows): %w", len(batch), err)
	}

	return nil
}
