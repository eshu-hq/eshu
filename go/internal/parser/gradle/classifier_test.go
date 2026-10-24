// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Gradle string notation is group:name:version:classifier@ext. The classifier
// and the extension describe the artifact, not the version, so neither may
// leak into the version or the coordinate name. A classifier alone leaves the
// version exact. An '@ext' suffix keeps the row unresolved with the version
// text plus suffix, the same as "g:a:1.0@jar" (issue #8988). Text past the
// classifier is not a notation Gradle accepts, so it is no dependency at all
// (see TestParseStringNotationPastTheClassifierIsNotADependency).
func TestParseStringNotationKeepsClassifierAndExtensionOutOfTheVersion(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, "build.gradle", `dependencies {
    implementation 'org.g:classified:1.0:jdk8'
    implementation 'org.g:classext:1.0:jdk8@jar'
    implementation 'org.g:ext:2.0@aar'
    implementation 'org.g:noversion@jar'
    implementation 'org.g:rangeclass:[1.0,2.0):tests'
}
`)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for _, tc := range []struct {
		name  string
		state string
		value string
	}{
		{name: "org.g:classified", state: "resolved", value: "1.0"},
		{name: "org.g:classext", state: "unresolved", value: "1.0@jar"},
		{name: "org.g:ext", state: "unresolved", value: "2.0@aar"},
		{name: "org.g:noversion", state: "partial", value: ""},
		{name: "org.g:rangeclass", state: "unresolved", value: "[1.0,2.0)"},
	} {
		row := requireDependencyRow(t, payload, tc.name)
		if row["dependency_resolution_state"] != tc.state || row["value"] != tc.value {
			t.Errorf("%s = state %v value %q, want state %s value %q",
				tc.name, row["dependency_resolution_state"], row["value"], tc.state, tc.value)
		}
	}
}

// Text past the classifier is not a notation Gradle accepts, so the declaration
// emits no row. A row would carry the colon-bearing text as its value, and a
// reader that takes the value of an unresolved row as the observed version
// would report it as a version.
func TestParseStringNotationPastTheClassifierIsNotADependency(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, "build.gradle", `dependencies {
    implementation 'org.g:toomany:1.0:jdk8:extra'
    implementation 'org.g:kept:1.0:jdk8'
}
`)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	for _, row := range rows {
		if name, _ := row["name"].(string); name == "org.g:toomany" {
			t.Fatalf("over-long notation emitted a row: %#v", row)
		}
	}
	requireDependencyRow(t, payload, "org.g:kept")
}

// Kotlin string notation follows the same grammar.
func TestParseKotlinStringNotationKeepsClassifierOutOfTheVersion(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, "build.gradle.kts", "dependencies {\n    testImplementation(\"org.g:k:3.1.4:tests\")\n}\n")
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	row := requireDependencyRow(t, payload, "org.g:k")
	if row["dependency_resolution_state"] != "resolved" || row["value"] != "3.1.4" {
		t.Fatalf("row = %#v, want resolved 3.1.4", row)
	}
}
