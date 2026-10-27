// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Two-statement content_files writer prototype (#8950).
//
// An unchanged-content rewrite under a new generation rewrites every row
// through the guarded upsert: SET content forces a new TOAST value and new
// entries in both trigram GIN indexes, so the update is never HOT. This file
// splits the batch instead: rows the pre-read classified as metadata_only go
// through a metadata-only UPDATE that sets only commit_sha and generation_id,
// leaving content (and its generated content_lower copy) and indexed_at alone,
// and every other row keeps the guarded upsert.
//
// The UPDATE rechecks the full row-equality twin of the guard in SQL (content
// text included, not the digest-and-size proxy), plus the lineage-differ
// predicate, so it updates exactly the rows the guarded upsert would rewrite
// with only (commit_sha, generation_id[, indexed_at]) changed. A row it does
// not update — a concurrent content edit between the pre-read and the UPDATE,
// a proxy false positive, a skipped or inserted row — falls through to the
// guarded upsert, which keeps last-writer-wins semantics. RETURNING reports
// the updated keys, so the fall-through set is exact, not re-derived.
//
// indexed_at is deliberately not moved: it sits in the language covering
// index's INCLUDE, and moving it costs the HOT update. A metadata-only row
// keeps its old indexed_at, which on these converted paths (classified
// batches, plus the single-row ContentStore twin) means "when this row was
// last written with a change beyond commit_sha/generation_id lineage"
// (#8950, replacing the #8915 wording rather than extending it for those
// paths: a commit/generation-only rewrite is no longer a content write for
// indexed_at purposes). The language rollup's MAX(indexed_at) therefore no
// longer advances on a pure replay through these paths; unobserved writers
// and unclassified batches still take the single-statement upsert, which
// moves indexed_at as before. That scoped semantic change rolled out under
// the arbiter ruling for #8950, recorded in the evidence note.
// The statement-level triggers still fire (with an
// empty transition table when every row falls through): the secret-lines
// derive diffs old against new content and derives nothing for identical
// rows, and the config-artifact version fence stages every repository the
// UPDATE touched.
//
// The path engages only when the batch was classified (outcomes != nil). An
// unobserved writer and every unclassified batch keep the single-statement
// upsert byte-identical to before.

// metadataOnlyContentFileUpdatePrefix and metadataOnlyContentFileUpdateSuffix
// bracket the per-row VALUES list assembled by
// updateMetadataOnlyContentFileBatch. The VALUES columns are the join keys,
// the assigned lineage, and the recheck twin of the guard.
const metadataOnlyContentFileUpdatePrefix = `UPDATE content_files AS c SET commit_sha = v.commit_sha, generation_id = v.generation_id
FROM (VALUES `

const metadataOnlyContentFileUpdateSuffix = `) AS v(repo_id, relative_path, commit_sha, generation_id, content_hash, body_len, body, line_count, language, artifact_type, template_dialect, iac_relevant)
WHERE c.repo_id = v.repo_id AND c.relative_path = v.relative_path
  AND c.content_hash = v.content_hash
  AND octet_length(c.content) = v.body_len
  AND c.content IS NOT DISTINCT FROM v.body
  AND c.line_count IS NOT DISTINCT FROM v.line_count
  AND c.language IS NOT DISTINCT FROM v.language
  AND c.artifact_type IS NOT DISTINCT FROM v.artifact_type
  AND c.template_dialect IS NOT DISTINCT FROM v.template_dialect
  AND c.iac_relevant IS NOT DISTINCT FROM v.iac_relevant
  AND (c.commit_sha IS DISTINCT FROM v.commit_sha OR c.generation_id IS DISTINCT FROM v.generation_id)
RETURNING c.repo_id, c.relative_path`

// columnsPerMetadataOnlyUpdate is the VALUES width of one metadata-only row.
const columnsPerMetadataOnlyUpdate = 12

// metadataOnlyValueTypes is the SQL type of each VALUES column, in order:
// untyped multi-row VALUES parameters default to text, which would break the
// body-size and line-count comparisons, so every placeholder carries its
// cast. The order matches the v(...) column list in the suffix.
var metadataOnlyValueTypes = [columnsPerMetadataOnlyUpdate]string{
	"text", "text", "text", "text", "text", "bigint",
	"text", "integer", "text", "text", "text", "boolean",
}

// metadataOnlyFileKey is the (repo_id, relative_path) identity of an updated row.
type metadataOnlyFileKey struct{ repoID, path string }

// Conflict discipline for the two-statement batch.
//
// The UPDATE and the guarded upsert take row locks over snapshot-dependent
// subsets of the batch, so concurrent writers for the same keys can deadlock
// (40P01) where the single statement never did: one session's UPDATE can wait
// on another's upsert while that upsert waits on the first session's UPDATE.
// The waits always resolve by aborting one side; the batch is idempotent,
// and each attempt re-reads the stored rows, so a retry converges on the
// winner's snapshot — and usually on a smaller fall-through, since an aborted
// attempt's committed UPDATE narrows the next attempt's work. Retry a bounded
// number of times with jittered backoff, mirroring the projector claim's
// conflict discipline; when every attempt conflicts the Write fails and the
// projector retries the generation.
//
// Eight attempts, not the claim's three: the claim takes every lock SKIP
// LOCKED and almost never conflicts, while the 6-writer adversarial proof
// deadlocks roughly every second batch. Backoff grows per attempt with jitter,
// so simultaneous losers spread while the winners drain.
const (
	contentFileWriteConflictAttempts       = 8
	defaultContentFileWriteConflictBackoff = 25 * time.Millisecond
)

// contentFileWriteConflictClass returns the failure class for a retryable
// write conflict, or "" when err is nil or not a deadlock/serialization
// failure.
func contentFileWriteConflictClass(err error) string {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) {
		return ""
	}
	switch pgErr.Code {
	case "40P01":
		return "deadlock"
	case "40001":
		return "serialization_failure"
	default:
		return ""
	}
}

// contentFileWriteConflictDelay returns attempt*base plus jitter in [0, base),
// mirroring the claim conflict delay.
func contentFileWriteConflictDelay(attempt int) time.Duration {
	base := defaultContentFileWriteConflictBackoff
	jitter := time.Duration(queuestore.DefaultJitterSource() * float64(base))
	return time.Duration(attempt)*base + jitter
}

// retryContentFileWriteConflicts runs one batch's classified write unit —
// pre-read, metadata UPDATE and guarded upsert — and retries it when Postgres
// reports a deadlock or serialization failure. The outcome counters are
// recorded once, for the attempt that succeeds.
func (w ContentWriter) retryContentFileWriteConflicts(ctx context.Context, batch []preparedFileRow, write func() error) error {
	for attempt := 1; ; attempt++ {
		err := write()
		class := contentFileWriteConflictClass(err)
		if class == "" {
			return err
		}
		if attempt >= contentFileWriteConflictAttempts {
			return fmt.Errorf("content_files batch (%d files) conflicts after %d attempts: %w", len(batch), attempt, err)
		}
		if w.instruments != nil && w.instruments.ContentFileWriteConflictRetries != nil {
			w.instruments.ContentFileWriteConflictRetries.Add(ctx, 1, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionFailureClass, class),
			))
		}
		if w.Logger != nil {
			w.Logger.WarnContext(ctx, "content_files batch write conflict; retrying",
				"event_name", "content_file_write.conflict_retry",
				"batch_file_count", len(batch), "attempt", attempt, "conflict_class", class, "error", err)
		}
		if waitErr := sleepContext(ctx, contentFileWriteConflictDelay(attempt)); waitErr != nil {
			return fmt.Errorf("content_files batch (%d files): %w", len(batch), waitErr)
		}
	}
}

// classifyAndWriteFileBatch is one attempt of a batch's classified write: the
// pre-read, the metadata-only UPDATE for metadata-classified rows, and the
// guarded upsert for everything the UPDATE did not touch (including the rows
// the UPDATE missed under a concurrent edit). On success it folds the
// pre-read cost into the tally; a failed attempt leaves the tally alone so a
// retry does not count the pre-read twice.
func (w ContentWriter) classifyAndWriteFileBatch(ctx context.Context, obs *fileUpsertObservation, batch []preparedFileRow, indexedAt time.Time, tag any) ([]fileOutcome, string, error) {
	outcomes, unclassifiedReason, classifySeconds := w.preClassify(ctx, obs, batch, tag)

	upsertBatch := batch
	if meta, rest := partitionMetadataOnlyBatch(batch, outcomes); len(meta) > 0 {
		updated, err := w.updateMetadataOnlyContentFileBatch(ctx, meta, tag)
		if err != nil {
			return nil, "", err
		}
		upsertBatch = rest
		for _, row := range meta {
			if _, ok := updated[metadataOnlyFileKey{repoID: row.repoID, path: row.path}]; !ok {
				upsertBatch = append(upsertBatch, row)
			}
		}
	}

	if len(upsertBatch) > 0 {
		args := make([]any, 0, len(upsertBatch)*columnsPerContentFile)
		var values strings.Builder
		for i, row := range upsertBatch {
			if i > 0 {
				values.WriteString(", ")
			}
			offset := i * columnsPerContentFile
			fmt.Fprintf(
				&values,
				"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				offset+1, offset+2, offset+3, offset+4, offset+5,
				offset+6, offset+7, offset+8, offset+9, offset+10,
				offset+11, offset+12,
			)
			args = append(
				args,
				row.repoID,
				row.path,
				row.commitSHA,
				row.body,
				row.contentHash,
				row.lineCount,
				row.language,
				row.artifactType,
				row.templateDialect,
				row.iacRelevant,
				indexedAt,
				tag,
			)
		}
		query := upsertContentFileBatchPrefix + values.String() + upsertContentFileBatchSuffix
		if _, err := w.database.ExecContext(ctx, query, args...); err != nil {
			return nil, "", fmt.Errorf("upsert content_files batch (%d files): %w", len(upsertBatch), err)
		}
	}
	if obs != nil {
		obs.tally.classifySeconds += classifySeconds
	}
	return outcomes, unclassifiedReason, nil
}

// partitionMetadataOnlyBatch splits a classified batch into the metadata-only
// rows for the UPDATE and the rest for the guarded upsert. An unclassified
// batch (outcomes nil, or a length mismatch that must never happen) returns
// the whole batch as rest, so the writer keeps the legacy single statement.
func partitionMetadataOnlyBatch(batch []preparedFileRow, outcomes []fileOutcome) (meta, rest []preparedFileRow) {
	if len(outcomes) != len(batch) {
		return nil, batch
	}
	for i, row := range batch {
		if outcomes[i] == outcomeMetadataOnly {
			meta = append(meta, row)
			continue
		}
		rest = append(rest, row)
	}
	return meta, rest
}

// updateMetadataOnlyContentFileBatch runs the metadata-only UPDATE for rows
// the pre-read classified as metadata_only and reports the keys it updated.
// A row whose stored version moved under the pre-read (content or any guarded
// column differs, or the lineage no longer differs) is left untouched for the
// guarded upsert. An empty batch issues no statement.
func (w ContentWriter) updateMetadataOnlyContentFileBatch(ctx context.Context, batch []preparedFileRow, tag any) (map[metadataOnlyFileKey]struct{}, error) {
	updated := map[metadataOnlyFileKey]struct{}{}
	if len(batch) == 0 {
		return updated, nil
	}

	args := make([]any, 0, len(batch)*columnsPerMetadataOnlyUpdate)
	var values strings.Builder
	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		values.WriteString("(")
		for c := 0; c < columnsPerMetadataOnlyUpdate; c++ {
			if c > 0 {
				values.WriteString(", ")
			}
			fmt.Fprintf(&values, "$%d::%s", i*columnsPerMetadataOnlyUpdate+c+1, metadataOnlyValueTypes[c])
		}
		values.WriteString(")")
		args = append(
			args,
			row.repoID,
			row.path,
			row.commitSHA,
			tag,
			row.contentHash,
			len(row.body),
			row.body,
			row.lineCount,
			row.language,
			row.artifactType,
			row.templateDialect,
			row.iacRelevant,
		)
	}

	query := metadataOnlyContentFileUpdatePrefix + values.String() + metadataOnlyContentFileUpdateSuffix
	rows, err := w.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update metadata-only content_files batch (%d files): %w", len(batch), err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key metadataOnlyFileKey
		if err := rows.Scan(&key.repoID, &key.path); err != nil {
			return nil, fmt.Errorf("scan updated metadata-only content_files keys: %w", err)
		}
		updated[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read updated metadata-only content_files keys: %w", err)
	}
	return updated, nil
}
