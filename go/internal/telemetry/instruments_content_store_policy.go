// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
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
// It also registers the content_files upsert outcome counters
// ContentFileUpserts and ContentFileUpsertBodyBytes (#8950), which the postgres
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
		metric.WithDescription("content_files rows per ContentWriter upsert batch by outcome (inserted, unchanged_skipped, metadata_only, content_changed) and snapshot (full, delta); outcome is classified by a content-free pre-read that compares the caller-supplied content digest, so it is a proxy for body equality (#8950)"),
		metric.WithUnit("{file}"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpserts counter: %w", err)
	}
	if inst.ContentFileUpsertUnclassifiedRows, err = meter.Int64Counter(
		"eshu_dp_content_file_upsert_unclassified_rows_total",
		metric.WithDescription("content_files rows of ContentWriter upsert batches that were written but not classified, by reason (sampled_out, classify_failed) and snapshot (full, delta); eshu_dp_content_file_upserts_total plus this counter is every row written (#8950)"),
		metric.WithUnit("{file}"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpsertUnclassifiedRows counter: %w", err)
	}
	if inst.ContentFileUpsertBodyBytes, err = meter.Int64Counter(
		"eshu_dp_content_file_upsert_body_bytes_total",
		metric.WithDescription("Body bytes of the content_files rows per ContentWriter upsert batch by outcome (inserted, unchanged_skipped, metadata_only, content_changed); the bytes the writer was handed, not the bytes written (#8950)"),
		metric.WithUnit("By"),
	); err != nil {
		return fmt.Errorf("register ContentFileUpsertBodyBytes counter: %w", err)
	}
	return nil
}
