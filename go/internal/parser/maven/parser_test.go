// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maven

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseEmitsDirectDependenciesWithGroupArtifactCoordinate(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>5.3.20</version>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "org.springframework:spring-core")
	if got, want := row["value"], "5.3.20"; got != want {
		t.Fatalf("value = %#v, want %q", got, want)
	}
	if got, want := row["section"], "dependencies"; got != want {
		t.Fatalf("section = %#v, want %q", got, want)
	}
	if got, want := row["dependency_scope"], "compile"; got != want {
		t.Fatalf("dependency_scope = %#v, want %q", got, want)
	}
	if got, want := row["dependency_resolution_state"], "resolved"; got != want {
		t.Fatalf("dependency_resolution_state = %#v, want %q", got, want)
	}
	if got, want := row["direct_dependency"], true; got != want {
		t.Fatalf("direct_dependency = %#v, want %v", got, want)
	}
	if got, want := row["package_manager"], "maven"; got != want {
		t.Fatalf("package_manager = %#v, want %q", got, want)
	}
}

func TestParseSplitsTestAndProvidedScopesIntoDistinctSections(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>jakarta.servlet</groupId>
      <artifactId>jakarta.servlet-api</artifactId>
      <version>6.0.0</version>
      <scope>provided</scope>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13.2</version>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	provided := requireDependencyRow(t, payload, "jakarta.servlet:jakarta.servlet-api")
	if got, want := provided["section"], "dependencies:provided"; got != want {
		t.Fatalf("provided section = %#v, want %q", got, want)
	}
	if got, want := provided["dependency_scope"], "provided"; got != want {
		t.Fatalf("provided dependency_scope = %#v, want %q", got, want)
	}

	testDep := requireDependencyRow(t, payload, "junit:junit")
	if got, want := testDep["section"], "dependencies:test"; got != want {
		t.Fatalf("test section = %#v, want %q", got, want)
	}
}

func TestParseResolvesLocalPropertyReferences(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <properties>
    <spring.version>5.3.20</spring.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>${spring.version}</version>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "org.springframework:spring-core")
	if got, want := row["value"], "5.3.20"; got != want {
		t.Fatalf("value = %#v, want %q (local property must be resolved)", got, want)
	}
	if got, want := row["dependency_resolution_state"], "resolved"; got != want {
		t.Fatalf("dependency_resolution_state = %#v, want %q", got, want)
	}
}

func TestParseMarksExternalPropertyReferencesAsUnresolved(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.example</groupId>
      <artifactId>external</artifactId>
      <version>${external.version}</version>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "org.example:external")
	if got, want := row["value"], "${external.version}"; got != want {
		t.Fatalf("value = %#v, want %q (raw unresolved reference preserved)", got, want)
	}
	if got, want := row["dependency_resolution_state"], "unresolved"; got != want {
		t.Fatalf("dependency_resolution_state = %#v, want %q", got, want)
	}
	unresolved, ok := row["dependency_unresolved_keys"].([]string)
	if !ok || len(unresolved) != 1 || unresolved[0] != "external.version" {
		t.Fatalf("dependency_unresolved_keys = %#v, want [external.version]", row["dependency_unresolved_keys"])
	}
}

func TestParseMarksDependencyWithoutVersionAsPartial(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.apache.commons</groupId>
      <artifactId>commons-lang3</artifactId>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "org.apache.commons:commons-lang3")
	if got, want := row["dependency_resolution_state"], "partial"; got != want {
		t.Fatalf("dependency_resolution_state = %#v, want %q for missing version", got, want)
	}
	if got, want := row["value"], ""; got != want {
		t.Fatalf("value = %#v, want empty string for missing version", got)
	}
}

func TestParseExtractsDependencyManagementWithImportScope(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.springframework.boot</groupId>
        <artifactId>spring-boot-dependencies</artifactId>
        <version>3.2.0</version>
        <type>pom</type>
        <scope>import</scope>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "org.springframework.boot:spring-boot-dependencies")
	if got, want := row["section"], "dependencyManagement:import"; got != want {
		t.Fatalf("section = %#v, want %q", got, want)
	}
	if got, want := row["dependency_scope"], "import"; got != want {
		t.Fatalf("dependency_scope = %#v, want %q", got, want)
	}
}

func TestParseResolvesDependencyManagementLocalProperties(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <properties>
    <jackson.version>2.15.3</jackson.version>
  </properties>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.fasterxml.jackson.core</groupId>
        <artifactId>jackson-databind</artifactId>
        <version>${jackson.version}</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "com.fasterxml.jackson.core:jackson-databind")
	if got, want := row["section"], "dependencyManagement"; got != want {
		t.Fatalf("section = %#v, want %q", got, want)
	}
	if got, want := row["value"], "2.15.3"; got != want {
		t.Fatalf("value = %#v, want locally resolved dependencyManagement version %q", got, want)
	}
	if got, want := row["dependency_resolution_state"], "resolved"; got != want {
		t.Fatalf("dependency_resolution_state = %#v, want %q", got, want)
	}
}

func TestParseMarksOptionalDependencies(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.slf4j</groupId>
      <artifactId>slf4j-api</artifactId>
      <version>2.0.7</version>
      <optional>true</optional>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "org.slf4j:slf4j-api")
	if got, want := row["dependency_optional"], true; got != want {
		t.Fatalf("dependency_optional = %#v, want %v", got, want)
	}
}

func TestParseRejectsMalformedXMLWithoutSmugglingRows(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project><dependencies><dependency><groupId>x</groupId></dependencies>`)

	_, err := Parse(path, false, shared.Options{})
	if err == nil {
		t.Fatal("Parse() error = nil, want malformed XML error")
	}
}

func TestParseHandlesMultiModuleWithoutCrossFileResolution(t *testing.T) {
	t.Parallel()

	// Sub-module pom that references a property only defined in the parent.
	// Without parent resolution the reference must remain unresolved rather
	// than the parser inventing a guess.
	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>parent</artifactId>
    <version>1.0.0</version>
    <relativePath>../pom.xml</relativePath>
  </parent>
  <artifactId>module-a</artifactId>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>shared</artifactId>
      <version>${parent.shared.version}</version>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	row := requireDependencyRow(t, payload, "com.example:shared")
	if got, want := row["dependency_resolution_state"], "unresolved"; got != want {
		t.Fatalf("dependency_resolution_state = %#v, want %q (no parent POM resolution)", got, want)
	}
}

func TestParseSkipsDependencyMissingGroupOrArtifact(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.example</groupId>
      <version>1.0.0</version>
    </dependency>
    <dependency>
      <groupId>org.example</groupId>
      <artifactId>good</artifactId>
      <version>2.0.0</version>
    </dependency>
  </dependencies>
</project>`)

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
