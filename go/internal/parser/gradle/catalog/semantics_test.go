// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"strings"
	"testing"
)

// rowByAlias returns the single row whose catalog_alias is alias.
func rowByAlias(t *testing.T, rows []map[string]any, alias string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, row := range rows {
		if row["catalog_alias"] == alias {
			if found != nil {
				t.Fatalf("two rows for alias %q", alias)
			}
			found = row
		}
	}
	if found == nil {
		t.Fatalf("no row for alias %q in %#v", alias, rows)
	}
	return found
}

// requireUnresolved fails unless row is unresolved, carries no value, and
// names wantKey as the blocking key.
func requireUnresolved(t *testing.T, row map[string]any, wantKey string) {
	t.Helper()
	if row["dependency_resolution_state"] != "unresolved" || row["value"] != "" {
		t.Fatalf("row = %#v, want unresolved with no value", row)
	}
	keys, _ := row["dependency_unresolved_keys"].([]string)
	if len(keys) != 1 || keys[0] != wantKey {
		t.Fatalf("dependency_unresolved_keys = %v, want [%s]", keys, wantKey)
	}
}

// Gradle normalizes '-', '_' and '.' in a version alias to '.', keeps the
// first registration and silently ignores the rest (registration runs in key
// length order, not file order). Two spellings of one alias are therefore one
// version in Gradle, so neither reference may resolve to a guessed text.
func TestScanVersionAliasCollisionNeverResolves(t *testing.T) {
	t.Parallel()
	text := "[versions]\na-b = \"1.0\"\na_b = \"9.9\"\nsolo = \"2.0\"\n" +
		"[libraries]\nx = { module = \"g:x\", version.ref = \"a-b\" }\n" +
		"y = { module = \"g:y\", version.ref = \"a_b\" }\n" +
		"z = { module = \"g:z\", version.ref = \"a.b\" }\n" +
		"w = { module = \"g:w\", version.ref = \"solo\" }\n"
	result := Scan([]byte(text))
	if result.State != StateParsed {
		t.Fatalf("State = %q (%s), want parsed", result.State, result.ParseError)
	}
	for _, alias := range []string{"x", "y", "z"} {
		row := rowByAlias(t, result.Rows, alias)
		requireUnresolved(t, row, row["version_ref"].(string))
	}
	if row := rowByAlias(t, result.Rows, "w"); row["dependency_resolution_state"] != "resolved" || row["value"] != "2.0" {
		t.Fatalf("non-colliding ref row = %#v, want resolved 2.0", row)
	}
	if result.Counts.Collided != 2 || result.Counts.Unresolved != 3 || result.Counts.Resolved != 1 {
		t.Fatalf("Counts = %+v, want Collided=2 Unresolved=3 Resolved=1", result.Counts)
	}
}

// Gradle normalizes a version.ref the same way, so a reference spelled with a
// different separator still finds a single, non-colliding definition.
func TestScanVersionRefIsNormalizedBeforeLookup(t *testing.T) {
	t.Parallel()
	text := "[versions]\nkotlin_core = \"1.9.22\"\n[libraries]\nk = { module = \"g:k\", version.ref = \"kotlin-core\" }\n"
	result := Scan([]byte(text))
	row := rowByAlias(t, result.Rows, "k")
	if row["dependency_resolution_state"] != "resolved" || row["value"] != "1.9.22" || row["version_ref"] != "kotlin-core" {
		t.Fatalf("row = %#v, want resolved 1.9.22 with version_ref as written", row)
	}
}

// For libraries and plugins Gradle replaces the earlier entry with the later
// one. The parser does not guess which declaration survives: every colliding
// row is unresolved, keyed by catalog_alias, and counted.
func TestScanLibraryAndPluginAliasCollisionsAreUnresolved(t *testing.T) {
	t.Parallel()
	text := "[libraries]\ndup-lib = \"com.col:a:1.0\"\ndup_lib = \"com.col:b:2.0\"\n\"dup.lib\" = \"com.col:c:3.0\"\nok = \"com.col:d:4.0\"\n" +
		"[plugins]\np-x = \"org.p.one:1.0\"\np_x = { id = \"org.p.two\", version = \"2.0\" }\n" +
		"[bundles]\nb-1 = [\"ok\"]\nb_1 = [\"ok\"]\n"
	result := Scan([]byte(text))
	if result.State != StateParsed {
		t.Fatalf("State = %q (%s), want parsed", result.State, result.ParseError)
	}
	for _, alias := range []string{"dup-lib", "dup_lib", "dup.lib", "p-x", "p_x"} {
		requireUnresolved(t, rowByAlias(t, result.Rows, alias), "catalog_alias")
	}
	if row := rowByAlias(t, result.Rows, "ok"); row["dependency_resolution_state"] != "resolved" {
		t.Fatalf("non-colliding row = %#v, want resolved", row)
	}
	want := Counts{Libraries: 4, Plugins: 2, Bundles: 2, Resolved: 1, Unresolved: 5, Collided: 7}
	if result.Counts != want {
		t.Fatalf("Counts = %+v, want %+v", result.Counts, want)
	}
}

// An entry past the row cap is not emitted, but Gradle would still register
// it, so a dropped alias that collides with an emitted one must still make the
// emitted row unresolved.
func TestScanDroppedAliasStillMarksACollision(t *testing.T) {
	t.Parallel()
	source := catalogWithEntries(MaxRows)
	source = append(source, []byte("a_0 = \"g:late:9\"\n")...)
	result := Scan(source)
	if result.Counts.Dropped != 1 || len(result.Rows) != MaxRows {
		t.Fatalf("rows=%d Counts=%+v, want %d rows and one dropped", len(result.Rows), result.Counts, MaxRows)
	}
	// catalogWithEntries names aliases a0, a1, ...; "a_0" normalizes to "a.0",
	// which does not collide with "a0". Use a second fixture that does.
	source = []byte("[libraries]\n")
	for i := 0; i < MaxRows; i++ {
		source = append(source, []byte("a-"+itoa(i)+" = \"g:a"+itoa(i)+":1\"\n")...)
	}
	source = append(source, []byte("a_7 = \"g:late:9\"\n")...)
	result = Scan(source)
	if result.Counts.Dropped != 1 || result.Counts.Collided != 2 {
		t.Fatalf("Counts = %+v, want one dropped and two collided aliases", result.Counts)
	}
	requireUnresolved(t, rowByAlias(t, result.Rows, "a-7"), "catalog_alias")
	if row := rowByAlias(t, result.Rows, "a-8"); row["dependency_resolution_state"] != "resolved" {
		t.Fatalf("a-8 = %#v, want resolved", row)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for ; i > 0; i /= 10 {
		n--
		b[n] = byte('0' + i%10)
	}
	return string(b[n:])
}

// A dynamic selector, a range, a strict marker or an artifact-type suffix is
// not one version. Gradle reads "1.0!!" as strictly 1.0 and "1.0@jar" as
// version 1.0 with type jar, so the text as written is never the version.
func TestScanDynamicSelectorsAreNeverResolved(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{
		"1.+", "+", "latest.release", "latest.integration", "Latest.Milestone",
		"1.0!!", "[1.0,2.0)!!1.5", "1.0!", "1.0@jar", "[1.0,2.0)", "(,2.0]", "]1.0,2.0[", "1.0,2.0",
	} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			text := "[versions]\nv = \"" + selector + "\"\n[libraries]\n" +
				"lit = { module = \"g:lit\", version = \"" + selector + "\" }\n" +
				"ref = { module = \"g:ref\", version.ref = \"v\" }\n" +
				"str = \"g:str:" + selector + "\"\n" +
				"[plugins]\npl = \"org.p:" + selector + "\"\n"
			result := Scan([]byte(text))
			if result.State != StateParsed {
				t.Fatalf("State = %q (%s), want parsed", result.State, result.ParseError)
			}
			requireUnresolved(t, rowByAlias(t, result.Rows, "lit"), "version")
			requireUnresolved(t, rowByAlias(t, result.Rows, "ref"), "v")
			// "1.0,2.0" and similar have no ':' so the string notation still
			// splits into three parts.
			requireUnresolved(t, rowByAlias(t, result.Rows, "str"), "version")
			requireUnresolved(t, rowByAlias(t, result.Rows, "pl"), "version")
		})
	}
}

// Gradle requires exactly group:artifact:version for a library string and
// id:version for a plugin string; any other part count fails the build.
func TestScanStringNotationNeedsTheExactPartCount(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"two part library":  "[libraries]\na = \"g:a\"\n",
		"one part library":  "[libraries]\na = \"g\"\n",
		"one part plugin":   "[plugins]\np = \"org.p\"\n",
		"three part plugin": "[plugins]\np = \"org.p:1:2\"\n",
	} {
		result := Scan([]byte(text))
		if len(result.Rows) != 0 || result.Counts.Unsupported != 1 {
			t.Errorf("%s: rows=%d Counts=%+v, want no row and one unsupported", name, len(result.Rows), result.Counts)
		}
	}
}

// The streaming TOML parser does no semantic checks. A file TOML itself
// rejects, such as a table defined by dotted keys and then by a header, must
// yield no rows, the same as a syntax error.
func TestScanTOMLInvalidTableRedefinitionYieldsNoRows(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"dotted then header": "libraries.z = \"g:z:1\"\n[libraries]\na = \"g:a:1\"\n",
		"root key twice":     "a = 1\na = 2\n[libraries]\nb = \"g:b:1\"\n",
	} {
		result := Scan([]byte(text))
		if result.State != StateMalformed || len(result.Rows) != 0 || result.ParseError == "" {
			t.Errorf("%s: State=%q rows=%d err=%q, want malformed with none", name, result.State, len(result.Rows), result.ParseError)
		}
	}
}

// hyphenCatalog writes n libraries whose aliases all take the normalization
// path ("a-0", "a-1", ...).
func hyphenCatalog(n int) []byte {
	var b strings.Builder
	b.WriteString("[libraries]\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "a-%d = \"g:a%d:1\"\n", i, i)
	}
	return []byte(b.String())
}

// The normalized alias tables hold at most MaxRows aliases per table. An
// alias past the cap is only looked up, so allocation stops growing with the
// entry count once the cap is reached.
func TestScanAliasNormalizationStaysCappedByMaxRows(t *testing.T) {
	source := hyphenCatalog(80_000)
	if len(source) > MaxSourceBytes {
		t.Fatalf("test input %d bytes exceeds MaxSourceBytes", len(source))
	}
	var result Result
	full, _ := allocOf(func() { result = Scan(source) })
	if len(result.Rows) != MaxRows || result.Counts.Dropped != 80_000-MaxRows || result.Counts.Collided != 0 {
		t.Fatalf("rows=%d Counts=%+v, want %d rows, %d dropped, no collision",
			len(result.Rows), result.Counts, MaxRows, 80_000-MaxRows)
	}
	requireCeiling(t, "bytes allocated for 80k normalized aliases", full, MaxRows*3072+uint64(len(source))/4)
	half, _ := allocOf(func() { Scan(hyphenCatalog(40_000)) })
	if ratio := float64(full) / float64(half); ratio > 1.25 {
		t.Fatalf("doubling entries past the cap grew allocation %.2fx, want <= 1.25x", ratio)
	}
}

// The semantic TOML check decodes into an empty struct. A valid value full of
// escapes is unescaped by both passes, so allocation stays a small multiple
// of the input and never grows with anything but the input.
func TestScanSemanticCheckStaysLinearOnEscapedValues(t *testing.T) {
	source := []byte("[libraries]\na = \"" + strings.Repeat("\\n", 900_000) + "\"\n")
	var result Result
	bytesAllocated, _ := allocOf(func() { result = Scan(source) })
	if result.State != StateParsed || result.Counts.Unsupported != 1 {
		t.Fatalf("State=%q Counts=%+v, want parsed with one unsupported", result.State, result.Counts)
	}
	requireCeiling(t, "bytes allocated for a 1.8 MB escaped value", bytesAllocated, 3*uint64(len(source)))
}

// An exact repeat stays a malformed file even past the row cap: the repeated
// key is invalid TOML wherever it appears.
func TestScanExactRepeatPastTheCapIsMalformed(t *testing.T) {
	t.Parallel()
	source := append(hyphenCatalog(MaxRows), []byte("a-3 = \"g:again:2\"\n")...)
	if result := Scan(source); result.State != StateMalformed || len(result.Rows) != 0 {
		t.Fatalf("State=%q rows=%d, want malformed with none", result.State, len(result.Rows))
	}
}
