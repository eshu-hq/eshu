// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"testing"
)

const positiveCatalog = `# comment
[versions]
groovy = "3.0.5"
spring = "6.1.4"
guava-range = "[31.0,32.0)"
strict = { strictly = "1.2.3", prefer = "1.2.5" }

[libraries]
groovy-core = { module = "org.codehaus.groovy:groovy", version.ref = "groovy" }
spring-core = { group = "org.springframework", name = "spring-core", version = { ref = "spring" } }
guava = "com.google.guava:guava:33.0.0-jre"
junit-bom = "org.junit:junit-bom"
commons = { module = "commons-io:commons-io", version = "2.15.1" }
unversioned = { module = "org.slf4j:slf4j-api" }
ranged = { module = "com.google.guava:failureaccess", version.ref = "guava-range" }

[bundles]
groovy = ["groovy-core"]

[plugins]
spotless = { id = "com.diffplug.spotless", version.ref = "spring" }
short = "org.jetbrains.kotlin.jvm:1.9.22"
bare = { id = "application" }
`

func rowByName(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("no row named %q in %d rows", name, len(rows))
	return nil
}

func TestScanPositiveCatalogEmitsDocumentedRows(t *testing.T) {
	t.Parallel()

	result := Scan([]byte(positiveCatalog))
	if result.State != StateParsed {
		t.Fatalf("State = %q (%s), want %q", result.State, result.ParseError, StateParsed)
	}

	type want struct {
		name, value, state, section, kind string
		line                              int
	}
	cases := []want{
		{"org.codehaus.groovy:groovy", "3.0.5", "resolved", "libraries", "dependency", 9},
		{"org.springframework:spring-core", "6.1.4", "resolved", "libraries", "dependency", 10},
		{"com.google.guava:guava", "33.0.0-jre", "resolved", "libraries", "dependency", 11},
		{"org.junit:junit-bom", "", "partial", "libraries", "dependency", 12},
		{"commons-io:commons-io", "2.15.1", "resolved", "libraries", "dependency", 13},
		{"org.slf4j:slf4j-api", "", "partial", "libraries", "dependency", 14},
		{"com.google.guava:failureaccess", "[31.0,32.0)", "resolved", "libraries", "dependency", 15},
		{"com.diffplug.spotless", "6.1.4", "resolved", "plugins", "gradle_plugin", 21},
		{"org.jetbrains.kotlin.jvm", "1.9.22", "resolved", "plugins", "gradle_plugin", 22},
		{"application", "", "partial", "plugins", "gradle_plugin", 23},
	}
	if len(result.Rows) != len(cases) {
		t.Fatalf("rows = %d, want %d: %#v", len(result.Rows), len(cases), result.Rows)
	}
	for _, c := range cases {
		row := rowByName(t, result.Rows, c.name)
		if row["value"] != c.value || row["dependency_resolution_state"] != c.state ||
			row["section"] != c.section || row["config_kind"] != c.kind || row["line_number"] != c.line {
			t.Errorf("%s row = %#v, want value=%q state=%q section=%q kind=%q line=%d",
				c.name, row, c.value, c.state, c.section, c.kind, c.line)
		}
		if row["package_manager"] != "gradle" || row["lang"] != "gradle" {
			t.Errorf("%s package_manager/lang = %v/%v, want gradle/gradle", c.name, row["package_manager"], row["lang"])
		}
		if _, set := row["direct_dependency"]; set {
			t.Errorf("%s sets direct_dependency: a catalog entry does not prove a direct dependency", c.name)
		}
	}
	if got := rowByName(t, result.Rows, "org.codehaus.groovy:groovy")["catalog_alias"]; got != "groovy-core" {
		t.Errorf("catalog_alias = %v, want groovy-core", got)
	}
	if got := rowByName(t, result.Rows, "org.codehaus.groovy:groovy")["version_ref"]; got != "groovy" {
		t.Errorf("version_ref = %v, want groovy", got)
	}

	wantCounts := Counts{Versions: 4, Libraries: 7, Plugins: 3, Bundles: 1, Resolved: 7, Partial: 3}
	if result.Counts != wantCounts {
		t.Errorf("Counts = %+v, want %+v", result.Counts, wantCounts)
	}
}

func TestScanNegativeFixturesNeverResolveAGuess(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		text        string
		wantRows    int
		wantState   string // dependency_resolution_state of the single row, "" when no row
		wantUnsupp  int
		wantUnresol int
		wantKeys    []string
	}{
		{
			name:      "ref to a missing version key",
			text:      "[libraries]\na = { module = \"g:a\", version.ref = \"nope\" }\n",
			wantRows:  1,
			wantState: "unresolved", wantUnresol: 1, wantKeys: []string{"nope"},
		},
		{
			name:      "ref to a rich version table",
			text:      "[versions]\nv = { strictly = \"1.0\" }\n[libraries]\na = { module = \"g:a\", version.ref = \"v\" }\n",
			wantRows:  1,
			wantState: "unresolved", wantUnresol: 1, wantKeys: []string{"v"},
		},
		{
			name:      "inline rich version",
			text:      "[libraries]\na = { module = \"g:a\", version = { strictly = \"1.0\" } }\n",
			wantRows:  1,
			wantState: "unresolved", wantUnresol: 1, wantKeys: []string{"version"},
		},
		{
			name:      "dotted rich version key",
			text:      "[libraries]\na = { module = \"g:a\", version.require = \"1.0\" }\n",
			wantRows:  1,
			wantState: "unresolved", wantUnresol: 1, wantKeys: []string{"version"},
		},
		{
			name:      "version text with an interpolation character",
			text:      "[libraries]\na = { module = \"g:a\", version = \"${v}\" }\n",
			wantRows:  1,
			wantState: "unresolved", wantUnresol: 1, wantKeys: []string{"version"},
		},
		{
			name:     "sub-table library header",
			text:     "[libraries.a]\nmodule = \"g:a\"\nversion = \"1\"\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "array of tables",
			text:     "[[libraries]]\nmodule = \"g:a\"\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "root level key",
			text:     "libraries = 1\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "dotted alias",
			text:     "[libraries]\na.b = \"g:a:1\"\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "unknown key in a library table",
			text:     "[libraries]\na = { module = \"g:a\", version = \"1\", classifier = \"x\" }\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "module and group together",
			text:     "[libraries]\na = { module = \"g:a\", group = \"g\", name = \"a\" }\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "four part string",
			text:     "[libraries]\na = \"g:a:1:jar\"\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "module with whitespace",
			text:     "[libraries]\na = { module = \"g :a\" }\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "non-string module",
			text:     "[libraries]\na = { module = 3 }\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "library value is an array",
			text:     "[libraries]\na = [\"g:a:1\"]\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "plugin without id",
			text:     "[plugins]\np = { version = \"1\" }\n",
			wantRows: 0, wantUnsupp: 1,
		},
		{
			name:     "unknown table",
			text:     "[metadata]\nformat.version = \"1.1\"\n[libraries]\na = \"g:a:1\"\n",
			wantRows: 1, wantState: "resolved", wantUnsupp: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := Scan([]byte(tc.text))
			if result.State != StateParsed {
				t.Fatalf("State = %q (%s), want parsed", result.State, result.ParseError)
			}
			if len(result.Rows) != tc.wantRows {
				t.Fatalf("rows = %d, want %d: %#v", len(result.Rows), tc.wantRows, result.Rows)
			}
			if result.Counts.Unsupported != tc.wantUnsupp || result.Counts.Unresolved != tc.wantUnresol {
				t.Fatalf("Counts = %+v, want Unsupported=%d Unresolved=%d", result.Counts, tc.wantUnsupp, tc.wantUnresol)
			}
			if tc.wantRows == 0 {
				return
			}
			row := result.Rows[0]
			if row["dependency_resolution_state"] != tc.wantState {
				t.Fatalf("state = %v, want %q", row["dependency_resolution_state"], tc.wantState)
			}
			if tc.wantState == "unresolved" {
				if row["value"] != "" {
					t.Fatalf("unresolved row carries value %v: never a guessed version", row["value"])
				}
				keys, _ := row["dependency_unresolved_keys"].([]string)
				if len(keys) != len(tc.wantKeys) || keys[0] != tc.wantKeys[0] {
					t.Fatalf("dependency_unresolved_keys = %v, want %v", keys, tc.wantKeys)
				}
			}
		})
	}
}

func TestScanMalformedAndDuplicateInputYieldNoRows(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"syntax error":           "[libraries\na = \"g:a:1\"\n",
		"unterminated string":    "[libraries]\na = \"g:a:1\n",
		"duplicate table header": "[libraries]\na = \"g:a:1\"\n[libraries]\nb = \"g:b:1\"\n",
		"duplicate alias":        "[libraries]\na = \"g:a:1\"\na = \"g:b:2\"\n",
		"duplicate version key":  "[versions]\nv = \"1\"\nv = \"2\"\n[libraries]\na = { module = \"g:a\", version.ref = \"v\" }\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := Scan([]byte(text))
			if result.State != StateMalformed {
				t.Fatalf("State = %q, want %q", result.State, StateMalformed)
			}
			if len(result.Rows) != 0 {
				t.Fatalf("malformed input produced rows: %#v", result.Rows)
			}
			if result.ParseError == "" {
				t.Fatal("ParseError is empty")
			}
		})
	}
}

func TestScanEmptyAndCommentOnlyInputIsParsedWithNoRows(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "\n", "# only a comment\n", "[versions]\n[libraries]\n"} {
		result := Scan([]byte(text))
		if result.State != StateParsed || len(result.Rows) != 0 {
			t.Fatalf("Scan(%q) = %q with %d rows, want parsed with none", text, result.State, len(result.Rows))
		}
	}
}

func TestScanReportsRealLineNumbersAcrossMultilineValues(t *testing.T) {
	t.Parallel()
	text := "[libraries]\nnote = \"\"\"\nline\nline\n\"\"\"\n"
	result := Scan([]byte(text))
	if result.Counts.Unsupported != 1 || len(result.Rows) != 0 {
		t.Fatalf("multiline string library: rows=%d counts=%+v, want one unsupported", len(result.Rows), result.Counts)
	}
	text = "[libraries]\nx = [\n  \"a\",\n  \"b\",\n]\nlast = \"g:last:1\"\n"
	result = Scan([]byte(text))
	if len(result.Rows) != 1 || result.Rows[0]["line_number"] != 6 {
		t.Fatalf("rows after a multiline array = %#v, want one row at line 6", result.Rows)
	}
}
