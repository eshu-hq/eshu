// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// reducerFactBatchInsertVersionedConflictGuard is the FROZEN normalized tail of
// reducerFactBatchInsertVersionedQuery (#5848): its whole ON CONFLICT clause,
// including the `fencing_token <= EXCLUDED.fencing_token` guard.
//
// This mirrors reducerFactBatchInsertConflictGuard for the versioned family.
// aws_cloud_runtime_drift is the first versioned writer to opt in: its
// identity embeds finding_kind, so a stale pass and a fresh pass that
// disagree on classification never collide on this guard at all (they mint
// different fact_ids) — but a RETRY or REDELIVERY of the SAME pass, or two
// passes that happen to agree on classification, DO collide on one fact_id,
// and this guard is what stops the poorer one from winning.
//
// Frozen in FULL rather than probed by substring for the same reason as the
// unversioned guard: a substring check for "fencing_token" survives a rewrite
// to `fencing_token = GREATEST(...)` with the content columns assigned
// unconditionally, which protects the token and nothing else.
const reducerFactBatchInsertVersionedConflictGuard = "ON CONFLICT (fact_id) DO UPDATE SET " +
	"fact_kind = EXCLUDED.fact_kind, " +
	"stable_fact_key = EXCLUDED.stable_fact_key, " +
	"schema_version = EXCLUDED.schema_version, " +
	"collector_kind = EXCLUDED.collector_kind, " +
	"source_confidence = EXCLUDED.source_confidence, " +
	"source_system = EXCLUDED.source_system, " +
	"source_fact_key = EXCLUDED.source_fact_key, " +
	"source_uri = EXCLUDED.source_uri, " +
	"source_record_id = EXCLUDED.source_record_id, " +
	"observed_at = EXCLUDED.observed_at, " +
	"ingested_at = EXCLUDED.ingested_at, " +
	"is_tombstone = EXCLUDED.is_tombstone, " +
	"payload = EXCLUDED.payload, " +
	"fencing_token = EXCLUDED.fencing_token " +
	"WHERE fact_records.fencing_token <= EXCLUDED.fencing_token"

// TestReducerFactBatchInsertVersionedFreezesItsConflictGuard holds the line on
// the versioned insert's conflict clause the same way
// TestReducerFactBatchInsertFreezesItsConflictGuard does for the unversioned
// one. Six versioned writers share this statement; only aws_cloud_runtime_drift
// opts a fencing token in today, so the guard must stay correct for it while
// remaining inert (0 <= 0) for the rest.
func TestReducerFactBatchInsertVersionedFreezesItsConflictGuard(t *testing.T) {
	t.Parallel()

	normalized := normalizeReducerSQL(reducerFactBatchInsertVersionedQuery)
	if !strings.HasSuffix(normalized, reducerFactBatchInsertVersionedConflictGuard) {
		t.Fatalf(
			"the versioned batched insert's ON CONFLICT clause changed.\n got: %s\nwant suffix: %s\n"+
				"Weakening this clause lets a pass that read stale evidence overwrite a fresher pass's "+
				"payload while the row keeps advertising the fresher watermark.",
			normalized, reducerFactBatchInsertVersionedConflictGuard,
		)
	}
}
