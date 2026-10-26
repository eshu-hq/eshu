// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Closed outcome set of eshu_dp_content_file_upserts_total (#8950). The
// postgres ContentWriter classifies each content_files row of an upsert batch
// against the stored row it found before the write:
//
//   - inserted: no stored row for (repo_id, relative_path).
//   - unchanged_skipped: every compared column and the content digest match, so
//     the upsert guard (#8915) skips the row.
//   - metadata_only: the content digest matches and only commit_sha and/or
//     generation_id differ, so the row is rewritten with unchanged content
//     (the #8950 candidate for a HOT metadata-only update).
//   - content_changed: the row is rewritten for any other reason (digest or
//     size changed, or another stored column changed).
//
// The measure-first classification compares the caller-supplied content
// digest and the body size, not the text, so it is a proxy for body equality.
const (
	ContentFileUpsertOutcomeInserted         = "inserted"
	ContentFileUpsertOutcomeUnchangedSkipped = "unchanged_skipped"
	ContentFileUpsertOutcomeMetadataOnly     = "metadata_only"
	ContentFileUpsertOutcomeContentChanged   = "content_changed"
)

// Closed snapshot set of eshu_dp_content_file_upserts_total: full when the
// Write carried the repository's complete file set, delta otherwise.
const (
	ContentFileSnapshotFull  = "full"
	ContentFileSnapshotDelta = "delta"
)

// Closed reason set of eshu_dp_content_file_upsert_unclassified_rows_total
// (#8950): the rows of upsert batches that were written but not classified.
// sampled_out means the writer skipped the pre-read for a small batch to keep
// its fixed cost off the write path; classify_failed means the pre-read failed.
const (
	ContentFileUnclassifiedSampledOut     = "sampled_out"
	ContentFileUnclassifiedClassifyFailed = "classify_failed"
)

// Closed batch-class set of the content_files upsert counters (#8950). The
// ContentWriter classifies every batch of at least the sampling minimum (100
// rows) and one smaller batch in eight, so a share computed over the
// classified rows weights small-batch rows at one eighth. The batch_class label
// lets a query weight them back: large is a batch at or above the minimum
// (always classified), small is a batch below it (sampled).
const (
	ContentFileBatchSmall = "small"
	ContentFileBatchLarge = "large"
)

// MetricDimensionBatchClass labels the content_files upsert counters with the
// size class of the batch a row was written in (#8950). The value set is closed:
// small or large.
const MetricDimensionBatchClass = "batch_class"

// AttrBatchClass returns a batch_class attribute. v must be ContentFileBatchSmall
// or ContentFileBatchLarge.
func AttrBatchClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionBatchClass, v)
}

// MetricDimensionSnapshot labels eshu_dp_content_file_upserts_total with the
// snapshot shape of the Write (#8950). The value set is closed: full or delta.
const MetricDimensionSnapshot = "snapshot"

// AttrSnapshot returns a snapshot attribute. v must be ContentFileSnapshotFull
// or ContentFileSnapshotDelta.
func AttrSnapshot(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSnapshot, v)
}

// registerContentStorePolicyInstruments registers the content-store policy
// counter (#8812). One add per (policy, redactor, outcome) per collected fact
// stream, where policy is metadata or redact, redactor is the redact mode
// (marker or skeleton, #8833) or none, and outcome is metadata (body withheld),
// redacted (at least one value replaced), clean (nothing to replace), or
// redactor_failed (the redactor errored or the body was not UTF-8, so the file
// became a metadata row; never the raw body). A rising redactor_failed count
// means a credential-bearing file is silently losing its body.
//
// It also registers the four content_files upsert outcome counters
// ContentFileUpserts, ContentFileUpsertBodyBytes, ContentFileUpsertUnclassifiedRows
// and ContentFileUpsertUnclassifiedBodyBytes (#8950), which the postgres
// ContentWriter emits; they are unrelated to the policy counter and sit here
// only to avoid a new file in this directory.
func registerContentStorePolicyInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ContentStorePolicyFiles, err = meter.Int64Counter(
		"eshu_dp_collector_content_store_files_total",
		metric.WithDescription("Files the git collector applied a non-store content-store policy to, by policy (metadata, redact), redactor (marker, skeleton, none) and outcome (metadata, redacted, clean, redactor_failed) (#8812, #8833)"),
	); err != nil {
		return fmt.Errorf("register ContentStorePolicyFiles counter: %w", err)
	}

	if inst.ContentFileUpserts, err = meter.Int64Counter(
		"eshu_dp_content_file_upserts_total",
		metric.WithDescription("content_files rows per ContentWriter upsert batch by outcome (inserted, unchanged_skipped, metadata_only, content_changed), snapshot (full, delta) and batch_class (small, large; small batches are classified one in eight, so weight them back with the unclassified counters); outcome is classified by a content-free pre-read that compares the caller-supplied content digest, so it is a proxy for body equality (#8950)"),
		metric.WithUnit("{file}"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpserts counter: %w", err)
	}
	if inst.ContentFileUpsertUnclassifiedRows, err = meter.Int64Counter(
		"eshu_dp_content_file_upsert_unclassified_rows_total",
		metric.WithDescription("content_files rows of ContentWriter upsert batches that were written but not classified, by reason (sampled_out, classify_failed), snapshot (full, delta) and batch_class (small, large); eshu_dp_content_file_upserts_total plus this counter is every row written (#8950)"),
		metric.WithUnit("{file}"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpsertUnclassifiedRows counter: %w", err)
	}
	if inst.ContentFileUpsertUnclassifiedBodyBytes, err = meter.Int64Counter(
		"eshu_dp_content_file_upsert_unclassified_body_bytes_total",
		metric.WithDescription("Body bytes of the content_files rows counted by eshu_dp_content_file_upsert_unclassified_rows_total, by reason (sampled_out, classify_failed), snapshot (full, delta) and batch_class (small, large); with eshu_dp_content_file_upsert_body_bytes_total it accounts for every body byte written (#8950)"),
		metric.WithUnit("By"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpsertUnclassifiedBodyBytes counter: %w", err)
	}
	if inst.ContentFileUpsertBodyBytes, err = meter.Int64Counter(
		"eshu_dp_content_file_upsert_body_bytes_total",
		metric.WithDescription("Body bytes of the classified content_files rows per ContentWriter upsert batch by outcome (inserted, unchanged_skipped, metadata_only, content_changed), snapshot (full, delta) and batch_class (small, large); the bytes the writer was handed, not the bytes written (#8950)"),
		metric.WithUnit("By"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpsertBodyBytes counter: %w", err)
	}
	return nil
}

// ContentPolicyRawRowsObserver reports the detector-visible raw credential
// lines still stored on paths a content-store policy governs (#8833).
type ContentPolicyRawRowsObserver interface {
	// ContentPolicyRawRows returns the count of unsuppressed hardcoded-secret
	// lines on governed paths that carry no REDACTED_<finding_kind> marker
	// and are not value-free HCL block openers.
	ContentPolicyRawRows(ctx context.Context) (int64, error)
}

// RegisterContentPolicyRawRowsObservableGauge registers
// eshu_dp_content_policy_raw_rows (#8833), one input to the #8813 cleanup
// runbook's exit signal: zero means no governed row keeps a detector-visible
// raw line (an unsuppressed finding with no REDACTED_<finding_kind> marker
// that is not a value-free HCL block opener). A non-zero value means raw
// credential lines are still in content_files on a governed path: a
// repository the reindex has not reached, or an ingester still on an old
// binary. The runbook's census raw_files column, not this gauge, proves every
// governed file was stored again under its policy. A nil observer registers
// nothing.
func RegisterContentPolicyRawRowsObservableGauge(inst *Instruments, meter metric.Meter, observer ContentPolicyRawRowsObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}
	var err error
	inst.ContentPolicyRawRows, err = meter.Int64ObservableGauge(
		"eshu_dp_content_policy_raw_rows",
		metric.WithDescription("Unsuppressed hardcoded-secret lines on content-store governed paths that carry no REDACTED_<finding_kind> marker and are not value-free HCL block openers: zero means no detector-visible raw line remains on a governed path; the cleanup runbook census raw_files column is the re-storage proof (#8833)"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			count, err := observer.ContentPolicyRawRows(ctx)
			if err != nil {
				return fmt.Errorf("observe content policy raw rows: %w", err)
			}
			o.Observe(count)
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register ContentPolicyRawRows gauge: %w", err)
	}
	return nil
}
