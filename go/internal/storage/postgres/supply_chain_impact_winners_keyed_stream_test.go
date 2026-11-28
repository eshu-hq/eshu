// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"
)

// Hermetic pins for the CTE-streaming keyed winners body build (#9805 C5 /
// #9975 C4). The stage-1 narrow temp and the streamed body are derived from
// the shipped statements, never copied: these tests prove the derivation is
// exact, invertible, and preserves every row-source filter. The live
// differential (keyed_stream_live_test.go) proves the derived statements
// emit byte-identical winners.

// reverseWinnersKeyedStreamBody inverts streamWinnersKeyedBody step by step.
// It is written against the shipped body constants, never hand-copied from
// the derivation, so a drift between the two fails here instead of going
// false-green. TestWinnersKeyedStreamBodyReversesToShipped pins the round
// trip byte-for-byte.
func reverseWinnersKeyedStreamBody(streamed string) string {
	return reverseWinnersKeyedStreamBodyFrom(streamed, winnersSupportsTable)
}

// reverseWinnersKeyedStreamBodyFrom inverts a streamed body whose payload
// point lookups read payloadTable: the ruled #9805 v3-full chunks stream
// over fact_records, the keyed tick over the supports table.
func reverseWinnersKeyedStreamBodyFrom(streamed, payloadTable string) string {
	out := streamed
	const streamFromAnchor = `FROM source_winners AS source
LEFT JOIN operator_overrides AS override
  ON override.canonical_key = source.canonical_key`
	streamedFrom := `FROM source_winners AS source
JOIN ` + payloadTable + ` AS source_payload ON source_payload.fact_id = source.fact_id
LEFT JOIN operator_overrides AS override
  ON override.canonical_key = source.canonical_key
LEFT JOIN ` + payloadTable + ` AS override_payload ON override_payload.fact_id = override.fact_id`
	if n := strings.Count(out, streamedFrom); n != 1 {
		panic("reverse stream body: streamed FROM occurs != 1")
	}
	out = strings.Replace(out, streamedFrom, streamFromAnchor, 1)
	out = strings.ReplaceAll(out, "source_payload.payload", "source.payload")
	out = strings.ReplaceAll(out, "override_payload.payload", "override.payload")
	const lineSourceConfidence = "        fact.scope_id,\n        fact.source_confidence,"
	if n := strings.Count(out, lineSourceConfidence); n != 1 {
		panic("reverse stream body: source passthrough anchor occurs != 1")
	}
	out = strings.Replace(out, lineSourceConfidence,
		"        fact.scope_id,\n        fact.payload,\n        fact.source_confidence,", 1)
	const lineOperatorKey = "        fact.fact_id,\n        CONCAT_WS"
	if n := strings.Count(out, lineOperatorKey); n != 1 {
		panic("reverse stream body: operator passthrough anchor occurs != 1")
	}
	out = strings.Replace(out, lineOperatorKey,
		"        fact.fact_id,\n        fact.payload,\n        CONCAT_WS", 1)
	for _, f := range winnersDetoastFields {
		out = strings.ReplaceAll(out, "fact."+f, "d."+f)
	}
	const lateralAnchor = "FROM active_facts AS fact\n"
	if n := strings.Count(out, lateralAnchor); n != 2 {
		panic("reverse stream body: lateral anchor occurs != 2")
	}
	return strings.ReplaceAll(out, lateralAnchor, "FROM active_facts AS fact"+winnersDetoastFactLateral+"\n")
}

// TestWinnersKeyedStreamBodyReversesToShipped proves the streamed body
// differs from the shipped body only by the declared transform: inverting
// the derivation restores the shipped text byte-for-byte.
func TestWinnersKeyedStreamBodyReversesToShipped(t *testing.T) {
	t.Parallel()

	if got := reverseWinnersKeyedStreamBody(winnersKeyedStreamBodySQL); got != supplyChainImpactWinnerBodySQL {
		t.Fatalf("streamed body does not reverse to the shipped body byte-for-byte:\n%s",
			firstDiffLine(got, supplyChainImpactWinnerBodySQL))
	}
}

// firstDiffLine renders the first line where two statements differ, for a
// readable derivation failure.
func firstDiffLine(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d:\n got: %s\nwant: %s", i+1, al[i], bl[i])
		}
	}
	return "line counts differ"
}

// TestWinnersKeyedStreamBodyCarriesTheDeclaredShape pins the streamed
// body's steady-state shape, so a later edit cannot silently widen the
// temp, reintroduce per-field detoasts, or move the payload fetch off the
// fact_id point lookup.
func TestWinnersKeyedStreamBodyCarriesTheDeclaredShape(t *testing.T) {
	t.Parallel()
	body := winnersKeyedStreamBodySQL

	if strings.Contains(body, "jsonb_to_record") {
		t.Error("streamed body extracts fields itself: extraction belongs to the stage-1 temp")
	}
	if strings.Contains(body, "fact.payload") {
		t.Error("streamed body reads fact.payload: the narrow temp carries no payload")
	}
	// "payload.payload" (the fetched rows) contains the substring "d.",
	// so record refs are pinned per field, not by substring.
	for _, f := range winnersDetoastFields {
		if strings.Contains(body, "d."+f) {
			t.Errorf("streamed body still reads the lateral record d.%s: the temp column fact.%s replaced it", f, f)
		}
	}
	for _, want := range []string{
		"JOIN " + winnersSupportsTable + " AS source_payload ON source_payload.fact_id = source.fact_id",
		"LEFT JOIN " + winnersSupportsTable + " AS override_payload ON override_payload.fact_id = override.fact_id",
		// The deliberate detoast exceptions stay on a payload expression:
		// cvss.BucketSQL keeps reading a payload (now the fetched row's),
		// and the three jsonb array projections keep `->`, preserving the
		// jsonb-null semantics the live differential pins.
		"AS severity_bucket",
		"source_payload.payload->'service_ids'",
		"source_payload.payload->'workload_ids'",
		"source_payload.payload->'environments'",
		// Every DETOAST exception above aside, the final projection reads
		// the carried temp columns, never a payload text field.
		"source.impact_status",
		"COALESCE(override.suppression_state, source.suppression_state)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("streamed body lost %q", want)
		}
	}
	// The assoc/class LEFT JOINs ride verbatim: the stream carries the
	// identical candidate row set.
	if !strings.Contains(body, winnersDetoastEligibilityTailForStreamTest()) {
		t.Error("streamed body lost the eligibility tail (assoc/class LEFT JOINs and filters)")
	}
}

// winnersDetoastEligibilityTailForStreamTest exposes the eligibility tail
// without its spliced lateral, the text the streamed body must carry
// verbatim after the lateral moved to the stage-1 temp.
func winnersDetoastEligibilityTailForStreamTest() string {
	return strings.Replace(winnersDetoastEligibilityFromActiveFacts, winnersDetoastFactLateral, "", 1)
}

// TestWinnersKeyedNarrowTempBuildDerivation pins the stage-1 narrow builds:
// the same temp table, the identity plus the once-extracted fields, no
// payload, one guarded lateral, and the untouched row-source filters
// (live_gens for the fetch, the operator scope for the insert).
func TestWinnersKeyedNarrowTempBuildDerivation(t *testing.T) {
	t.Parallel()

	builds := map[string]string{
		"narrow active facts":   buildWinnersKeyedActiveFactsNarrowFromSupportsSQL,
		"narrow operator facts": addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
	}
	for name, stmt := range builds {
		if strings.Contains(stmt, "SELECT fact.*") {
			t.Errorf("%s still selects the wide row", name)
		}
		// The guarded lateral takes fact.payload as its input; no
		// per-field read (fact.payload->> or ->) may remain.
		if strings.Contains(stmt, "fact.payload->") {
			t.Errorf("%s reads payload fields: the temp carries extracted columns, not the payload", name)
		}
		if n := strings.Count(stmt, "jsonb_to_record"); n != 1 {
			t.Errorf("%s extracts %d times, want exactly once per kept row", name, n)
		}
		if !strings.Contains(stmt, "jsonb_typeof(fact.payload) = 'object'") {
			t.Errorf("%s lost the non-object guard: a scalar payload must read NULLs, not abort the tick", name)
		}
		for _, col := range []string{"fact.fact_id", "fact.scope_id", "fact.generation_id", "fact.source_confidence"} {
			if !strings.Contains(stmt, col) {
				t.Errorf("%s does not select the identity column %s", name, col)
			}
		}
		for _, f := range winnersDetoastFields {
			if n := strings.Count(stmt, "d."+f); n != 1 {
				t.Errorf("%s selects d.%s %d times, want exactly once", name, f, n)
			}
		}
	}
	if !strings.Contains(buildWinnersKeyedActiveFactsNarrowFromSupportsSQL,
		"CREATE TEMP TABLE "+winnersKeyedActiveFactsTable+" ") {
		t.Errorf("narrow active facts does not create the temp table %q", winnersKeyedActiveFactsTable)
	}
	if !strings.Contains(addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
		"INSERT INTO "+winnersKeyedActiveFactsTable) {
		t.Errorf("narrow operator facts does not insert into the temp table %q", winnersKeyedActiveFactsTable)
	}
	if !strings.Contains(buildWinnersKeyedActiveFactsNarrowFromSupportsSQL, "live_gens AS MATERIALIZED") {
		t.Error("narrow active facts lost the once-per-tick live generation set")
	}
	if !strings.Contains(buildWinnersKeyedActiveFactsNarrowFromSupportsSQL, "JOIN live_gens AS g") {
		t.Error("narrow active facts lost the live-generation semi-join")
	}
	if !strings.Contains(addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
		"WHERE fact.scope_id = '"+supplyChainImpactOperatorScopeID+"'") {
		t.Error("narrow operator facts lost the operator scope filter")
	}
	if !strings.Contains(buildWinnersKeyedActiveFactsNarrowFromSupportsSQL, winnersKeyedNarrowTable) {
		t.Error("narrow active facts no longer reads the persisted narrow probe table")
	}
}

// TestWinnersKeyedNarrowingCommutesWithSupportsSwaps proves the narrow
// supports statements derive identically through either order: narrowing
// the supports twin equals swapping the narrowed legacy text. The swap
// input (legacy text) then stays the single derivation source, and the
// supports twin cannot silently drift from it.
func TestWinnersKeyedNarrowingCommutesWithSupportsSwaps(t *testing.T) {
	t.Parallel()

	if got := narrowWinnersKeyedTempBuild("keyed active facts",
		applyWinnersSupportsInputSwaps("keyed active facts", buildWinnersKeyedActiveFactsSQL, winnersKeyedFetchSwaps), ""); got != buildWinnersKeyedActiveFactsNarrowFromSupportsSQL {
		t.Error("narrowing does not commute with the supports swaps on the active-facts build")
	}
	if got := narrowWinnersKeyedTempBuild("keyed operator facts",
		applyWinnersSupportsInputSwaps("keyed operator facts", addWinnersKeyedOperatorFactsSQL, winnersKeyedOperatorSwaps),
		"\nWHERE fact.scope_id = '"+supplyChainImpactOperatorScopeID+"'"); got != addWinnersKeyedOperatorFactsNarrowFromSupportsSQL {
		t.Error("narrowing does not commute with the supports swaps on the operator insert")
	}
}

// TestWinnersKeyedNarrowTempRejectsADroppedField is the seeded-violation
// half of the derivation: narrowing a build whose select list lost a field
// fails loudly instead of running half-narrowed.
func TestWinnersKeyedNarrowTempRejectsADroppedField(t *testing.T) {
	t.Parallel()

	mutant := strings.Replace(buildWinnersKeyedActiveFactsFromSupportsSQL, "SELECT fact.*", "SELECT fact.fact_id", 1)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("narrowing a build without SELECT fact.* did not panic")
			}
		}()
		_ = narrowWinnersKeyedTempBuild("mutant", mutant, "")
	}()
}

// reverseWinnersKeyedNarrowTempBuild inverts narrowWinnersKeyedTempBuild:
// the generated select list becomes SELECT fact.* again and the single
// guarded lateral drops out, restoring the wide supports build
// byte-for-byte. It shares the generator with the derivation, so the two
// cannot drift.
func reverseWinnersKeyedNarrowTempBuild(narrow string) string {
	list := winnersKeyedNarrowTempSelectList()
	if n := strings.Count(narrow, list); n != 1 {
		panic("reverse narrow temp: select list occurs != 1")
	}
	out := strings.Replace(narrow, list, "SELECT fact.*", 1)
	if n := strings.Count(out, winnersDetoastFactLateral); n != 1 {
		panic("reverse narrow temp: lateral occurs != 1")
	}
	return strings.Replace(out, winnersDetoastFactLateral, "", 1)
}

// reverseWinnersKeyedStreamLayer inverts the streaming layer of one contract
// statement before the supports and detoast reversals run: a narrow temp
// build becomes its wide supports twin, and the streamed body inside the
// keyed apply becomes the shipped body. Statements without stream markers
// pass through unchanged. It panics on an ambiguous marker instead of
// half-reversing.
func reverseWinnersKeyedStreamLayer(stmt string) string {
	out := stmt
	if strings.Contains(out, winnersKeyedNarrowTempSelectList()) {
		out = reverseWinnersKeyedNarrowTempBuild(out)
	}
	const streamedFrom = `FROM source_winners AS source
JOIN ` + winnersSupportsTable + ` AS source_payload ON source_payload.fact_id = source.fact_id
LEFT JOIN operator_overrides AS override
  ON override.canonical_key = source.canonical_key
LEFT JOIN ` + winnersSupportsTable + ` AS override_payload ON override_payload.fact_id = override.fact_id`
	if n := strings.Count(out, streamedFrom); n > 1 {
		panic("reverse stream layer: streamed FROM occurs more than once")
	} else if n == 1 {
		out = reverseWinnersKeyedStreamBody(out)
	}
	return out
}
