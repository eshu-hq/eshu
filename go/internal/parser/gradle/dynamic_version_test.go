// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// versionCase is one row of the shared Gradle version table that every Gradle
// reader must agree with (selector/testdata/versions.txt).
type versionCase struct {
	text  string
	exact bool
}

func loadSharedVersionTable(t *testing.T) []versionCase {
	t.Helper()
	file, err := os.Open("selector/testdata/versions.txt")
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

// requireVersionState asserts the row state Gradle's own reading owes the
// declared text: an exact version is resolved with that value, anything else
// is unresolved and keeps the literal text as written.
func requireVersionState(t *testing.T, row map[string]any, wantExact bool, wantResolvedValue, literal string) {
	t.Helper()
	state := row["dependency_resolution_state"]
	if wantExact {
		if state != "resolved" || row["value"] != wantResolvedValue {
			t.Fatalf("row = %#v, want resolved with value %q", row, wantResolvedValue)
		}
		return
	}
	if state != "unresolved" || row["value"] != literal {
		t.Fatalf("row = %#v, want unresolved keeping literal %q", row, literal)
	}
	if _, ok := row["dependency_unresolved_keys"]; ok {
		t.Fatalf("row = %#v, a selector names no interpolation key", row)
	}
}

// A Gradle selector (prefix '+', latest.<status>, a Maven-style range, the
// '!!' strict short-hand, an '@ext' artifact suffix) names no single version,
// so a build script row that declares one must never be resolved. Every
// notation the reader exposes is covered: Groovy string, Groovy map, Kotlin
// string, Kotlin named arguments, the kotlin() helper, a platform wrapper, and
// a same-file property interpolated into the coordinate.
func TestParseDynamicVersionSelectorsAreNeverResolved(t *testing.T) {
	t.Parallel()
	for _, tc := range loadSharedVersionTable(t) {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			groovy := writeFixture(t, "build.gradle", "def sel = '"+tc.text+"'\ndependencies {\n"+
				"    implementation 'org.g:str:"+tc.text+"'\n"+
				"    implementation group: 'org.g', name: 'map', version: '"+tc.text+"'\n"+
				"    implementation platform('org.g:bom:"+tc.text+"')\n"+
				"    implementation \"org.g:interp:$sel\"\n"+
				"}\n")
			payload, err := Parse(groovy, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse(groovy) error = %v", err)
			}
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:str"), tc.exact, tc.text, tc.text)
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:map"), tc.exact, tc.text, tc.text)
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:bom"), tc.exact, tc.text, tc.text)
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:interp"), tc.exact, tc.text, "$sel")

			kotlin := writeFixture(t, "build.gradle.kts", "val sel = \""+tc.text+"\"\ndependencies {\n"+
				"    implementation(\"org.g:str:"+tc.text+"\")\n"+
				"    implementation(group = \"org.g\", name = \"named\", version = \""+tc.text+"\")\n"+
				"    implementation(kotlin(\"stdlib\", \""+tc.text+"\"))\n"+
				"    implementation(\"org.g:interp:${sel}\")\n"+
				"}\n")
			payload, err = Parse(kotlin, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse(kotlin) error = %v", err)
			}
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:str"), tc.exact, tc.text, tc.text)
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:named"), tc.exact, tc.text, tc.text)
			requireVersionState(t, requireDependencyRow(t, payload, "org.jetbrains.kotlin:kotlin-stdlib"), tc.exact, tc.text, tc.text)
			requireVersionState(t, requireDependencyRow(t, payload, "org.g:interp"), tc.exact, tc.text, "${sel}")
		})
	}
}

// An interpolation the file cannot satisfy keeps today's behavior: unresolved
// with its key, whatever text surrounds it.
func TestParseUnresolvedInterpolationNextToSelectorKeepsItsKey(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, "build.gradle", "dependencies {\n    implementation \"org.g:a:${major}.+\"\n}\n")
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	row := requireDependencyRow(t, payload, "org.g:a")
	keys, _ := row["dependency_unresolved_keys"].([]string)
	if row["dependency_resolution_state"] != "unresolved" || row["value"] != "${major}.+" || len(keys) != 1 || keys[0] != "major" {
		t.Fatalf("row = %#v, want unresolved ${major}.+ with key major", row)
	}
}
