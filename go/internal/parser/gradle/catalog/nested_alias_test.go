// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"strings"
	"testing"
)

// scanParsed scans text and fails unless the file parsed.
func scanParsed(t *testing.T, text string) Result {
	t.Helper()
	result := Scan([]byte(text))
	if result.State != StateParsed {
		t.Fatalf("State = %q (%s), want parsed\n%s", result.State, result.ParseError, text)
	}
	return result
}

// Gradle walks each catalog table's keySet, so a dotted entry
// (alias.module = ..., alias.version = ...) and a [libraries.alias] sub-table
// register alias exactly like a one-segment key. The parser emits no row for
// either form, but the alias still collides with any other spelling of it:
// Gradle replaces one library with the other, so the plain row must not
// resolve to a version the build may not use.
func TestScanNestedLibraryAliasCollidesWithItsOtherSpellings(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"dotted after":     "[libraries]\na-b = \"g:a:1.0\"\na_b.module = \"g:other\"\na_b.version = \"2.0\"\n",
		"dotted before":    "[libraries]\na_b.module = \"g:other\"\na_b.version = \"2.0\"\na-b = \"g:a:1.0\"\n",
		"sub-table after":  "[libraries]\na-b = \"g:a:1.0\"\n[libraries.a_b]\nmodule = \"g:other\"\nversion = \"2.0\"\n",
		"sub-table before": "[libraries.a_b]\nmodule = \"g:other\"\nversion = \"2.0\"\n[libraries]\na-b = \"g:a:1.0\"\n",
		"array table":      "[libraries]\na-b = \"g:a:1.0\"\n[[libraries.a_b]]\nmodule = \"g:other\"\n",
		"deep dotted":      "[libraries]\na-b = \"g:a:1.0\"\na_b.version.ref = \"v\"\na_b.module = \"g:other\"\n",
		"deep sub-table":   "[libraries]\na-b = \"g:a:1.0\"\n[libraries.a_b.version]\nref = \"v\"\n",
	} {
		result := scanParsed(t, text)
		if len(result.Rows) != 1 {
			t.Fatalf("%s: %d rows, want only a-b", name, len(result.Rows))
		}
		requireUnresolved(t, rowByAlias(t, result.Rows, "a-b"), collisionKey)
		if result.Counts.Collided != 2 {
			t.Errorf("%s: Collided = %d, want 2", name, result.Counts.Collided)
		}
	}
}

// In [versions] Gradle keeps the first registration of a normalized alias,
// and it registers in key-length order: v-x (from v-x.strictly) and v_x tie
// on length, so the rich v-x may be the version Gradle uses. A reference to
// either spelling must stay unresolved.
func TestScanNestedVersionAliasMakesItsReferenceUnresolved(t *testing.T) {
	t.Parallel()
	lib := "[libraries]\nlib = { module = \"g:a\", version.ref = \"v_x\" }\n"
	for name, text := range map[string]string{
		"dotted after":     "[versions]\nv_x = \"1.0\"\nv-x.strictly = \"2.0\"\n" + lib,
		"dotted before":    "[versions]\nv-x.strictly = \"2.0\"\nv_x = \"1.0\"\n" + lib,
		"sub-table after":  "[versions]\nv_x = \"1.0\"\n" + lib + "[versions.v-x]\nstrictly = \"2.0\"\n",
		"sub-table before": "[versions.v-x]\nstrictly = \"2.0\"\n[versions]\nv_x = \"1.0\"\n" + lib,
	} {
		result := scanParsed(t, text)
		requireUnresolved(t, rowByAlias(t, result.Rows, "lib"), "v_x")
		if result.Counts.Collided != 2 {
			t.Errorf("%s: Collided = %d, want 2", name, result.Counts.Collided)
		}
	}
}

func TestScanNestedPluginAliasCollidesWithItsOtherSpellings(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"dotted":    "[plugins]\np-x.id = \"org.p\"\np_x = \"org.q:1.0\"\n",
		"sub-table": "[plugins]\np_x = \"org.q:1.0\"\n[plugins.p-x]\nid = \"org.p\"\n",
	} {
		result := scanParsed(t, text)
		requireUnresolved(t, rowByAlias(t, result.Rows, "p_x"), collisionKey)
		if result.Counts.Collided != 2 {
			t.Errorf("%s: Collided = %d, want 2", name, result.Counts.Collided)
		}
	}
}

func TestScanNestedBundleAliasCountsItsCollision(t *testing.T) {
	t.Parallel()
	result := scanParsed(t, "[bundles]\nb-x = [\"a\"]\n[bundles.b_x]\nc = 1\n")
	if result.Counts.Collided != 2 {
		t.Errorf("Collided = %d, want 2", result.Counts.Collided)
	}
}

// One alias written as several dotted keys, or as a sub-table with its own
// sub-table, is one alias: it must not collide with itself, make the file
// malformed, or change any other row.
func TestScanNestedAliasRepeatedInOneSpellingIsOneAlias(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"dotted":    "[libraries]\na_b.module = \"g:other\"\na_b.version = \"2.0\"\nc = \"g:c:1.0\"\n",
		"sub-table": "[libraries]\nc = \"g:c:1.0\"\n[libraries.a_b]\nmodule = \"g:other\"\n[libraries.a_b.version]\nref = \"v\"\n",
		"versions":  "[versions]\nv.strictly = \"1.0\"\nv.prefer = \"1.1\"\n[libraries]\nc = \"g:c:1.0\"\n",
	} {
		result := scanParsed(t, text)
		if len(result.Rows) != 1 {
			t.Fatalf("%s: %d rows, want only c", name, len(result.Rows))
		}
		row := rowByAlias(t, result.Rows, "c")
		if row["dependency_resolution_state"] != "resolved" || row["value"] != "1.0" {
			t.Errorf("%s: c = %#v, want resolved 1.0", name, row)
		}
		if result.Counts.Collided != 0 {
			t.Errorf("%s: Collided = %d, want 0", name, result.Counts.Collided)
		}
	}
}

// A nested alias that only names a rich version (v.strictly) is still not a
// plain version, so a reference to it stays unresolved.
func TestScanReferenceToANestedRichVersionIsUnresolved(t *testing.T) {
	t.Parallel()
	result := scanParsed(t, "[versions]\nv.strictly = \"1.0\"\n[libraries]\nlib = { module = \"g:a\", version.ref = \"v\" }\n")
	requireUnresolved(t, rowByAlias(t, result.Rows, "lib"), "v")
}

// Gradle registers an alias longer than the field bound too. Without a
// separator it can share an accessor name with a shorter alias.
func TestScanOverlongAliasStillMarksACollision(t *testing.T) {
	t.Parallel()
	stem := strings.Repeat("a", MaxFieldBytes-1)
	short := stem + "B" // MaxFieldBytes bytes, emits a row
	long := stem + "-b" // one byte over the bound, no row
	for name, text := range map[string]string{
		"long after":  "[libraries]\n" + short + " = \"g:a:1.0\"\n" + long + " = \"g:b:1.0\"\n",
		"long before": "[libraries]\n" + long + " = \"g:b:1.0\"\n" + short + " = \"g:a:1.0\"\n",
	} {
		result := scanParsed(t, text)
		requireUnresolved(t, rowByAlias(t, result.Rows, short), collisionKey)
		if result.Counts.Collided != 2 {
			t.Errorf("%s: Collided = %d, want 2", name, result.Counts.Collided)
		}
	}
}

// Past the row cap a nested alias still marks a collision with a kept alias,
// and its own repeats (alias.module, alias.version) count once.
func TestScanNestedAliasPastTheCapMarksACollisionOnce(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("[libraries]\n")
	for i := 0; i < MaxRows; i++ {
		b.WriteString("a" + itoa(i) + " = \"g:a" + itoa(i) + ":1.0\"\n")
	}
	b.WriteString("a_0.module = \"g:x\"\na_0.version = \"2.0\"\n")
	result := scanParsed(t, b.String())
	requireUnresolved(t, rowByAlias(t, result.Rows, "a0"), collisionKey)
	if result.Counts.Collided != 2 {
		t.Errorf("Collided = %d, want 2", result.Counts.Collided)
	}
}

// Every nested spelling the table keeps counts toward MaxRows, like a
// one-segment alias does, so the table stays bounded: after MaxRows nested
// spellings of one alias, the next alias is dropped.
func TestScanNestedSpellingsCountTowardTheCap(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("[libraries]\n")
	seps := []byte{'-', '_'}
	for i := 0; i < MaxRows; i++ {
		key := []byte("a")
		for v, k := i, 0; k < 13; k++ {
			key = append(key, seps[v%2], 'a')
			v /= 2
		}
		b.WriteString(string(key) + ".module = \"g:a\"\n")
	}
	b.WriteString("z = \"g:z:1.0\"\n")
	result := scanParsed(t, b.String())
	if len(result.Rows) != 0 || result.Counts.Dropped != 1 || result.Counts.Collided != MaxRows {
		t.Fatalf("rows=%d Counts=%+v, want z dropped and %d collided", len(result.Rows), result.Counts, MaxRows)
	}
}
