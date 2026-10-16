// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"

	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

// canonicalGenerationSQL collapses whitespace and drops the table aliases the
// copies use, so equal definitions compare equal across statements.
func canonicalGenerationSQL(sql string) string {
	return strings.Join(strings.Fields(generationAliasPrefix.ReplaceAllString(sql, "")), " ")
}

// generationAliasPrefix matches the alias qualifiers the copies put on
// scope_generations columns. The word boundary keeps full_generation. whole.
var generationAliasPrefix = regexp.MustCompile(`\b(?:full_generation|generation|ranked|pending)\.`)

// sqlSpan returns the text between the only occurrence of start and the next
// end.
func sqlSpan(t *testing.T, name, sql, start, end string) string {
	t.Helper()
	if strings.Count(sql, start) != 1 {
		t.Fatalf("%s: %q occurs %d times, want 1", name, start, strings.Count(sql, start))
	}
	rest := sql[strings.Index(sql, start)+len(start):]
	i := strings.Index(rest, end)
	if i < 0 {
		t.Fatalf("%s: %q not found after %q", name, end, start)
	}
	return rest[:i]
}

// newestActivatedFullSelector is the one definition of "the scope's newest
// activated full generation" every copy must use: a non-delta generation with
// activated_at set, newest by ingested_at then generation_id, read for its
// write-start marker.
var newestActivatedFullSelector = regexp.MustCompile(`^SELECT (DISTINCT ON \(scope_id\) scope_id, )?projection_write_started_at AS \w+ ` +
	`FROM scope_generations( AS \w+)? WHERE (scope_id = \S+ AND )?is_delta = false AND activated_at IS NOT NULL ` +
	`ORDER BY (scope_id, )?ingested_at DESC, generation_id DESC( LIMIT 1)?$`)

// uncoveredWriterComparison is the one definition of an uncovered writer: a
// generation that started writing, never activated, and started after the
// newest activated full's write start, with a full lacking a marker comparing
// as -infinity.
var uncoveredWriterComparison = regexp.MustCompile(`projection_write_started_at IS NOT NULL AND activated_at IS NULL ` +
	`(AND status IN \('superseded', 'failed'\) )?AND projection_write_started_at > ` +
	`COALESCE\(\(?(SELECT pws FROM last_full|last_full\.full_write_started_at)\)?, '-infinity'::timestamptz\)`)

// TestUncoveredWriterDefinitionsAgree is the drift guard for the four copies
// of the uncovered-writer definition (#8243): the reconcile probe
// (uncoveredProjectionWritersQuery), the two generation-retention statements,
// and the producer manifest reads' manifest-less leg. Each must select the
// newest activated full the same way, the first three must flag a writer with
// the same comparison, and the manifest-less leg must skip exactly the
// complement: a superseded or failed generation whose write start, with NULL
// as -infinity, is at or before that full's write start, and nothing when the
// full has no marker. Changing one copy alone fails this test.
func TestUncoveredWriterDefinitionsAgree(t *testing.T) {
	t.Parallel()

	selectors := map[string]string{
		"uncoveredProjectionWritersQuery": sqlSpan(t, "probe", uncoveredProjectionWritersQuery, "WITH last_full AS (", ")\nSELECT generation.generation_id"),
		"generationRetentionCandidateQuery": sqlSpan(t, "retention", generationRetentionCandidateQuery,
			"last_activated_full_write AS MATERIALIZED (", "\n),\n"),
		"generationRetentionTargetedCandidateQuery": sqlSpan(t, "targeted retention", generationRetentionTargetedCandidateQuery,
			"WITH last_activated_full_write AS MATERIALIZED (", "\n),\n"),
		"producerstore.ManifestLessLeg": sqlSpan(t, "manifest-less leg", producerstore.ManifestLessLeg, "LEFT JOIN LATERAL (", ") AS last_full ON true"),
	}
	for name, selector := range selectors {
		if canonical := canonicalGenerationSQL(selector); !newestActivatedFullSelector.MatchString(canonical) {
			t.Errorf("%s selects the newest activated full differently:\n%s", name, canonical)
		}
	}

	for name, sql := range map[string]string{
		"uncoveredProjectionWritersQuery":           uncoveredProjectionWritersQuery,
		"generationRetentionCandidateQuery":         generationRetentionCandidateQuery,
		"generationRetentionTargetedCandidateQuery": generationRetentionTargetedCandidateQuery,
	} {
		if canonical := canonicalGenerationSQL(sql); !uncoveredWriterComparison.MatchString(canonical) {
			t.Errorf("%s no longer flags an uncovered writer with the shared comparison", name)
		}
	}

	const covered = "AND activated_at IS NULL AND NOT ( status IN ('superseded', 'failed') " +
		"AND COALESCE(COALESCE(projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws, false) ) )"
	if !strings.Contains(canonicalGenerationSQL(producerstore.ManifestLessLeg), covered) {
		t.Errorf("producerstore.ManifestLessLeg no longer skips exactly the covered writers:\n%s", canonicalGenerationSQL(producerstore.ManifestLessLeg))
	}
}

// TestManifestLessLegLastFullIsTheProbeLastFull pins the manifest-less leg's
// newest-full lookup to the reconcile probe byte for byte (#8243): the LATERAL
// body is uncoveredProjectionWritersQuery's last_full CTE with $1 bound to the
// outer scope, and the leg exempts exactly the rows the probe's comparison
// does not flag, on the same column with the same -infinity default. The
// pattern follows TestSupersedeRunningGateProbeIsPrefixOfShippedQuery: derive
// from the shipped constant, never from a hand copy.
func TestManifestLessLegLastFullIsTheProbeLastFull(t *testing.T) {
	t.Parallel()

	const binding = "WHERE scope_id = $1\n"
	body := sqlSpan(t, "probe", uncoveredProjectionWritersQuery, "WITH last_full AS (\n", "\n)\nSELECT generation.generation_id")
	if strings.Count(body, binding) != 1 {
		t.Fatalf("probe last_full binds the scope %d times, want 1:\n%s", strings.Count(body, binding), body)
	}
	lateral := "LEFT JOIN LATERAL (\n" + strings.Replace(body, binding, "WHERE scope_id = scope.scope_id\n", 1) + "\n) AS last_full ON true\n"
	if got := strings.Count(producerstore.ManifestLessLeg, lateral); got != 1 {
		t.Fatalf("ManifestLessLeg carries the probe's last_full %d times, want 1; want:\n%s", got, lateral)
	}

	for _, pin := range []struct{ name, sql, want string }{
		{"probe status", uncoveredProjectionWritersQuery, "  AND generation.status IN ('superseded', 'failed')\n"},
		{"probe comparison", uncoveredProjectionWritersQuery, "  AND generation.projection_write_started_at >\n      COALESCE((SELECT pws FROM last_full), '-infinity'::timestamptz)\n"},
		{"leg status", producerstore.ManifestLessLeg, "        pending.status IN ('superseded', 'failed')\n"},
		{"leg comparison", producerstore.ManifestLessLeg, "        AND COALESCE(COALESCE(pending.projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws, false)\n"},
	} {
		if got := strings.Count(pin.sql, pin.want); got != 1 {
			t.Errorf("%s: %q occurs %d times, want 1", pin.name, pin.want, got)
		}
	}
}

// TestManifestLessLegDerivesThePreviousRule pins that the previous rule is the
// shipped leg minus exactly the two added pieces, and that the shipped read
// still keeps the previous never-activated predicate verbatim.
func TestManifestLessLegDerivesThePreviousRule(t *testing.T) {
	previous := previousManifestLessLeg(t)
	const signal = "    FROM scope_generations AS pending\n" +
		"    WHERE pending.scope_id = scope.scope_id\n" +
		"      AND pending.activated_at IS NULL\n" +
		"  )\n"
	if !strings.HasSuffix(previous, signal) {
		t.Fatalf("previous leg does not end with the never-activated signal:\n%s", previous)
	}
	if strings.Contains(previous, "last_full") || strings.Contains(previous, "projection_write_started_at") {
		t.Fatalf("previous leg still reads the write-start marker:\n%s", previous)
	}
}
