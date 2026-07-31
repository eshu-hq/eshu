// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// reducerFactBatchInsertConflictGuard is the FROZEN normalized tail of
// reducerFactBatchInsertQuery: its whole ON CONFLICT clause, including the
// `fencing_token <= EXCLUDED.fencing_token` guard.
//
// The guard is what makes the token mean anything. Without it a pass that read
// STALE evidence overwrites a fresher pass's payload on the same fact_id — this
// domain's current identity embeds only (scope, generation, image_ref), while
// source_revision, source_revision_provenance, build_provenance_repository_ids
// evidence_fact_ids, and outcome are payload-only and are filled in by
// classification and cross-scope
// enrichment whose visibility depends on which generations were active at load
// time. Two passes therefore collide on one row even when either classification
// or enrichment differs, and the poorer one must lose.
//
// The clause is frozen in FULL rather than probed by substring. A substring
// check for `fencing_token` survives every rewrite that matters: replacing the
// guard with `fencing_token = GREATEST(fact_records.fencing_token,
// EXCLUDED.fencing_token)` protects the token while letting the stale content
// through, which is worse than no fence at all, and it still contains the
// fragment. Freezing the tail also pins `<=` rather than `<`: the watermark is
// the evidence-read time and a pass reads its evidence once, so every retry,
// redelivery, and second chunk of the same pass carries the IDENTICAL token,
// and `<` would silently discard all of them while reporting success.
//
// Adding a column to the insert is expected to fail this test. That is the
// point: the conflict SET is the column contract, and a new column that is
// written on insert but not on conflict leaves an upserted row carrying the old
// value.
const reducerFactBatchInsertConflictGuard = "ON CONFLICT (fact_id) DO UPDATE SET " +
	"fact_kind = EXCLUDED.fact_kind, " +
	"stable_fact_key = EXCLUDED.stable_fact_key, " +
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

// normalizeReducerSQL collapses all runs of ASCII whitespace to single spaces
// and trims the ends, so a frozen statement fragment can stay readable while
// still comparing byte-for-byte against the indented production constant.
//
// ASCII whitespace ONLY, deliberately. strings.Fields splits on unicode.IsSpace,
// which counts U+00A0 NO-BREAK SPACE and the rest of the Unicode space class as
// whitespace — but PostgreSQL does not, and rejects a statement containing one.
// Normalizing those away would let a byte Postgres refuses pass a byte-exact
// comparison: with `0xC2 0xA0` injected into reducerFactBatchInsertQuery's
// `ON CONFLICT`, a strings.Fields normalizer erases it again and the statement
// still compares equal to the frozen guard above. Splitting on ASCII space alone
// leaves the NO-BREAK SPACE inside a field, so it survives into the joined
// output and the comparison reddens.
// TestReducerSQLNormalizerKeepsNonASCIIWhitespace holds that property against
// the shipped statement.
func normalizeReducerSQL(query string) string {
	return strings.Join(strings.FieldsFunc(query, isASCIISQLWhitespace), " ")
}

// isASCIISQLWhitespace reports whether r is one of the six ASCII characters
// PostgreSQL itself treats as whitespace between tokens. Every other rune,
// including U+00A0 and the rest of the Unicode space class, is content.
func isASCIISQLWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

// TestReducerFactBatchInsertFreezesItsConflictGuard holds the line on the one
// clause seven reducer writers share.
//
// The guard is asserted here rather than only against real Postgres because the
// live proofs (TestReducerFactBatchInsertRejectsStaleContentUpsertLive and its
// two siblings in go/internal/storage/postgres) are DSN-gated and skip in a
// credential-free lane, while this runs on every `go test ./internal/reducer`.
func TestReducerFactBatchInsertFreezesItsConflictGuard(t *testing.T) {
	t.Parallel()

	normalized := normalizeReducerSQL(reducerFactBatchInsertQuery)
	if !strings.HasSuffix(normalized, reducerFactBatchInsertConflictGuard) {
		t.Fatalf(
			"the batched insert's ON CONFLICT clause changed.\n got: %s\nwant suffix: %s\n"+
				"Weakening this clause — dropping the WHERE guard, or raising only the token with "+
				"GREATEST while assigning content unconditionally — lets a pass that read stale "+
				"evidence overwrite a fresher pass's payload while the row keeps advertising the "+
				"fresher watermark.",
			normalized, reducerFactBatchInsertConflictGuard,
		)
	}
}

// TestReducerSQLNormalizerKeepsNonASCIIWhitespace is the regression for the
// frozen-text guard normalizing away a byte PostgreSQL rejects.
//
// strings.Fields would treat U+00A0 as whitespace and erase it, so a statement
// carrying one would compare equal to the frozen text and ship a guard the
// database refuses to parse. The statement would fail loudly at runtime rather
// than corrupt anything, so this was never a silent-correctness hole in the
// writer — it was a hole in the guard the frozen comparison delegates to, and a
// frozen-text comparison that normalizes away a byte the database refuses is not
// comparing the text that actually ships.
func TestReducerSQLNormalizerKeepsNonASCIIWhitespace(t *testing.T) {
	t.Parallel()

	const noBreakSpace = "\u00a0"
	injected := strings.Replace(
		reducerFactBatchInsertQuery,
		"ON CONFLICT",
		"ON"+noBreakSpace+"CONFLICT",
		1,
	)
	if injected == reducerFactBatchInsertQuery {
		t.Fatalf("injection rewrote nothing; the probe no longer probes the shipped statement")
	}

	got := normalizeReducerSQL(injected)
	if strings.HasSuffix(got, reducerFactBatchInsertConflictGuard) {
		t.Fatalf(
			"normalizeReducerSQL erased a U+00A0, making a statement PostgreSQL rejects "+
				"compare equal to the frozen conflict guard: %q",
			got,
		)
	}
	if !strings.Contains(got, noBreakSpace) {
		t.Fatalf("normalizeReducerSQL dropped the U+00A0 instead of preserving it: %q", got)
	}
}
