// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"os"
	"strings"
	"testing"
)

// TestSpecReadingsMatchREADME keeps the README "Spec readings" table and
// SpecReadings() in step: every reading the report carries has a row, and
// the table has no row the code does not apply.
func TestSpecReadingsMatchREADME(t *testing.T) {
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	section := string(body)
	start := strings.Index(section, "## Spec readings")
	if start < 0 {
		t.Fatalf("README has no Spec readings section")
	}
	section = section[start:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	rows := strings.Count(section, "\n| `")
	readings := SpecReadings()
	if rows != len(readings) {
		t.Fatalf("README table has %d rows, SpecReadings has %d", rows, len(readings))
	}
	for _, r := range readings {
		if !strings.Contains(section, "| `"+r.ID+"` |") {
			t.Fatalf("README Spec readings table has no row for %q", r.ID)
		}
		if r.Quote == "" || r.Spec == "" || r.Reading == "" {
			t.Fatalf("reading %q has an empty field", r.ID)
		}
	}
}
