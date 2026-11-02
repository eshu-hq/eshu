// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractRowsQuarantinesNegativeVersionPart plants the -1 sentinel value
// itself as a version major and proves the fact quarantines instead of
// projecting (#9147): a projected -1 would satisfy both the page predicate
// (-1 < bound) and the undecided test (coalesce(part, -1) = -1), so it must
// never reach the graph.
func TestExtractRowsQuarantinesNegativeVersionPart(t *testing.T) {
	t.Parallel()

	bad := versionEnvelope("repo-1", ".nvmrc", 1, 0, "node", "20", intPtr(-1))
	bad.FactID = "fact-negative-major"
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
	if q.FactID != "fact-negative-major" {
		t.Errorf("quarantined FactID = %q, want %q", q.FactID, "fact-negative-major")
	}
	if q.Field != "version_major" {
		t.Errorf("quarantined Field = %q, want %q", q.Field, "version_major")
	}
	if q.Classification != factschema.ClassificationInputInvalid {
		t.Errorf("quarantined Classification = %q, want %q", q.Classification, factschema.ClassificationInputInvalid)
	}
}
