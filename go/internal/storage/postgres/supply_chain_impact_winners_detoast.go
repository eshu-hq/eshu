// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
)

// Single-detoast extraction (#11120 criterion 2). Every `payload->>'field'`
// detoasts the whole JSONB value again, so the winner body's ~25 text reads
// per candidate re-read the same payload ~25 times. These declarations extract
// every text field the body needs exactly once through one LATERAL
// jsonb_to_record per row; every downstream expression reads the record.
//
// The detoasted fragments are DERIVED from the legacy helpers, never copied:
// winnersDetoastFieldReplacer renders the payload helpers over fact.payload
// and swaps each field read for its record ref, so the canonical key, the
// finding-id fallback, and the ranking inputs cannot drift from the shipped
// definitions. The hermetic reverse (reverseWinnersDetoast, in the detoast
// test) inverts exactly this mapping, and the live field-equivalence proof
// covers every pair.
//
// Two deliberate exceptions stay on the payload, pinned by hermetic tests:
//   - cvss.BucketSQL keeps source.payload through the shared function, which
//     the read-time dedup also calls (splitting it forks the severity
//     contract).
//   - The three jsonb array projections keep `->`: jsonb_to_record maps a
//     JSON null to SQL NULL while `->` keeps jsonb null, so converting them
//     would store '[]' where the legacy stored null (proven live).
// The guarded lateral treats a non-object payload as all-NULLs, exactly like
// the legacy `->>` reads: jsonb_to_record errors on scalars, so the CASE keeps
// one bad row from aborting the tick.

// winnersDetoastFields are the nineteen text fields the winner body extracts
// per candidate: the identifier fields, the canonical-key arguments, the
// finding-id fallback, the ranking inputs, and the final-projection columns.
// Every declaration below derives from this list, so a field cannot be
// extracted without being declared, or declared without being extracted (the
// coverage test pins both directions).
var winnersDetoastFields = []string{
	"cve_id", "advisory_id", "package_id", "purl", "product_criteria",
	"match_criteria_id", "observed_version", "requested_range",
	"impact_status", "repository_id", "subject_digest", "finding_id",
	"priority_score", "suppression_state", "ecosystem", "image_ref",
	"priority_bucket", "detection_profile", "match_reason",
}

// winnersDetoastIdentFields are the five identifier fields the scope expansion
// extracts per finding: the subset of the record the identifier build reads.
var winnersDetoastIdentFields = []string{
	"package_id", "repository_id", "subject_digest", "cve_id", "advisory_id",
}

// winnersDetoastFinalFields are the twelve carried columns the final
// projection reads instead of re-extracting from source.payload.
var winnersDetoastFinalFields = []string{
	"impact_status", "ecosystem", "repository_id", "cve_id", "advisory_id",
	"package_id", "subject_digest", "image_ref", "priority_bucket",
	"detection_profile", "observed_version", "match_reason",
}

// winnersDetoastTextColumns is the record definition shared by every guarded
// lateral. Text columns keep the legacy `->>` coercion on every JSON shape
// (numbers, booleans, nulls), proven live field by field.
var winnersDetoastTextColumns = func() string {
	cols := make([]string, 0, len(winnersDetoastFields))
	for _, f := range winnersDetoastFields {
		cols = append(cols, f+" text")
	}
	return strings.Join(cols, ", ")
}()

// winnersDetoastFactLateral extracts the record once per active_facts row.
// The CASE guard maps a non-object payload to a NULL record, which is what
// the legacy `->>` reads returned for the same row.
var winnersDetoastFactLateral = `
    CROSS JOIN LATERAL jsonb_to_record(CASE WHEN jsonb_typeof(fact.payload) = 'object' THEN fact.payload END) AS d(` + winnersDetoastTextColumns + `)`

// winnersDetoastFieldReplacer swaps each fact.payload text-field read for its
// once-extracted record ref. The trailing quote on every old fragment keeps
// field names from prefix-colliding (purl cannot match a longer key).
var winnersDetoastFieldReplacer = func() *strings.Replacer {
	pairs := make([]string, 0, 2*len(winnersDetoastFields))
	for _, f := range winnersDetoastFields {
		pairs = append(pairs, "fact.payload->>'"+f+"'", "d."+f)
	}
	return strings.NewReplacer(pairs...)
}()

// winnersDetoastOperatorPredicate is the operator-suppression guard over the
// record, derived from the shared predicate so the two cannot drift. The
// staged scan keeps the shared const (it has no record); the body uses this.
var winnersDetoastOperatorPredicate = strings.Replace(
	supplyChainImpactOperatorSuppressionPredicate,
	"fact.payload->>'suppression_state'", "d.suppression_state", 1)

// winnersDetoastEligibilityFromActiveFacts is the source-candidate row source:
// the shared eligibility tail with the guarded lateral spliced between the
// FROM and the LEFT JOINs. It panics at initialization unless the splice point
// matches exactly once, so a change to the shared tail cannot silently drop
// the lateral (the staged scan reuses the same tail without it).
var winnersDetoastEligibilityFromActiveFacts = func() string {
	const anchor = "FROM active_facts AS fact\n"
	if n := strings.Count(supplyChainImpactSourceCandidateEligibilitySQL, anchor); n != 1 {
		panic(fmt.Sprintf("winners detoast eligibility splice anchor occurs %d times, want exactly once", n))
	}
	return strings.Replace(supplyChainImpactSourceCandidateEligibilitySQL, anchor,
		"FROM active_facts AS fact"+winnersDetoastFactLateral+"\n", 1)
}()

// winnersDetoastCarriedSelect renders the final-projection columns the
// candidates carry: each pre-wrapped exactly as the legacy final SELECT
// wrapped it, so the final SELECT reads bare source columns.
func winnersDetoastCarriedSelect() string {
	items := make([]string, 0, len(winnersDetoastFinalFields))
	for _, f := range winnersDetoastFinalFields {
		items = append(items, "COALESCE(d."+f+", '') AS "+f)
	}
	return ",\n        " + strings.Join(items, ",\n        ")
}
