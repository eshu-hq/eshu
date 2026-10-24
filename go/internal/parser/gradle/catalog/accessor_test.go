// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"strings"
	"testing"
)

// Gradle's LibrariesSourceGenerator.assertUnique fails the build when two
// aliases in one table map to the same accessor name: toJavaName splits an
// alias on '.', '-' and '_' and capitalizes each part, so fooBar and foo-bar
// are both getFooBar(). Neither row may claim a resolved version.
func TestScanAccessorNameClashIsACollision(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"camel and hyphen": "[libraries]\nfooBar = \"g:a:1.0\"\nfoo-bar = \"g:b:1.0\"\n",
		"quoted dot":       "[libraries]\n\"foo.bar\" = \"g:b:1.0\"\nfooBar = \"g:a:1.0\"\n",
		"double separator": "[libraries]\nfoo--bar = \"g:b:1.0\"\nfooBar = \"g:a:1.0\"\n",
		"nested camel":     "[libraries]\nfooBar = \"g:a:1.0\"\nfoo-bar.module = \"g:b\"\n",
	} {
		result := scanParsed(t, text)
		for _, row := range result.Rows {
			requireUnresolved(t, row, collisionKey)
		}
		if result.Counts.Collided != 2 {
			t.Errorf("%s: Collided = %d, want 2", name, result.Counts.Collided)
		}
	}
}

func TestScanAccessorNameClashInPluginsAndVersions(t *testing.T) {
	t.Parallel()
	result := scanParsed(t, "[versions]\nvX = \"1.0\"\nv-x = \"2.0\"\n"+
		"[libraries]\na = { module = \"g:a\", version.ref = \"vX\" }\nb = { module = \"g:b\", version.ref = \"v-x\" }\n"+
		"[plugins]\npX = \"org.p:1.0\"\np_x = \"org.q:1.0\"\n")
	requireUnresolved(t, rowByAlias(t, result.Rows, "a"), "vX")
	requireUnresolved(t, rowByAlias(t, result.Rows, "b"), "v-x")
	requireUnresolved(t, rowByAlias(t, result.Rows, "pX"), collisionKey)
	requireUnresolved(t, rowByAlias(t, result.Rows, "p_x"), collisionKey)
	if result.Counts.Collided != 4 {
		t.Errorf("Collided = %d, want 4", result.Counts.Collided)
	}
}

// Only the first letter of each part is capitalized, so foobar (getFoobar)
// and fooBar (getFooBar) are different accessors and both resolve.
func TestScanAccessorNamesThatDifferStayResolved(t *testing.T) {
	t.Parallel()
	result := scanParsed(t, "[libraries]\nfoobar = \"g:a:1.0\"\nfooBar = \"g:b:1.0\"\nfoo-barX = \"g:c:1.0\"\n")
	for _, alias := range []string{"foobar", "fooBar", "foo-barX"} {
		if row := rowByAlias(t, result.Rows, alias); row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s = %#v, want resolved", alias, row)
		}
	}
	if result.Counts.Collided != 0 {
		t.Errorf("Collided = %d, want 0", result.Counts.Collided)
	}
}

// A version.ref is looked up by its normalized alias, not by accessor name:
// Gradle finds no foo.bar version when only fooBar is defined.
func TestScanReferenceMatchesTheNormalizedAliasNotTheAccessorName(t *testing.T) {
	t.Parallel()
	result := scanParsed(t, "[versions]\nfooBar = \"1.0\"\n[libraries]\n"+
		"hit = { module = \"g:a\", version.ref = \"fooBar\" }\nmiss = { module = \"g:b\", version.ref = \"foo-bar\" }\n")
	if row := rowByAlias(t, result.Rows, "hit"); row["value"] != "1.0" {
		t.Errorf("hit = %#v, want resolved 1.0", row)
	}
	requireUnresolved(t, rowByAlias(t, result.Rows, "miss"), "foo-bar")
}

// A catalog key never needs more than a few dotted segments. A key with more
// than MaxKeySegments is refused before the semantic TOML decode, whose cost
// grows with the segment count.
func TestScanKeySegmentBoundIsExactlyMaxKeySegments(t *testing.T) {
	t.Parallel()
	at := "a" + strings.Repeat(".a", MaxKeySegments-1)
	over := at + ".a"
	for name, tc := range map[string]struct {
		text string
		want string
	}{
		"root at bound":     {at + " = 1\n", StateParsed},
		"root over bound":   {over + " = 1\n", StateTooDeep},
		"header at bound":   {"[" + at + "]\nb = 1\n", StateParsed},
		"header over bound": {"[" + over + "]\nb = 1\n", StateTooDeep},
		"array over bound":  {"[[" + over + "]]\nb = 1\n", StateTooDeep},
		"alias over bound":  {"[libraries]\n" + over + " = 1\n", StateTooDeep},
		"inline over bound": {"[libraries]\nx = { " + over + " = 1 }\n", StateTooDeep},
		"nested inline":     {"[metadata]\nx = [{ y = { " + over + " = 1 } }]\n", StateTooDeep},
	} {
		if result := Scan([]byte(tc.text)); result.State != tc.want || len(result.Rows) != 0 {
			t.Errorf("%s: State=%q (%s) rows=%d, want %q with none", name, result.State, result.ParseError, len(result.Rows), tc.want)
		}
	}
}
