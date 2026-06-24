// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseGroovyStringFormDeclarations(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle", `plugins { id 'java' }

dependencies {
    implementation 'org.springframework:spring-core:5.3.20'
    api "com.google.guava:guava:31.1-jre"
    runtimeOnly 'org.postgresql:postgresql:42.5.0'
    compileOnly 'org.projectlombok:lombok:1.18.30'
    testImplementation 'junit:junit:4.13.2'
    testRuntimeOnly 'org.junit.jupiter:junit-jupiter-engine:5.10.0'
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expected := map[string]string{
		"org.springframework:spring-core":        "implementation",
		"com.google.guava:guava":                 "api",
		"org.postgresql:postgresql":              "runtimeOnly",
		"org.projectlombok:lombok":               "compileOnly",
		"junit:junit":                            "testImplementation",
		"org.junit.jupiter:junit-jupiter-engine": "testRuntimeOnly",
	}
	for name, wantSection := range expected {
		row := requireDependencyRow(t, payload, name)
		if got, want := row["section"], wantSection; got != want {
			t.Fatalf("%s section = %#v, want %q", name, got, want)
		}
		if got, want := row["package_manager"], "gradle"; got != want {
			t.Fatalf("%s package_manager = %#v, want %q", name, got, want)
		}
		if got, want := row["direct_dependency"], true; got != want {
			t.Fatalf("%s direct_dependency = %#v, want %v", name, got, want)
		}
	}
	if got, want := requireDependencyRow(t, payload, "junit:junit")["value"], "4.13.2"; got != want {
		t.Fatalf("junit value = %#v, want %q", got, want)
	}
}

func TestParseKotlinDSLStringFormDeclarations(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle.kts", `plugins { java }

dependencies {
    implementation("org.springframework:spring-core:5.3.20")
    api("com.google.guava:guava:31.1-jre")
    runtimeOnly("org.postgresql:postgresql:42.5.0")
    compileOnly("org.projectlombok:lombok:1.18.30")
    testImplementation("junit:junit:4.13.2")
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, want := range []string{
		"org.springframework:spring-core",
		"com.google.guava:guava",
		"org.postgresql:postgresql",
		"org.projectlombok:lombok",
		"junit:junit",
	} {
		requireDependencyRow(t, payload, want)
	}

	implementation := requireDependencyRow(t, payload, "org.springframework:spring-core")
	if got, want := implementation["section"], "implementation"; got != want {
		t.Fatalf("section = %#v, want %q", got, want)
	}
	test := requireDependencyRow(t, payload, "junit:junit")
	if got, want := test["section"], "testImplementation"; got != want {
		t.Fatalf("test section = %#v, want %q", got, want)
	}
}

func TestParseMarksPlatformBomWithDistinctSection(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle", `dependencies {
    implementation platform('org.springframework.boot:spring-boot-dependencies:3.2.0')
    implementation enforcedPlatform('com.example:enforced-bom:1.0.0')
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	bom := requireDependencyRow(t, payload, "org.springframework.boot:spring-boot-dependencies")
	if got, want := bom["section"], "implementation:platform"; got != want {
		t.Fatalf("platform section = %#v, want %q", got, want)
	}
	if got, want := bom["dependency_scope"], "platform"; got != want {
		t.Fatalf("platform dependency_scope = %#v, want %q", got, want)
	}

	enforced := requireDependencyRow(t, payload, "com.example:enforced-bom")
	if got, want := enforced["section"], "implementation:enforcedPlatform"; got != want {
		t.Fatalf("enforcedPlatform section = %#v, want %q", got, want)
	}
	// enforcedPlatform must collapse to the documented `platform` scope so
	// downstream impact-priority logic does not see an unknown scope value.
	// The section already preserves the enforced-vs-plain distinction.
	if got, want := enforced["dependency_scope"], "platform"; got != want {
		t.Fatalf("enforcedPlatform dependency_scope = %#v, want %q (must normalize to documented scope)", got, want)
	}
}

func TestParseGroovyMapFormDeclarations(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle", `dependencies {
    implementation group: 'com.google.guava', name: 'guava', version: '31.1-jre'
    testImplementation(group: 'junit', name: 'junit', version: '4.13.2')
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	guava := requireDependencyRow(t, payload, "com.google.guava:guava")
	if got, want := guava["value"], "31.1-jre"; got != want {
		t.Fatalf("guava value = %#v, want %q", got, want)
	}
	if got, want := guava["section"], "implementation"; got != want {
		t.Fatalf("guava section = %#v, want %q", got, want)
	}

	junit := requireDependencyRow(t, payload, "junit:junit")
	if got, want := junit["section"], "testImplementation"; got != want {
		t.Fatalf("junit section = %#v, want %q", got, want)
	}
}

func TestParsePreservesUnresolvedVersionVariables(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle", `def springVersion = '5.3.20'
ext { kotlinVersion = '1.9.20' }

dependencies {
    implementation "org.springframework:spring-core:${springVersion}"
    implementation "org.jetbrains.kotlin:kotlin-stdlib:$kotlinVersion"
    implementation "org.example:external:${rootProject.ext.externalVersion}"
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	spring := requireDependencyRow(t, payload, "org.springframework:spring-core")
	if got, want := spring["value"], "5.3.20"; got != want {
		t.Fatalf("spring value = %#v, want %q (def-bound version)", got, want)
	}
	if got, want := spring["dependency_resolution_state"], "resolved"; got != want {
		t.Fatalf("spring dependency_resolution_state = %#v, want %q", got, want)
	}

	kotlin := requireDependencyRow(t, payload, "org.jetbrains.kotlin:kotlin-stdlib")
	if got, want := kotlin["value"], "1.9.20"; got != want {
		t.Fatalf("kotlin value = %#v, want %q (ext block version)", got, want)
	}

	external := requireDependencyRow(t, payload, "org.example:external")
	if got, want := external["dependency_resolution_state"], "unresolved"; got != want {
		t.Fatalf("external dependency_resolution_state = %#v, want %q", got, want)
	}
	if got, _ := external["value"].(string); got == "" || got == "5.3.20" {
		t.Fatalf("external value = %q, want raw unresolved reference", got)
	}
}

func TestParseSkipsProjectDependenciesAndFileCollections(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle", `dependencies {
    implementation project(':shared')
    implementation files('libs/local.jar')
    implementation fileTree(dir: 'libs', include: ['*.jar'])
    implementation 'org.example:real:1.0.0'
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	names := dependencyNames(rows)
	want := []string{"org.example:real"}
	if !equalStringSlices(names, want) {
		t.Fatalf("dependency rows = %#v, want %#v (project/files/fileTree skipped)", names, want)
	}
}

func TestParseSkipsGradleSourceSetAndVersionCatalogAliases(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle.kts", `dependencies {
    implementation(libs.spring.core)
    implementation(sourceSets.main.get().output)
    implementation("org.example:real:1.0.0")
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	names := dependencyNames(rows)
	want := []string{"org.example:real"}
	if !equalStringSlices(names, want) {
		t.Fatalf("dependency rows = %#v, want %#v (version catalog and source-set aliases skipped)", names, want)
	}
}

func TestParseHandlesBuildscriptDependenciesWithDistinctSection(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle", `buildscript {
    dependencies {
        classpath 'com.android.tools.build:gradle:8.1.0'
    }
}

dependencies {
    implementation 'org.example:real:1.0.0'
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	classpathRows := 0
	for _, row := range rows {
		if row["name"] == "com.android.tools.build:gradle" {
			classpathRows++
			if got, want := row["section"], "buildscript:classpath"; got != want {
				t.Fatalf("buildscript section = %#v, want %q", got, want)
			}
		}
	}
	if classpathRows != 1 {
		t.Fatalf("classpath emitted %d rows, want 1 (nested block must not be processed twice)", classpathRows)
	}

	requireDependencyRow(t, payload, "org.example:real")
}

func TestParseToleratesMalformedDSLWithoutSmugglingPartialRows(t *testing.T) {
	t.Parallel()

	// Unbalanced braces / quotes: parser must skip the bad block and continue
	// rather than emit a row that names a non-coordinate string.
	path := writeFixture(t, "build.gradle", `dependencies {
    implementation 'not-a-valid-coordinate'
    implementation "missing-quote
    implementation 'org.example:good:1.2.3'
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	names := dependencyNames(rows)
	want := []string{"org.example:good"}
	if !equalStringSlices(names, want) {
		t.Fatalf("dependency rows = %#v, want %#v", names, want)
	}
}

func TestParseHandlesParenthesizedKotlinDSLForms(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "build.gradle.kts", `dependencies {
    implementation("org.springframework:spring-core") {
        version { strictly("5.3.20") }
    }
    implementation(platform("io.micrometer:micrometer-bom:1.11.0"))
    implementation(kotlin("stdlib", "1.9.20"))
}`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// The string form `"org.springframework:spring-core"` lacks a version so
	// the parser must mark it as partial rather than fabricate one.
	spring := requireDependencyRow(t, payload, "org.springframework:spring-core")
	if got, want := spring["dependency_resolution_state"], "partial"; got != want {
		t.Fatalf("spring dependency_resolution_state = %#v, want %q", got, want)
	}

	bom := requireDependencyRow(t, payload, "io.micrometer:micrometer-bom")
	if got, want := bom["section"], "implementation:platform"; got != want {
		t.Fatalf("micrometer section = %#v, want %q", got, want)
	}
}

func writeFixture(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func requireDependencyRow(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	for _, row := range rows {
		if rowName, _ := row["name"].(string); rowName == name {
			if row["config_kind"] != "dependency" {
				t.Fatalf("%s: config_kind = %#v, want dependency", name, row["config_kind"])
			}
			return row
		}
	}
	t.Fatalf("dependency row %q missing; got rows %#v", name, rows)
	return nil
}

func dependencyNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		if name, ok := row["name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
