// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// Hermetic pins for the ruled #9805 v3-full fix, re-sourced round 5: the full
// rebuild walks the table as bounded keyed chunks over the supports table,
// the only copy of a finding since the #10880 write stop. The
// supports-ready gate picks the probe shape only (triple-guarded presence
// while open, supports payload-expression while closed); ranking reads
// PAYLOAD-derived fields through the shipped detoast extraction, never
// stored supports columns; the winner and override payloads arrive by
// fact_id point lookup over supports; the shipped final projection runs
// verbatim; each chunk sorts at default work_mem; the lock and the audit
// oracle are unchanged. Rejected: a covering index, a precise-only path, a
// work_mem raise, serialization to batch 1, and any fact_records read on the
// chunk path.

// TestWinnersFullChunkedReadsPayloadDerivedRanking pins the ruled row source
// and ranking inputs: the chunked-full statements read supports, extract
// the ranking fields from the verbatim payload through the shipped detoast
// lateral, order by the derived canonical key (never a stored supports
// column), add no covering index, and touch no work_mem knob.
func TestWinnersFullChunkedReadsPayloadDerivedRanking(t *testing.T) {
	t.Parallel()

	// The enumeration pages the exact supports candidate set: the
	// supports-swapped active-facts inner select is its row source, so
	// eligibility cannot drift from what the keyed tick and the audit run.
	if !strings.Contains(buildWinnersFullIdentsSQL, supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL) {
		t.Error("full-chunk enumeration does not read the supports active-facts inner select")
	}
	if strings.Contains(buildWinnersFullIdentsSQL, supplyChainImpactWinnerActiveFactsInnerSQL) {
		t.Error("full-chunk enumeration reads the legacy fact_records inner select: it pages supports identifiers")
	}
	// The identifier columns come from the once-extracted detoast record
	// over the verbatim supports payload, never a stored supports column.
	for _, f := range winnersDetoastIdentFields {
		if !strings.Contains(buildWinnersFullIdentsSQL, "d."+f) {
			t.Errorf("full-chunk enumeration does not extract identifier field d.%s from the payload", f)
		}
	}
	if !strings.Contains(buildWinnersFullIdentsSQL, "supply_chain_impact_finding_supports") {
		t.Error("full-chunk enumeration does not read the supports table: it must page supports identifiers")
	}

	for name, stmt := range map[string]string{
		"chunk select": winnersFullChunkSelectSQL,
		"chunk apply":  applyWinnersFullChunkSQL,
	} {
		// Ranking inputs are payload-derived through the once-per-row
		// detoast extraction the narrow chunk temp carries: canonical_key,
		// priority_score and has_payload_finding_id read the extracted
		// temp columns, never a per-field payload re-detost.
		for _, want := range []string{
			"fact.priority_score",
			"fact.cve_id",
			"has_payload_finding_id",
		} {
			if !strings.Contains(stmt, want) {
				t.Errorf("%s lost the payload-derived ranking input %q", name, want)
			}
		}
		for _, banned := range []string{
			"fact.payload->>",
			"source.payload->>",
			"override.payload->>",
			"jsonb_to_record",
		} {
			if strings.Contains(stmt, banned) {
				t.Errorf("%s re-detosts the payload per field via %q: ranking reads the extracted temp", name, banned)
			}
		}
		if !strings.Contains(stmt, "ORDER BY canonical_key,") {
			t.Errorf("%s does not sort winners by the derived canonical key", name)
		}
		// Never stored supports columns: no key-presence bits, no stored
		// plain-column ordering — the chunk fetches winner payloads from
		// supports by fact_id, and ranking reads the extracted temp,
		// never the stored derived columns.
		for _, banned := range []string{
			"key_presence",
			"s.package_id",
			"s.repository_id",
			"s.subject_digest",
			"ORDER BY s.",
		} {
			if strings.Contains(stmt, banned) {
				t.Errorf("%s reads a stored supports column via %q", name, banned)
			}
		}
		if !strings.Contains(stmt, "supply_chain_impact_finding_supports") {
			t.Errorf("%s does not read the supports table: payloads arrive from supports, never fact_records", name)
		}
		if strings.Contains(stmt, "fact_records") {
			t.Errorf("%s reads fact_records: no chunk statement may read the frozen finding facts", name)
		}
		// Winner and override payloads arrive by fact_id point lookup only.
		for _, want := range []string{
			"JOIN supply_chain_impact_finding_supports AS source_payload ON source_payload.fact_id = source.fact_id",
			"LEFT JOIN supply_chain_impact_finding_supports AS override_payload ON override_payload.fact_id = override.fact_id",
		} {
			if !strings.Contains(stmt, want) {
				t.Errorf("%s lost the supports point lookup %q", name, want)
			}
		}
	}

	for name, stmt := range map[string]string{
		"enumeration":  buildWinnersFullIdentsSQL,
		"chunk select": winnersFullChunkSelectSQL,
		"chunk apply":  applyWinnersFullChunkSQL,
		"final stamp":  stampWinnersFullWatermarkSQL,
		"chunk page":   selectWinnersFullChunkPageSQL,
	} {
		if strings.Contains(strings.ToLower(stmt), "work_mem") {
			t.Errorf("%s sets work_mem: every chunk sorts at the default", name)
		}
		if strings.Contains(strings.ToUpper(stmt), "CREATE INDEX") {
			t.Errorf("%s adds an index: the ruled fix adds no covering index", name)
		}
	}
}

// TestWinnersFullChunkNarrowProbeFallsBackToSupportsExpression pins the ruled
// probe flip: under the supports-ready gate the chunk probes supports for
// candidate fact_id presence with the triple guard; when the gate is closed
// the same chunk probes supports with the shipped payload expression. The
// flip is the probe shape, never the row source: both read supports, since
// the write stop left no finding facts for a fact_records probe to read.
func TestWinnersFullChunkNarrowProbeFallsBackToSupportsExpression(t *testing.T) {
	t.Parallel()

	if got := winnersFullChunkNarrowProbe(true); got != insertWinnersKeyedNarrowPresenceFullChunkSQL {
		t.Error("an open supports gate must run the triple-guarded presence probe over supports")
	}
	if got := winnersFullChunkNarrowProbe(false); got != insertWinnersKeyedNarrowSupportsFullChunkSQL {
		t.Error("a closed supports gate must fall back to the supports payload-expression probe")
	}
	if strings.Contains(winnersFullChunkNarrowProbe(true), "fact_records") ||
		strings.Contains(winnersFullChunkNarrowProbe(false), "fact_records") {
		t.Error("a chunk probe reads fact_records: both probe shapes read supports")
	}
	// The presence probe reads supports for fact_id presence only: the
	// triple guard plus the copy identity, no stored ranking columns, no
	// payload text reads.
	for _, want := range []string{
		"FROM " + winnersSupportsTable + " AS s",
		"(s.key_presence & 1) <> 0",
		"SELECT s.fact_id, s.scope_id, s.generation_id",
	} {
		if !strings.Contains(insertWinnersKeyedNarrowPresenceFullChunkSQL, want) {
			t.Errorf("presence probe lost %q: supports serve candidate fact_id presence only", want)
		}
	}
	if strings.Contains(insertWinnersKeyedNarrowPresenceFullChunkSQL, "s.payload->>") {
		t.Error("presence probe reads supports payload fields: presence is the fact_id, ranking stays payload-derived")
	}
}

// TestWinnersFullChunkStreamBodyReversesToShipped is the differential guard:
// the chunked-full body differs from the shipped legacy select body only by
// the declared point-lookup transform, so its winners match the legacy
// supplyChainImpactWinnerSelectSQL row for row on the same facts.
func TestWinnersFullChunkStreamBodyReversesToShipped(t *testing.T) {
	t.Parallel()

	if got := reverseWinnersKeyedStreamBodyFrom(winnersFullChunkStreamBodySQL, winnersSupportsTable); got != supplyChainImpactWinnerBodySQL {
		t.Fatalf("full-chunk streamed body does not reverse to the shipped body byte-for-byte:\n%s",
			firstDiffLine(got, supplyChainImpactWinnerBodySQL))
	}
}

// TestWinnersFullChunkSelectDerivesFromShippedBody pins that the chunked-full
// select is the shipped body over the chunk's active_facts temp, so the rank
// and the final projection cannot drift from what the audit runs.
func TestWinnersFullChunkSelectDerivesFromShippedBody(t *testing.T) {
	t.Parallel()

	const prefix = `WITH active_facts AS (SELECT * FROM ` + winnersKeyedActiveFactsTable + `)`
	if !strings.HasPrefix(winnersFullChunkSelectSQL, prefix) {
		t.Fatal("full-chunk select does not open over the chunk active_facts temp")
	}
	if rest := strings.TrimPrefix(winnersFullChunkSelectSQL, prefix); rest != winnersFullChunkStreamBodySQL {
		t.Fatalf("full-chunk select tail is not the streamed body:\n%s",
			firstDiffLine(rest, winnersFullChunkStreamBodySQL))
	}
}

// TestWinnersFullChunkApplyKeepsTheWriteOnlyChangedShape pins that each
// chunk writes only new or changed winner rows with the same row comparison
// as the keyed apply, scoped to the chunk's identifier set, and deletes only
// stored winners of that set that the chunk no longer produces.
func TestWinnersFullChunkApplyKeepsTheWriteOnlyChangedShape(t *testing.T) {
	t.Parallel()

	body := applyWinnersFullChunkBodySQL
	for _, want := range []string{
		"changed AS MATERIALIZED (",
		"LEFT JOIN supply_chain_impact_canonical_winners AS w",
		"WHERE w.canonical_key IS NULL",
		") IS DISTINCT FROM ROW(",
		"FROM changed\n    ON CONFLICT (canonical_key) DO UPDATE SET",
		winnersStoredIdentMatch,
		"AND NOT EXISTS (SELECT 1 FROM winners_now AS n WHERE n.canonical_key = w.canonical_key)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("full-chunk apply lost %q", want)
		}
	}
	if !strings.Contains(body, "supply_chain_impact_finding_supports") {
		t.Error("full-chunk apply does not read the supports table: payloads arrive from supports, never fact_records")
	}
	if strings.Contains(body, "fact_records") {
		t.Error("full-chunk apply reads fact_records: no chunk statement may read the frozen finding facts")
	}
	// The chunk counts are the keyed apply's first four RETURNING lines:
	// inserted, updated, deleted, unchanged. The final stamp reports the
	// committed watermark separately, so a chunk never stamps.
	for _, want := range []string{
		"(SELECT count(*) FROM changed WHERE is_new)",
		"(SELECT count(*) FROM changed WHERE NOT is_new)",
		"(SELECT count(*) FROM deleted)",
		"(SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed)",
	} {
		if !strings.Contains(applyWinnersFullChunkSQL, want) {
			t.Errorf("full-chunk counts lost %q", want)
		}
	}
	if strings.Contains(applyWinnersFullChunkSQL, "supply_chain_impact_winners_materialization") {
		t.Error("a chunk stamps the watermark: only the final stamp runs, once, after every chunk")
	}
}

// TestWinnersFullChunkNarrowTempMatchesSelectList pins that the chunk
// active-facts temp declares its columns in winnersKeyedNarrowTempSelectList
// order, so an INSERT position can never land a field in the wrong column.
func TestWinnersFullChunkNarrowTempMatchesSelectList(t *testing.T) {
	t.Parallel()

	list := strings.Split(winnersKeyedNarrowTempSelectList()[len("SELECT "):], ",\n    ")
	want := make([]string, 0, len(list))
	for _, col := range list {
		// Each select item is table-qualified: the temp column is the bare name.
		parts := strings.Split(strings.TrimSpace(col), ".")
		want = append(want, parts[len(parts)-1])
	}
	// The literal CREATE (the DDL parity scan reads only literals) must
	// declare each temp column as text, in select-list order.
	rest := createWinnersKeyedActiveFactsNarrowSQL
	for _, col := range want {
		idx := strings.Index(rest, col+" text")
		if idx < 0 {
			t.Fatalf("narrow temp CREATE does not declare column %q as text", col)
		}
		rest = rest[idx+len(col)+len(" text"):]
	}
}

// TestWinnersFullChunkInsertDerivationsRoundTrip pins that the per-chunk
// INSERT builds are the existing CREATE builds with only the head swapped,
// so the probe, the live-generation set and the operator filter cannot drift
// between the first chunk and any later one.
func TestWinnersFullChunkInsertDerivationsRoundTrip(t *testing.T) {
	t.Parallel()

	roundTrip := func(name, insertSQL, insertHead, createSQL, createHead string) {
		t.Helper()
		if n := strings.Count(insertSQL, insertHead); n != 1 {
			t.Fatalf("%s holds the insert head %d times, want exactly once", name, n)
		}
		if got := strings.Replace(insertSQL, insertHead, createHead, 1); got != createSQL {
			t.Errorf("%s does not round-trip to its CREATE build:\n%s", name,
				firstDiffLine(got, createSQL))
		}
	}
	roundTrip("supports payload-expression chunk probe",
		insertWinnersKeyedNarrowSupportsFullChunkSQL, winnersFullChunkNarrowInsertHead,
		buildWinnersKeyedNarrowFromSupportsSQL, winnersFullChunkNarrowCreateHead)
	roundTrip("presence chunk probe",
		insertWinnersKeyedNarrowPresenceFullChunkSQL, winnersFullChunkNarrowInsertHead,
		buildWinnersKeyedNarrowKeyPresenceSQL, winnersFullChunkNarrowCreateHead)
	roundTrip("chunk active facts",
		insertWinnersFullChunkActiveFactsSQL, winnersFullChunkActiveFactsInsertHead,
		buildWinnersKeyedActiveFactsNarrowFromSupportsSQL, winnersFullChunkActiveFactsCreateHead)
}

// TestWinnersStoredIdentMatchDerivesFromSharedFragment pins that the delete
// match literal is the derived shared fragment, not a second hand-copy: the
// match LHS is byte-equal to winnersStoredIdentTuple("w"), and its c0 is
// byte-equal to winnersStoredIdentC0("w"). The literal stays a const so const
// SQL builders can splice it; this test is what keeps it honest. Mutating the
// fragment (or hand-editing the literal) fails here.
func TestWinnersStoredIdentMatchDerivesFromSharedFragment(t *testing.T) {
	t.Parallel()

	wantLHS := `(` + winnersStoredIdentTuple("w") + `)`
	if !strings.HasPrefix(winnersStoredIdentMatch, wantLHS) {
		t.Errorf("match LHS is not the derived tuple:\nmatch %.120s...\nwant  %.120s...",
			winnersStoredIdentMatch, wantLHS)
	}
	c0 := winnersStoredIdentC0("w")
	if strings.Count(winnersStoredIdentMatch, c0) != 1 {
		t.Errorf("match holds the shared c0 %d times, want exactly once", strings.Count(winnersStoredIdentMatch, c0))
	}
	// The tuple and the select list share the c0 fragment verbatim: a drift
	// between the delete's identity and the enumeration's stored arm would
	// leave stored-only rows no chunk visits.
	if !strings.Contains(winnersStoredIdentSelectList("w"), c0+" AS c0") {
		t.Error("stored select list does not carry the shared c0 fragment as c0")
	}
}

// TestWinnersFullEnumerationUnionsLiveAndStoredIdents pins the ruled #9805
// enumeration shape: the live arm pages the supports-swapped active-facts
// inner select, the stored arm selects the derived stored identifiers
// verbatim, and exactly one UNION joins them. A stored arm that hand-copies
// the c0 expression (instead of deriving winnersStoredIdentSelectList) fails
// the verbatim check; dropping the stored arm fails the UNION and table
// checks, which is the tombstoned-key regression that left Deleted at 0. A
// live arm over fact_records fails the row-source check: the write stop left
// no finding facts to enumerate.
func TestWinnersFullEnumerationUnionsLiveAndStoredIdents(t *testing.T) {
	t.Parallel()

	if !strings.Contains(buildWinnersFullIdentsSQL, supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL) {
		t.Error("enumeration lost the live arm: the supports active-facts inner select is gone")
	}
	if strings.Contains(buildWinnersFullIdentsSQL, supplyChainImpactWinnerActiveFactsInnerSQL) {
		t.Error("enumeration reads the legacy fact_records inner select: the live arm pages supports")
	}
	if !strings.Contains(buildWinnersFullIdentsSQL, winnersFullStoredIdentsSQL) {
		t.Error("enumeration does not carry the stored arm verbatim: it must derive, never hand-copy")
	}
	if !strings.Contains(winnersFullStoredIdentsSQL, winnersStoredIdentSelectList("w")) {
		t.Error("stored arm does not derive from winnersStoredIdentSelectList")
	}
	if n := strings.Count(buildWinnersFullIdentsSQL, "UNION"); n != 1 {
		t.Errorf("enumeration holds UNION %d times, want exactly one live-plus-stored union", n)
	}
	if !strings.Contains(buildWinnersFullIdentsSQL, "FROM supply_chain_impact_canonical_winners AS w") {
		t.Error("enumeration stored arm does not read the canonical winners table")
	}
	if !strings.Contains(buildWinnersFullIdentsSQL, "supply_chain_impact_finding_supports") {
		t.Error("enumeration live arm does not read the supports table: ranking stays payload-derived over supports")
	}
}
