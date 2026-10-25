// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// versionCase is one row of the shared Gradle version table
// (../selector/testdata/versions.txt) every Gradle reader must agree with.
type versionCase struct {
	text  string
	exact bool
}

func loadSharedVersionTable(t *testing.T) []versionCase {
	t.Helper()
	file, err := os.Open("../selector/testdata/versions.txt")
	if err != nil {
		t.Fatalf("open shared version table: %v", err)
	}
	defer func() { _ = file.Close() }()
	cases := make([]versionCase, 0, 64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want, text, ok := strings.Cut(line, "\t")
		if !ok || (want != "exact" && want != "not-exact") {
			t.Fatalf("malformed shared version table line %q", line)
		}
		cases = append(cases, versionCase{text: text, exact: want == "exact"})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read shared version table: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("shared version table is empty")
	}
	return cases
}

// The catalog reader adopts the shared selector call and table
// (selector/README.md): every table entry resolves exactly when the table
// says exact, whether written as a literal version or named through a
// version.ref. A reference failure keys on the reference; a literal failure
// keys on the version.
func TestScanAgreesWithSharedVersionTable(t *testing.T) {
	t.Parallel()
	for _, tc := range loadSharedVersionTable(t) {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			text := "[versions]\nrefver = \"" + tc.text + "\"\n[libraries]\n" +
				"lit = { module = \"g:lit\", version = \"" + tc.text + "\" }\n" +
				"viaref = { module = \"g:viaref\", version.ref = \"refver\" }\n"
			result := Scan([]byte(text))
			if result.State != StateParsed {
				t.Fatalf("State = %q (%s), want parsed", result.State, result.ParseError)
			}
			lit := rowByAlias(t, result.Rows, "lit")
			if tc.exact {
				if lit["dependency_resolution_state"] != "resolved" || lit["value"] != tc.text {
					t.Fatalf("lit row = %#v, want resolved with value %q", lit, tc.text)
				}
			} else {
				requireUnresolved(t, lit, "version")
			}
			ref := rowByAlias(t, result.Rows, "viaref")
			if tc.exact {
				if ref["dependency_resolution_state"] != "resolved" || ref["value"] != tc.text {
					t.Fatalf("viaref row = %#v, want resolved with value %q", ref, tc.text)
				}
			} else {
				requireUnresolved(t, ref, "refver")
			}
		})
	}
}

// A ':' never belongs to a version: it is the string-notation separator, so
// colon-bearing text is a classifier or other notation text that leaked into
// the version. The shared rule rejects it, and the catalog reader must too,
// as a literal and through a version reference.
func TestScanColonVersionNeverResolves(t *testing.T) {
	t.Parallel()
	text := "[versions]\nrefver = \"1:2\"\n[libraries]\n" +
		"lit = { module = \"g:lit\", version = \"1:2\" }\n" +
		"viaref = { module = \"g:viaref\", version.ref = \"refver\" }\n"
	result := Scan([]byte(text))
	if result.State != StateParsed {
		t.Fatalf("State = %q (%s), want parsed", result.State, result.ParseError)
	}
	requireUnresolved(t, rowByAlias(t, result.Rows, "lit"), "version")
	requireUnresolved(t, rowByAlias(t, result.Rows, "viaref"), "refver")
	for _, row := range result.Rows {
		if value, _ := row["value"].(string); value != "" {
			t.Fatalf("row = %#v, want no resolved value", row)
		}
	}
}
