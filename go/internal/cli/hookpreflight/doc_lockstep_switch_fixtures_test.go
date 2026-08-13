// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture drive for the closed-switch scanner in doc_lockstep_switch_test.go.
// The scanner reports findings rather than failing, so the same code that reads
// triggerAllowed can be run over throwaway packages here and proved to say what
// it should. Splitting the drive out of the scanner keeps both files well under
// the repo's 500-line cap as the evasion table grows.

// writeSwitchFixture writes body as the sole non-test file of a fresh
// directory, next to a _test.go file the scanner must ignore.
func writeSwitchFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte("package fixture\n\n"+body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ignored := "package fixture\n\nfunc allowed(v string) bool {\n\treturn v == \"from_a_test_file\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(ignored), 0o600); err != nil {
		t.Fatalf("write ignored fixture: %v", err)
	}
	return dir
}

// TestDocLockstepSwitchScannerReadsTheClosedShape proves the scanner reads the
// literals out of a well-formed closed switch, skips _test.go files, and errors
// on a missing function rather than returning an empty set a caller could read
// as an empty allow-list.
//
// It also proves the tag check leaves a case-folding, whitespace-trimming tag
// alone. That shape is a legitimate hardening of triggerAllowed, and a guard
// that refused it would push the same normalization into normalizeInput --
// which is the remap E2 used to widen the accepted set with triggerAllowed
// byte-identical.
func TestDocLockstepSwitchScannerReadsTheClosedShape(t *testing.T) {
	t.Parallel()

	dir := writeSwitchFixture(t, "func allowed(v string) bool {\n\tswitch v {\n"+
		"\tcase \"read\", \"list\":\n\t\treturn true\n"+
		"\tcase \"symbol\":\n\t\treturn true\n"+
		"\tdefault:\n\t\treturn false\n\t}\n}\n")

	clauses, values, findings, err := scanClosedStringSwitch(dir, "allowed")
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none for a well-formed closed switch", findings)
	}
	if clauses != 2 {
		t.Fatalf("true-returning clauses = %d, want 2", clauses)
	}
	if strings.Join(values, ",") != "list,read,symbol" {
		t.Fatalf("values = %v, want [list read symbol]", values)
	}
	if _, _, _, err := scanClosedStringSwitch(dir, "notThere"); err == nil {
		t.Fatal("scanning a missing function returned no error; a caller could read the empty set as an empty allow-list")
	}

	normalizing := writeSwitchFixture(t,
		"import \"strings\"\n\nfunc allowed(v string) bool {\n"+
			"\tswitch strings.ToLower(strings.TrimSpace(v)) {\n"+
			"\tcase \"read\":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n")
	_, _, findings, err = scanClosedStringSwitch(normalizing, "allowed")
	if err != nil {
		t.Fatalf("scan normalizing fixture: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v for a tag of strings.ToLower(strings.TrimSpace(v)); case-folding and trimming cannot map one class onto another, so this shape must stay allowed", findings)
	}
}

// TestDocLockstepSwitchScannerRefusesADecoyDeclaration proves the two halves of
// the build-excluded-decoy fix. An `aliases.go` carrying `//go:build ignore`
// and a pristine triggerAllowed sorts ahead of preflight.go, and the old
// scanner returned the first match in sorted filename order -- so it read the
// decoy's allow-list while `go build` compiled the widened one next door.
// parseNonTestGoFiles now drops build-constrained files, so the scan reads the
// real declaration; and two unconstrained declarations of one name are an
// error rather than a silent first-match.
func TestDocLockstepSwitchScannerRefusesADecoyDeclaration(t *testing.T) {
	t.Parallel()

	const widened = "func allowed(v string) bool {\n\tswitch v {\n" +
		"\tcase \"read\", \"list\":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n"
	const pristine = "func allowed(v string) bool {\n\tswitch v {\n" +
		"\tcase \"read\":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n"

	excluded := writeSwitchFixture(t, widened)
	if err := os.WriteFile(filepath.Join(excluded, "aliases.go"), []byte("//go:build ignore\n\npackage fixture\n\n"+pristine), 0o600); err != nil {
		t.Fatalf("write build-excluded decoy: %v", err)
	}
	_, values, findings, err := scanClosedStringSwitch(excluded, "allowed")
	if err != nil {
		t.Fatalf("scan excluded-decoy fixture: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none; the compiled declaration is well-formed", findings)
	}
	if strings.Join(values, ",") != "list,read" {
		t.Fatalf("values = %v, want the compiled [list read]; a //go:build-excluded file answered for the real one", values)
	}
	if _, _, constrained, parseErr := parseNonTestGoFiles(excluded); parseErr != nil || strings.Join(constrained, ",") != "aliases.go" {
		t.Fatalf("constrained = %v (err %v), want [aliases.go] so a real constrained file cannot hide instead", constrained, parseErr)
	}

	duplicated := writeSwitchFixture(t, widened)
	if err := os.WriteFile(filepath.Join(duplicated, "aliases.go"), []byte("package fixture\n\n"+pristine), 0o600); err != nil {
		t.Fatalf("write duplicate decoy: %v", err)
	}
	if _, _, _, err := scanClosedStringSwitch(duplicated, "allowed"); err == nil {
		t.Fatal("two unconstrained files declaring allowed returned no error; the scanner would report whichever sorts first")
	}
}

// TestDocLockstepSwitchScannerRejectsEvasions is the negative half. The first
// four bodies are the rewrites of triggerAllowed that accept "list" while the
// old bare-`return true` scanner saw nothing; the rest are the neighbouring
// shapes that express the same change differently. Each must produce a finding.
//
// Two of the bodies deliberately do not compile (a function that can fall off
// the end). The scanner only parses, and a fixture that has to compile could
// not isolate a missing default from the extra statement Go would require.
func TestDocLockstepSwitchScannerRejectsEvasions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "guard_before_switch",
			body: "func allowed(v string) bool {\n\tif v == \"list\" {\n\t\treturn true\n\t}\n" +
				"\tswitch v {\n\tcase \"read\":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "conditional_inside_case",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"list\":\n\t\tif v != \"\" {\n\t\t\treturn true\n\t\t}\n\t\treturn false\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "variable_returned_from_case",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"list\":\n\t\tok := true\n\t\treturn ok\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "expression_returned_from_case",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"list\":\n\t\treturn v != \"\"\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "tagless_switch",
			body: "func allowed(v string) bool {\n\tswitch {\n\tcase v == \"read\" || v == \"list\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "switch_init_statement",
			body: "func allowed(v string) bool {\n\tswitch t := v; t {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "case_returns_false",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"edit\":\n\t\treturn false\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "default_returns_true",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn true\n\t}\n}\n",
		},
		{
			name: "no_default_clause",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\", \"list\":\n\t\treturn true\n\t}\n}\n",
		},
		{
			name: "statement_after_switch",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\tbreak\n\t}\n\treturn v == \"list\"\n}\n",
		},
		{
			name: "tag_is_a_canonicalizing_helper",
			body: "func canonical(v string) string {\n\tif v == \"list\" {\n\t\treturn \"read\"\n\t}\n\treturn v\n}\n\n" +
				"func allowed(v string) bool {\n\tswitch canonical(v) {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "tag_is_a_non_normalizing_strings_call",
			body: "import \"strings\"\n\nfunc allowed(v string) bool {\n" +
				"\tswitch strings.Replace(v, \"list\", \"read\", 1) {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "tag_is_not_the_parameter",
			body: "var override = \"read\"\n\nfunc allowed(v string) bool {\n\tswitch override {\n" +
				"\tcase \"read\":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "function_takes_no_parameter",
			body: "func allowed() bool {\n\tswitch \"list\" {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
	}
	if len(cases) < 14 {
		t.Fatalf("evasion cases = %d, want one per shape that expresses a widened allow-list", len(cases))
	}

	checked := 0
	for _, tc := range cases {
		dir := writeSwitchFixture(t, tc.body)
		_, values, findings, err := scanClosedStringSwitch(dir, "allowed")
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		if len(findings) == 0 {
			t.Errorf("%s: scanner reported no finding and read the accepted set as %v; this shape widens the allow-list invisibly", tc.name, values)
		}
		checked++
	}
	if checked != len(cases) {
		t.Fatalf("scanned %d of %d evasion fixtures", checked, len(cases))
	}
}
