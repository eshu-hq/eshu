// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// TestComponentEvidenceRowsDedupesCapsAndCountsBeforeCap proves
// componentEvidenceRows applies the same write-time bounding discipline as
// dependency/external-reference evidence (#5370): deterministic dedupe on the
// complete tuple, deterministic lexicographic sort with fact_id as the final
// tiebreaker, a write-time cap at maxSBOMAttachmentComponentEvidenceRows, and
// a full distinct-tuple count computed before that cap so callers can detect
// truncation (#5412).
func TestComponentEvidenceRowsDedupesCapsAndCountsBeforeCap(t *testing.T) {
	t.Parallel()

	components := make([]sbomAttachmentComponentEvidence, 0, maxSBOMAttachmentComponentEvidenceRows+2)
	for i := 0; i <= maxSBOMAttachmentComponentEvidenceRows; i++ {
		components = append(components, sbomAttachmentComponentEvidence{
			factID:           fmt.Sprintf("fact-%03d", i),
			componentID:      fmt.Sprintf("component-%03d", i),
			name:             fmt.Sprintf("component-%03d", i),
			version:          "1.0.0",
			purl:             fmt.Sprintf("pkg:npm/component-%03d@1.0.0", i),
			ecosystem:        "npm",
			dependencyScope:  "runtime",
			dependencyType:   "direct",
			extractionReason: "sbom",
		})
	}
	components = append(components, sbomAttachmentComponentEvidence{
		factID:           "fact-000-duplicate",
		componentID:      "component-000",
		name:             "component-000",
		version:          "1.0.0",
		purl:             "pkg:npm/component-000@1.0.0",
		ecosystem:        "npm",
		dependencyScope:  "runtime",
		dependencyType:   "direct",
		extractionReason: "sbom",
	})
	components = append(components, sbomAttachmentComponentEvidence{
		factID:           "fact-000-lockfile",
		componentID:      "component-000",
		name:             "component-000",
		version:          "1.0.0",
		purl:             "pkg:npm/component-000@1.0.0",
		ecosystem:        "npm",
		lockfilePath:     "package-lock.json",
		dependencyScope:  "runtime",
		dependencyType:   "direct",
		extractionReason: "sbom",
	})

	rows, count := componentEvidenceRows(components)
	if got, want := count, maxSBOMAttachmentComponentEvidenceRows+2; got != want {
		t.Fatalf("component count = %d, want %d distinct tuples before cap", got, want)
	}
	if got, want := len(rows), maxSBOMAttachmentComponentEvidenceRows; got != want {
		t.Fatalf("component evidence rows = %d, want capped %d", got, want)
	}
	if got, want := rows[0]["fact_id"], "fact-000"; got != want {
		t.Fatalf("duplicate component fact_id = %q, want lexicographically smallest %q", got, want)
	}
	if got, want := rows[1]["lockfile_path"], "package-lock.json"; got != want {
		t.Fatalf("hidden-tuple lockfile_path = %q, want %q", got, want)
	}
	if got, want := rows[len(rows)-1]["component_id"], "component-098"; got != want {
		t.Fatalf("last capped component_id = %q, want %q", got, want)
	}
}

// TestComponentEvidenceRowsIsOrderInvariantAcrossShuffledDuplicates proves the
// fact_id final tiebreak in componentEvidenceLess actually determines which
// duplicate survives dedupe, not merely which one a single fixed input
// ordering happens to keep. sort.Slice is not a stable sort: without the
// fact_id tiebreak, two tuples that are equal on every other field compare as
// neither-less-than-the-other, so their relative order after sorting is
// unspecified and can leak the input's original order into the result. This
// test feeds the same duplicate-carrying component set in many independently
// shuffled orders and asserts componentEvidenceRows returns byte-identical
// output (and keeps the lexicographically smallest fact_id, "dup-a") every
// time. Deleting the "return a.factID < b.factID" tiebreak line in
// componentEvidenceLess makes this test flaky/fail across shuffles even
// though TestComponentEvidenceRowsDedupesCapsAndCountsBeforeCap above still
// passes (it only ever exercises one fixed input order).
func TestComponentEvidenceRowsIsOrderInvariantAcrossShuffledDuplicates(t *testing.T) {
	t.Parallel()

	base := make([]sbomAttachmentComponentEvidence, 0, maxSBOMAttachmentComponentEvidenceRows+3)
	for i := 0; i <= maxSBOMAttachmentComponentEvidenceRows; i++ {
		base = append(base, sbomAttachmentComponentEvidence{
			factID:           fmt.Sprintf("fact-%03d", i),
			componentID:      fmt.Sprintf("component-%03d", i),
			name:             fmt.Sprintf("component-%03d", i),
			version:          "1.0.0",
			purl:             fmt.Sprintf("pkg:npm/component-%03d@1.0.0", i),
			ecosystem:        "npm",
			dependencyScope:  "runtime",
			dependencyType:   "direct",
			extractionReason: "sbom",
		})
	}
	// dup-a and dup-b are equal on every field the tiebreak isn't (including
	// leaving dependencyScope/dependencyType/extractionReason blank, unlike
	// the loop entries above, so neither collides with the loop's own
	// component-000 tuple): only fact_id distinguishes them, so
	// componentEvidenceLess's tiebreak alone decides which one dedupe keeps.
	base = append(base,
		sbomAttachmentComponentEvidence{
			factID: "dup-a", componentID: "component-000", name: "component-000",
			version: "1.0.0", purl: "pkg:npm/component-000@1.0.0", ecosystem: "npm",
		},
		sbomAttachmentComponentEvidence{
			factID: "dup-b", componentID: "component-000", name: "component-000",
			version: "1.0.0", purl: "pkg:npm/component-000@1.0.0", ecosystem: "npm",
		},
	)
	const wantDistinctCount = maxSBOMAttachmentComponentEvidenceRows + 2 // 101 loop tuples + 1 deduped dup

	var reference []map[string]string
	for trial := 0; trial < 40; trial++ {
		shuffled := append([]sbomAttachmentComponentEvidence(nil), base...)
		rng := rand.New(rand.NewSource(int64(trial))) //nolint:gosec // deterministic shuffle, not security-sensitive
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		rows, count := componentEvidenceRows(shuffled)
		if got, want := count, wantDistinctCount; got != want {
			t.Fatalf("trial %d: component count = %d, want %d distinct tuples", trial, got, want)
		}
		if trial == 0 {
			reference = rows
			continue
		}
		if !reflect.DeepEqual(rows, reference) {
			t.Fatalf(
				"trial %d: componentEvidenceRows output changed across a shuffled input order (tiebreak not order-invariant)\nreference: %#v\ngot:       %#v",
				trial, reference, rows,
			)
		}
	}

	var sawDupA, sawDupB bool
	for _, row := range reference {
		switch row["fact_id"] {
		case "dup-a":
			sawDupA = true
		case "dup-b":
			sawDupB = true
		}
	}
	if !sawDupA {
		t.Fatal(`reference output missing fact_id "dup-a": tiebreak should keep the lexicographically smallest fact_id`)
	}
	if sawDupB {
		t.Fatal(`reference output contains fact_id "dup-b": duplicate tuple should have deduped away, keeping only "dup-a"`)
	}
}
