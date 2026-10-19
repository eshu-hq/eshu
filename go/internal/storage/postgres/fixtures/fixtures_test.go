// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixtures

import (
	"strings"
	"testing"
)

// TestConfigParserRowShape pins the parser row shape both drift-evidence
// families share: one copy here, imported at every use site.
func TestConfigParserRowShape(t *testing.T) {
	row := ConfigParserRow("aws_instance", "web")
	for _, want := range []string{
		`"name":"aws_instance.web"`,
		`"resource_type":"aws_instance"`,
		`"resource_name":"web"`,
		`"path":"main.tf"`,
		`"lang":"hcl"`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("ConfigParserRow() = %s, missing %s", row, want)
		}
	}
	arr := ConfigResourcesArray(row, row)
	if !strings.HasPrefix(string(arr), "[") || !strings.HasSuffix(string(arr), "]") {
		t.Errorf("ConfigResourcesArray() = %s, want JSON array shape", arr)
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

	row10 := []any{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	got = PadQueueFakeRow(row10, 11)
	if len(got) != 11 {
		t.Fatalf("PadQueueFakeRow(10-wide, 11) width = %d, want 11", len(got))
	}
	if got[9] != 9 {
		t.Errorf("PadQueueFakeRow(10-wide, 11)[9] = %v, want duplicated row[8] as last_attempt_at token", got[9])
	}

	row9 := []any{1, 2, 3, 4, 5, 6, 7, 8, 9}
	got = PadQueueFakeRow(row9, 10)
	if len(got) != 10 {
		t.Fatalf("PadQueueFakeRow(9-wide, 10) width = %d, want 10", len(got))
	}
	if got[9] != false {
		t.Errorf("PadQueueFakeRow(9-wide, 10)[9] = %v, want zero-filled delta_protected flag", got[9])
	}
}
