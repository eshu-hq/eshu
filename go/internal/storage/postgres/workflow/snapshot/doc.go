// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package snapshot encodes and decodes the stored coordinator_snapshot status
// summary row (#8828).
//
// The status snapshot's coordinator section (collector instances, workflow run,
// work-item and completeness status counts, claims, collector backpressure, the
// recent-failure counts, and the collector terminal failures) costs time linear
// in workflow_work_items on every status read and every hosted /metrics scrape.
// The reducer's status summary writer runs that read once per interval and
// stores the result as one bounded row in status_summary_snapshots; the status
// store reads the row instead.
//
// Encode turns the snapshot the live read returns into [section, ordinal, json]
// entries and Decode turns the entries back into the same snapshot, strictly:
// an unknown section or field, a bad timestamp, trailing data, or a missing or
// repeated claims entry is an error, so a reader falls back instead of serving
// a row it only half understands. AdvanceAges moves the clock-relative
// durations from the row's as_of to the read; Clone copies a snapshot that
// concurrent readers share. SourceSHA256 digests the statement texts and the
// encoding, so a row computed by another version is never decoded.
//
// The package depends only on the status types and the summary row types. It
// never imports the parent postgres package, which supplies the statements and
// the digest inputs.
package snapshot
