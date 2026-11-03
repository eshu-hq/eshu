// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractRowsQuarantinesNegativeVersionPart plants the -1 sentinel value
// itself in each version part and proves the fact quarantines instead of
// projecting (#9147): a projected -1 would satisfy both the page predicate
// (-1 < bound) and the undecided test (coalesce(part, -1) = -1), so it must
// never reach the graph.
func TestExtractRowsQuarantinesNegativeVersionPart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
		major *int
		minor *int
		patch *int
	}{
		{name: "major-sentinel", field: "version_major", major: intPtr(-1), minor: intPtr(2), patch: intPtr(3)},
		{name: "minor-sentinel", field: "version_minor", major: intPtr(20), minor: intPtr(-1), patch: intPtr(3)},
		{name: "patch-sentinel", field: "version_patch", major: intPtr(20), minor: intPtr(2), patch: intPtr(-1)},
		// Non-sentinel negatives exercise the same contract: any present
		// negative part quarantines, not just the -1 sentinel (#9147).
		{name: "major-other", field: "version_major", major: intPtr(-2), minor: intPtr(2), patch: intPtr(3)},
		{name: "minor-other", field: "version_minor", major: intPtr(20), minor: intPtr(-100), patch: intPtr(3)},
		{name: "patch-other", field: "version_patch", major: intPtr(20), minor: intPtr(2), patch: intPtr(-7)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bad := versionEnvelopeParts("repo-1", ".nvmrc", 1, 0, "node", "20", tc.major, tc.minor, tc.patch)
			bad.FactID = "fact-negative-" + tc.name
			good := versionEnvelope("repo-1", ".tool-versions", 2, 0, "node", "20", intPtr(20))
			good.FactID = "fact-valid-major"

			rows, quarantined, err := ExtractRows([]facts.Envelope{bad, good})
			if err != nil {
				t.Fatalf("ExtractRows returned error: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("ExtractRows projected %d rows, want 1 (the valid fact only): %v", len(rows), rows)
			}
			if got := rows[0]["relative_path"]; got != ".tool-versions" {
				t.Errorf("projected row relative_path = %v, want %q (the valid fact)", got, ".tool-versions")
			}
			if len(quarantined) != 1 {
				t.Fatalf("ExtractRows quarantined %d facts, want 1: %v", len(quarantined), quarantined)
			}
			q := quarantined[0]
			if q.FactID != "fact-negative-"+tc.name {
				t.Errorf("quarantined FactID = %q, want %q", q.FactID, "fact-negative-"+tc.name)
			}
			if q.Field != tc.field {
				t.Errorf("quarantined Field = %q, want %q", q.Field, tc.field)
			}
			if q.Classification != factschema.ClassificationInputInvalid {
				t.Errorf("quarantined Classification = %q, want %q", q.Classification, factschema.ClassificationInputInvalid)
			}
		})
	}
}
