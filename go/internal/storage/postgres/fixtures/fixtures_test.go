// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixtures

import (
	"strings"
	"testing"
)

// TestFixtureConfigParserRowShape pins the parser row shape both drift-evidence
// families share: one copy here, imported at every use site.
func TestFixtureConfigParserRowShape(t *testing.T) {
	row := FixtureConfigParserRow("aws_instance", "web")
	for _, want := range []string{
		`"name":"aws_instance.web"`,
		`"resource_type":"aws_instance"`,
		`"resource_name":"web"`,
		`"path":"main.tf"`,
		`"lang":"hcl"`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("FixtureConfigParserRow() = %s, missing %s", row, want)
		}
	}
	arr := FixtureConfigResourcesArray(row, row)
	if !strings.HasPrefix(string(arr), "[") || !strings.HasSuffix(string(arr), "]") {
		t.Errorf("FixtureConfigResourcesArray() = %s, want JSON array shape", arr)
	}
}

// TestPadQueueFakeRowRules pins the padding rules both queue-fake families
// share: claim-epoch synthesis, last_attempt_at synthesis, and retention
// zero-fills.
func TestPadQueueFakeRowRules(t *testing.T) {
	row8 := []any{1, 2, 3, 4, 5, 6, 7, 8}
	got := PadQueueFakeRow(row8, 9)
	if len(got) != 9 {
		t.Fatalf("PadQueueFakeRow(8-wide, 9) width = %d, want 9", len(got))
	}
	if got[5] != int64(0) {
		t.Errorf("PadQueueFakeRow(8-wide, 9)[5] = %v, want synthesized int64(0) claim epoch", got[5])
	}

	row6 := []any{1, 2, 3, 4, 5, 6}
	got = PadQueueFakeRow(row6, 10)
	if len(got) != 10 {
		t.Fatalf("PadQueueFakeRow(6-wide, 10) width = %d, want 10", len(got))
	}
}
