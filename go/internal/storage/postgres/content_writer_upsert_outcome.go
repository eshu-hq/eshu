// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// contentFileOutcomePreReadSQL reads the stored state a batch is about to be
// upserted over, so the writer can count what the upsert will do (#8950). It is
// a plain MVCC SELECT: no row lock, no write, and the body is read only through
// octet_length, which sizes a TOASTed value from its header without detoasting
// it. It joins the batch's (repo_id, relative_path) keys to the primary key.
//
// This is a measure-first instrument, not part of the upsert: it never changes
// what the upsert writes, and a failure to run it never fails the Write.
const contentFileOutcomePreReadSQL = `
SELECT c.repo_id, c.relative_path, c.content_hash, octet_length(c.content),
       c.commit_sha, c.line_count, c.language, c.artifact_type,
       c.template_dialect, c.iac_relevant, c.generation_id
FROM unnest($1::text[], $2::text[]) AS k(repo_id, relative_path)
JOIN content_files AS c
  ON c.repo_id = k.repo_id AND c.relative_path = k.relative_path
`

// fileOutcome is one row's classification against the stored row. Its values
// index fileOutcomeNames and fileUpsertTally.
type fileOutcome int

const (
	outcomeInserted fileOutcome = iota
	outcomeUnchangedSkipped
	outcomeMetadataOnly
	outcomeContentChanged
	fileOutcomeCount
)

// fileOutcomeNames are the closed telemetry outcome values, indexed by
// fileOutcome.
var fileOutcomeNames = [fileOutcomeCount]string{
	telemetry.ContentFileUpsertOutcomeInserted,
	telemetry.ContentFileUpsertOutcomeUnchangedSkipped,
	telemetry.ContentFileUpsertOutcomeMetadataOnly,
	telemetry.ContentFileUpsertOutcomeContentChanged,
}

// String returns the telemetry outcome value.
func (o fileOutcome) String() string { return fileOutcomeNames[o] }

// storedContentFile is the content-free stored state of one content_files row.
type storedContentFile struct {
	contentHash     string
	bodyBytes       sql.NullInt64
	commitSHA       sql.NullString
	lineCount       sql.NullInt64
	language        sql.NullString
	artifactType    sql.NullString
	templateDialect sql.NullString
	iacRelevant     sql.NullBool
	generation      sql.NullString
}

// fileUpsertTally accumulates the outcome counts and body bytes of the batches
// of one Write, plus the cost of classifying them.
type fileUpsertTally struct {
	rows                [fileOutcomeCount]int
	bodyBytes           [fileOutcomeCount]int64
	classifiedBatches   int
	unclassifiedBatches int
	classifySeconds     float64
}

// add folds a per-batch tally into t.
func (t *fileUpsertTally) add(batch fileUpsertTally) {
	for i := range t.rows {
		t.rows[i] += batch.rows[i]
		t.bodyBytes[i] += batch.bodyBytes[i]
	}
	t.classifiedBatches += batch.classifiedBatches
	t.unclassifiedBatches += batch.unclassifiedBatches
	t.classifySeconds += batch.classifySeconds
}

// classifyContentFileRow predicts what the guarded upsert does to one row,
// from the stored state read before it. The comparison mirrors
// upsertContentFileConflictClause: a row is skipped only when every guarded
// column matches (NULL-safe) and the content is equal.
//
// The body is not read, so content equality is a proxy: the caller-supplied
// content digest and the body size must both match. A digest that does not
// describe its body (a caller bug) can read as unchanged here while the guard,
// which compares the text, rewrites the row. The implementation that follows
// this measure-first counter takes its counts from the statement's own result
// instead.
func classifyContentFileRow(stored *storedContentFile, row preparedFileRow, tag any) fileOutcome {
	if stored == nil {
		return outcomeInserted
	}
	contentSame := stored.contentHash == row.contentHash &&
		stored.bodyBytes.Valid && stored.bodyBytes.Int64 == int64(len(row.body))
	if !contentSame {
		return outcomeContentChanged
	}
	otherSame := stored.lineCount.Valid && stored.lineCount.Int64 == int64(row.lineCount) &&
		nullStringEquals(stored.language, row.language) &&
		nullStringEquals(stored.artifactType, row.artifactType) &&
		nullStringEquals(stored.templateDialect, row.templateDialect) &&
		nullBoolEquals(stored.iacRelevant, row.iacRelevant)
	if !otherSame {
		return outcomeContentChanged
	}
	lineageSame := nullStringEquals(stored.commitSHA, row.commitSHA) && nullStringEquals(stored.generation, tag)
	if lineageSame {
		return outcomeUnchangedSkipped
	}
	return outcomeMetadataOnly
}

// nullStringEquals is the NULL-safe equality (IS NOT DISTINCT FROM) of a stored
// nullable text column and an incoming bind value (nil or a string).
func nullStringEquals(stored sql.NullString, incoming any) bool {
	if incoming == nil {
		return !stored.Valid
	}
	text, ok := incoming.(string)
	return ok && stored.Valid && stored.String == text
}

// nullBoolEquals is the NULL-safe equality of a stored nullable boolean and an
// incoming bind value (nil or a bool).
func nullBoolEquals(stored sql.NullBool, incoming any) bool {
	if incoming == nil {
		return !stored.Valid
	}
	flag, ok := incoming.(bool)
	return ok && stored.Valid && stored.Bool == flag
}

// fileUpsertObservation carries one Write's snapshot label and running tally
// through its file batches.
type fileUpsertObservation struct {
	snapshot string
	tally    fileUpsertTally
}

// newFileUpsertObservation returns the observation for a Write, or nil when
// neither a counter nor a logger would use the classification, so a writer with
// no observers pays no pre-read.
func (w ContentWriter) newFileUpsertObservation(fullSnapshot bool) *fileUpsertObservation {
	counted := w.instruments != nil && w.instruments.ContentFileUpserts != nil
	if !counted && w.Logger == nil {
		return nil
	}
	snapshot := telemetry.ContentFileSnapshotDelta
	if fullSnapshot {
		snapshot = telemetry.ContentFileSnapshotFull
	}
	return &fileUpsertObservation{snapshot: snapshot}
}

// classifyFileBatch runs the pre-read for one batch and classifies every row.
// The returned slice parallels batch.
func (w ContentWriter) classifyFileBatch(ctx context.Context, batch []preparedFileRow, tag any) ([]fileOutcome, error) {
	repoIDs := make([]string, len(batch))
	paths := make([]string, len(batch))
	for i, row := range batch {
		repoIDs[i], paths[i] = row.repoID, row.path
	}
	rows, err := w.database.QueryContext(ctx, contentFileOutcomePreReadSQL, repoIDs, paths)
	if err != nil {
		return nil, fmt.Errorf("read stored content_files state for upsert outcome: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type fileKey struct{ repoID, path string }
	stored := make(map[fileKey]*storedContentFile, len(batch))
	for rows.Next() {
		var (
			key fileKey
			row storedContentFile
		)
		if err := rows.Scan(
			&key.repoID, &key.path, &row.contentHash, &row.bodyBytes, &row.commitSHA, &row.lineCount,
			&row.language, &row.artifactType, &row.templateDialect, &row.iacRelevant, &row.generation,
		); err != nil {
			return nil, fmt.Errorf("scan stored content_files state for upsert outcome: %w", err)
		}
		stored[key] = &row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stored content_files state for upsert outcome: %w", err)
	}

	outcomes := make([]fileOutcome, len(batch))
	for i, row := range batch {
		outcomes[i] = classifyContentFileRow(stored[fileKey{repoID: row.repoID, path: row.path}], row, tag)
	}
	return outcomes, nil
}

// preClassify runs the pre-read when obs is active and returns the outcomes, or
// nil when the batch goes unclassified. A pre-read failure is logged and
// counted in the stage log, never returned: the classification is observability
// and must not fail or change the Write.
func (w ContentWriter) preClassify(ctx context.Context, obs *fileUpsertObservation, batch []preparedFileRow, tag any) []fileOutcome {
	if obs == nil {
		return nil
	}
	start := time.Now()
	outcomes, err := w.classifyFileBatch(ctx, batch, tag)
	obs.tally.classifySeconds += time.Since(start).Seconds()
	if err != nil {
		obs.tally.unclassifiedBatches++
		if w.Logger != nil {
			w.Logger.WarnContext(ctx, "content file upsert outcome classification failed; batch not counted",
				"event_name", "content_file_upsert_outcome.classify_failed",
				"batch_file_count", len(batch), "error", err)
		}
		return nil
	}
	return outcomes
}

// recordFileUpsertOutcomes folds a successfully upserted batch into the Write's
// tally and emits the counters. It runs only after the upsert statement
// succeeded.
func (w ContentWriter) recordFileUpsertOutcomes(ctx context.Context, obs *fileUpsertObservation, batch []preparedFileRow, outcomes []fileOutcome) {
	if obs == nil || outcomes == nil {
		return
	}
	var batchTally fileUpsertTally
	for i, row := range batch {
		batchTally.rows[outcomes[i]]++
		batchTally.bodyBytes[outcomes[i]] += int64(len(row.body))
	}
	obs.tally.add(batchTally)
	obs.tally.classifiedBatches++

	if w.instruments == nil {
		return
	}
	for outcome := fileOutcome(0); outcome < fileOutcomeCount; outcome++ {
		if batchTally.rows[outcome] == 0 {
			continue
		}
		if w.instruments.ContentFileUpserts != nil {
			w.instruments.ContentFileUpserts.Add(ctx, int64(batchTally.rows[outcome]), metric.WithAttributes(
				telemetry.AttrOutcome(outcome.String()), telemetry.AttrSnapshot(obs.snapshot)))
		}
		if w.instruments.ContentFileUpsertBodyBytes != nil {
			w.instruments.ContentFileUpsertBodyBytes.Add(ctx, batchTally.bodyBytes[outcome], metric.WithAttributes(
				telemetry.AttrOutcome(outcome.String())))
		}
	}
}

// stageAttrs returns the upsert_files stage-log fields for the Write's tally.
// classified_batch_count and unclassified_batch_count tell an uncounted Write
// (no observers, or a failed pre-read) from one with zero rows of an outcome.
func (o *fileUpsertObservation) stageAttrs() []any {
	if o == nil {
		return nil
	}
	return []any{
		"snapshot", o.snapshot,
		"inserted_count", o.tally.rows[outcomeInserted],
		"unchanged_skipped_count", o.tally.rows[outcomeUnchangedSkipped],
		"metadata_only_count", o.tally.rows[outcomeMetadataOnly],
		"content_changed_count", o.tally.rows[outcomeContentChanged],
		"metadata_only_body_bytes", o.tally.bodyBytes[outcomeMetadataOnly],
		"classified_batch_count", o.tally.classifiedBatches,
		"unclassified_batch_count", o.tally.unclassifiedBatches,
		"classify_seconds", o.tally.classifySeconds,
	}
}
