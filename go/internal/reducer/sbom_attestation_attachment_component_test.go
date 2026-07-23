// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
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
