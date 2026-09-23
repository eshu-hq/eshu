// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// Fingerprint side-table writes (migration 111): narrow
// code_function_fingerprint rows plus one code_fingerprint_band row per LSH
// band. Split out of content_writer_upserts.go under the 500-line file cap;
// behavior is unchanged.

// preparedFingerprintRow is the narrow code_function_fingerprint side-table
// row decoded from one entity's fingerprint metadata keys. fpRenamed,
// fpSketch, and fpShingles are nil for exact-only tiers; fpShingles is also
// nil for pre-#6837 payloads that carry no shingle set.
type preparedFingerprintRow struct {
	entityID       string
	repoID         string
	fpExact        string
	fpRenamed      any
	fpSketch       any
	fpShingles     any
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
	if shingles, _ := metadata[fingerprint.KeyShingles].(string); strings.TrimSpace(shingles) != "" {
		row.fpShingles = shingles
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
// upsertFingerprintBatches reports whether this call (re)published
// fingerprint side-table truth for the #6837 trigger signal: fingerprinted
// rows upserted, or withdrawn deletes that actually removed stale rows.
func (w ContentWriter) upsertFingerprintBatches(ctx context.Context, rows []preparedFingerprintRow, indexedAt time.Time) (bool, error) {
	fpRows := make([]preparedFingerprintRow, 0, len(rows))
	withdrawn := make([]preparedFingerprintRow, 0)
	for _, row := range rows {
		if row.hasFingerprint {
			fpRows = append(fpRows, row)
		} else {
			withdrawn = append(withdrawn, row)
		}
	}
	withdrawnAffected, err := w.deleteWithdrawnFingerprints(ctx, withdrawn)
	if err != nil {
		return false, err
	}
	if len(fpRows) == 0 {
		return withdrawnAffected > 0, nil
	}
	batchSize := w.effectiveEntityBatchSize()
	if err := runConcurrentBatches(ctx, len(fpRows), batchSize, w.effectiveBatchConcurrency(), func(c context.Context, start, end int) error {
		return w.upsertFingerprintBatch(c, fpRows[start:end], indexedAt)
	}); err != nil {
		return false, err
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
	if _, err := w.deleteFingerprintBandsForEntities(ctx, fpRows); err != nil {
		return false, err
	}
	if len(bands) == 0 {
		return true, nil
	}
	if err := runConcurrentBatches(ctx, len(bands), batchSize, w.effectiveBatchConcurrency(), func(c context.Context, start, end int) error {
		return w.upsertFingerprintBandBatch(c, bands[start:end])
	}); err != nil {
		return false, err
	}
	return true, nil
}

// deleteFingerprintBandsForEntities removes every code_fingerprint_band row
// for the given freshly upserted fingerprint rows, grouped by repo. Band
// rows are a pure function of the persisted sketch: without this invalidation
// a re-fingerprinted entity keeps its prior bands alongside the new ones.
// Entity sets chunk at contentFileBatchSize, matching the sibling delete
// convention in Write(): a repo-scale Write rewrites thousands of entities,
// and one unbounded text[] per repo would ship multi-megabyte parameters.
// It returns the total rows deleted: the #6837 trigger signal counts a
// withdrawal as changed only when stale rows actually disappeared.
func (w ContentWriter) deleteFingerprintBandsForEntities(ctx context.Context, rows []preparedFingerprintRow) (int64, error) {
	byRepo := map[string][]string{}
	for _, row := range rows {
		byRepo[row.repoID] = append(byRepo[row.repoID], row.entityID)
	}
	var affected int64
	for repoID, entityIDs := range byRepo {
		for _, chunk := range chunkEntityIDs(entityIDs, contentFileBatchSize) {
			res, err := w.database.ExecContext(ctx, deleteFingerprintBandsForEntitiesSQL, repoID, array.StringArray(chunk))
			if err != nil {
				return 0, fmt.Errorf("delete code_fingerprint_band rows for %d rewritten entities: %w", len(chunk), err)
			}
			affected += rowsAffected(res)
		}
	}
	return affected, nil
}

// rowsAffected returns the deleted/written row count of an Exec result,
// treating an uncountable result as zero rather than failing the Write: the
// count feeds the #6837 trigger signal only, never correctness.
func rowsAffected(res interface{ RowsAffected() (int64, error) }) int64 {
	if res == nil {
		return 0
	}
	n, err := res.RowsAffected()
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// chunkEntityIDs splits an entity ID set into contentFileBatchSize chunks so
// scoped (repo_id, entity text[]) deletes stay bounded on repo-scale Writes.
// A non-positive size degrades to a single chunk rather than looping forever.
func chunkEntityIDs(ids []string, size int) [][]string {
	if size <= 0 || len(ids) <= size {
		return [][]string{ids}
	}
	chunks := make([][]string, 0, len(ids)/size+1)
	for i := 0; i < len(ids); i += size {
		end := i + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}

// deleteWithdrawnFingerprints removes code_function_fingerprint and
// code_fingerprint_band rows for entities rewritten without fingerprint
// keys. Deleting for never-fingerprinted entities is a harmless no-op: the
// writer cannot distinguish "never had" from "just lost" without a read,
// and the delete is idempotent either way. Rows group by repo and chunk at
// contentFileBatchSize per table, matching the scoped-delete shape the band
// invalidation already uses: withdrawn holds every non-fingerprinted entity
// in the Write, so an unchunked array would grow with the repo.
// deleteWithdrawnFingerprints returns the total stale side-table rows its
// scoped deletes removed: the #6837 trigger signal counts a withdrawal as
// changed only when rows actually disappeared, so an already-clean side
// table stays quiet.
func (w ContentWriter) deleteWithdrawnFingerprints(ctx context.Context, rows []preparedFingerprintRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	byRepo := map[string][]string{}
	for _, row := range rows {
		byRepo[row.repoID] = append(byRepo[row.repoID], row.entityID)
	}
	var affected int64
	for repoID, entityIDs := range byRepo {
		for _, chunk := range chunkEntityIDs(entityIDs, contentFileBatchSize) {
			res, err := w.database.ExecContext(ctx, deleteWithdrawnFingerprintSQL, repoID, array.StringArray(chunk))
			if err != nil {
				return 0, fmt.Errorf("delete withdrawn code_function_fingerprint rows for %d entities: %w", len(chunk), err)
			}
			affected += rowsAffected(res)
			res, err = w.database.ExecContext(ctx, deleteFingerprintBandsForEntitiesSQL, repoID, array.StringArray(chunk))
			if err != nil {
				return 0, fmt.Errorf("delete withdrawn code_fingerprint_band rows for %d entities: %w", len(chunk), err)
			}
			affected += rowsAffected(res)
		}
	}
	return affected, nil
}

// upsertFingerprintBatch inserts one batch of fingerprint rows.
func (w ContentWriter) upsertFingerprintBatch(ctx context.Context, batch []preparedFingerprintRow, indexedAt time.Time) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*8)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 8
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8,
		)

		args = append(
			args,
			row.entityID,
			row.repoID,
			row.fpExact,
			row.fpRenamed,
			row.fpSketch,
			row.fpShingles,
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
