// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

func TestSplitPropertiesLineAcceptsJavaSeparators(t *testing.T) {
	for _, tc := range []struct {
		line, key, value string
		ok               bool
	}{
		{"sbt.version=1.9.9", "sbt.version", "1.9.9", true},
		{"sbt.version:1.9.9", "sbt.version", "1.9.9", true},
		{"sbt.version 1.9.9", "sbt.version", "1.9.9", true},
		{"sbt.version\t1.9.9", "sbt.version", "1.9.9", true},
		{"sbt.version : 1.9.9", "sbt.version", "1.9.9", true},
		{"sbt.version = 1.9.9", "sbt.version", "1.9.9", true},
		{"sbt.version= 1.9.9 ", "sbt.version", "1.9.9", true},
		{"sbt.version", "sbt.version", "", false},
		{"other.key:ignored", "other.key", "ignored", true},
	} {
		t.Run(tc.line, func(t *testing.T) {
			key, value, ok := splitPropertiesLine(tc.line)
			if key != tc.key || value != tc.value || ok != tc.ok {
				t.Fatalf("splitPropertiesLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.line, key, value, ok, tc.key, tc.value, tc.ok)
			}
		})
	}
}

func TestExtractBuildPropertiesEvidenceReadsSeparatorForms(t *testing.T) {
	for _, tc := range []struct {
		name, body, value string
		line              int
	}{
		{"equals", "sbt.version=1.9.9\n", "1.9.9", 1},
		{"colon", "sbt.version:1.9.9\n", "1.9.9", 1},
		{"whitespace", "sbt.version 1.9.9\n", "1.9.9", 1},
		{"multiline", "# managed by sbt\n# do not edit\n\nsbt.version : 1.9.9\n", "1.9.9", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := ExtractBuildPropertiesEvidence([]byte(tc.body))
			if evidence.State == "unavailable" || evidence.State == "over_cap" {
				t.Fatalf("state = %q, want collected evidence", evidence.State)
			}
			if len(evidence.Values) != 1 {
				t.Fatalf("values = %+v, want exactly one", evidence.Values)
			}
			got := evidence.Values[0]
			if got.SourceKind != "sbt_build_properties_version" || got.Value != tc.value || got.Line != tc.line {
				t.Fatalf("value = %+v, want kind=sbt_build_properties_version value=%s line=%d",
					got, tc.value, tc.line)
			}
		})
	}
}
