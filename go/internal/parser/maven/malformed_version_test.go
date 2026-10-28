// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maven

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// malformedPom renders one dependency per version text in the dependencies
// section so a table of version texts maps to rows one to one.
func malformedPom(versions []string) string {
	var body strings.Builder
	for i, version := range versions {
		fmt.Fprintf(&body, `    <dependency>
      <groupId>org.m%d</groupId>
      <artifactId>a%d</artifactId>
      <version>%s</version>
    </dependency>
`, i, i, version)
	}
	return `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
` + body.String() + `  </dependencies>
</project>`
}

// TestParseMarksVersionsWithForbiddenCharactersUnresolved pins issue #9088:
// a <version> holding a character Maven's model validator forbids
// (`\/:"<>|?*`) is not a version, so its row is unresolved and keeps the
// literal text. A forbidden character names no property key, so none is
// recorded. Legitimate versions keep resolving, including semver build
// metadata with '+' which Maven allows.
func TestParseMarksVersionsWithForbiddenCharactersUnresolved(t *testing.T) {
	t.Parallel()

	malformed := []string{"1.0:jdk8", `1.0\1`, "1.0/1", `1.0"1`, "1.0&lt;1", "1.0&gt;1", "1.0|1", "1.0?1", "1.0*1"}
	legit := []string{"5.3.20", "1.0.0-SNAPSHOT", "3.2.0", "33.0.0-jre", "1.0.Final", "1.0.0+build.1"}
	decoded := []string{"1.0:jdk8", `1.0\1`, "1.0/1", `1.0"1`, "1.0<1", "1.0>1", "1.0|1", "1.0?1", "1.0*1"}

	path := writeFixture(t, "pom.xml", malformedPom(append(append([]string{}, malformed...), legit...)))
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for i, want := range decoded {
		row := requireDependencyRow(t, payload, fmt.Sprintf("org.m%d:a%d", i, i))
		if got := row["dependency_resolution_state"]; got != "unresolved" {
			t.Fatalf("version %q: state = %#v, want unresolved", want, got)
		}
		if got := row["value"]; got != want {
			t.Fatalf("version %q: value = %#v, want the literal text", want, got)
		}
		if _, ok := row["dependency_unresolved_keys"]; ok {
			t.Fatalf("version %q: row = %#v, a forbidden character names no key", want, row)
		}
	}
	for i, want := range legit {
		row := requireDependencyRow(t, payload, fmt.Sprintf("org.m%d:a%d", len(decoded)+i, len(decoded)+i))
		if got := row["dependency_resolution_state"]; got != "resolved" {
			t.Fatalf("version %q: state = %#v, want resolved", want, got)
		}
		if got := row["value"]; got != want {
			t.Fatalf("version %q: value = %#v, want %q", want, got, want)
		}
	}
}

// TestParseMarksPropertyResolvedToForbiddenCharactersUnresolved pins the
// property variant: a local property whose value holds a forbidden character
// resolves, but the resolved text is still not a version.
func TestParseMarksPropertyResolvedToForbiddenCharactersUnresolved(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <properties>
    <bad.version>1.0:jdk8</bad.version>
    <good.version>5.3.20</good.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>org.example</groupId>
      <artifactId>bad</artifactId>
      <version>${bad.version}</version>
    </dependency>
    <dependency>
      <groupId>org.example</groupId>
      <artifactId>good</artifactId>
      <version>${good.version}</version>
    </dependency>
  </dependencies>
</project>`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	bad := requireDependencyRow(t, payload, "org.example:bad")
	if got := bad["dependency_resolution_state"]; got != "unresolved" {
		t.Fatalf("bad state = %#v, want unresolved", got)
	}
	if got := bad["value"]; got != "${bad.version}" {
		t.Fatalf("bad value = %#v, want the literal text", got)
	}
	good := requireDependencyRow(t, payload, "org.example:good")
	if got := good["dependency_resolution_state"]; got != "resolved" {
		t.Fatalf("good state = %#v, want resolved", got)
	}
	if got := good["value"]; got != "5.3.20" {
		t.Fatalf("good value = %#v, want 5.3.20", got)
	}
}

// illegalMavenVersionChar draws one character Maven forbids in a version.
func illegalMavenVersionChar(rng *rand.Rand) string {
	chars := []string{`\`, "/", ":", `"`, "<", ">", "|", "?", "*"}
	return chars[rng.Intn(len(chars))]
}

// exactMavenVersion draws a legitimate Maven version, including semver build
// metadata with '+' which Maven allows.
func exactMavenVersion(rng *rand.Rand) string {
	base := fmt.Sprintf("%d.%d.%d", rng.Intn(30), rng.Intn(30), rng.Intn(30))
	switch rng.Intn(5) {
	case 0:
		return base + "-SNAPSHOT"
	case 1:
		return base + "+build.1"
	case 2:
		return base + ".Final"
	default:
		return base
	}
}

// TestGeneratedVersionsMatchTheMavenOracle drives seeded POMs (one per round,
// several versions each) through Parse across every section the reader
// exposes and checks every row against the generator's family label, which
// comes from Maven's forbidden-character set, not from the implementation: a
// legitimate version resolves, a text with a forbidden character stays
// unresolved keeping the literal. The proof is that no legitimate version
// flips.
func TestGeneratedVersionsMatchTheMavenOracle(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(9088))
	for round := 0; round < 200; round++ {
		type want struct {
			exact bool
			value string
		}
		wants := map[string]want{}
		var deps, managed, profiles strings.Builder
		for i, n := 0, 1+rng.Intn(4); i < n; i++ {
			version := exactMavenVersion(rng)
			exact := true
			if rng.Intn(2) == 0 {
				version = fmt.Sprintf("%s%s%d", version, illegalMavenVersionChar(rng), rng.Intn(10))
				exact = false
			}
			escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(version)
			section := rng.Intn(3)
			name := fmt.Sprintf("org.r%d:a%d", round*10+i, round*10+i)
			wants[name] = want{exact: exact, value: version}
			dep := fmt.Sprintf(`<dependency><groupId>org.r%d</groupId><artifactId>a%d</artifactId><version>%s</version></dependency>`,
				round*10+i, round*10+i, escaped)
			switch section {
			case 0:
				deps.WriteString("    " + dep + "\n")
			case 1:
				managed.WriteString("    " + dep + "\n")
			default:
				profiles.WriteString("    " + dep + "\n")
			}
		}
		source := `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
` + deps.String() + `  </dependencies>
  <dependencyManagement>
    <dependencies>
` + managed.String() + `    </dependencies>
  </dependencyManagement>
  <profiles>
    <profile>
      <id>p</id>
      <dependencies>
` + profiles.String() + `      </dependencies>
    </profile>
  </profiles>
</project>`
		path := writeFixture(t, "pom.xml", source)
		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("round %d: Parse() error = %v", round, err)
		}
		rows, _ := payload["variables"].([]map[string]any)
		if len(rows) != len(wants) {
			t.Fatalf("round %d: %d rows, want %d", round, len(rows), len(wants))
		}
		for _, row := range rows {
			name, _ := row["name"].(string)
			exp, ok := wants[name]
			if !ok {
				t.Fatalf("round %d: unexpected row %q", round, name)
			}
			state := row["dependency_resolution_state"]
			if exp.exact && (state != "resolved" || row["value"] != exp.value) {
				t.Fatalf("round %d: %s = %#v, want resolved %q", round, name, row, exp.value)
			}
			if !exp.exact && (state != "unresolved" || row["value"] != exp.value) {
				t.Fatalf("round %d: %s = %#v, want unresolved keeping %q", round, name, row, exp.value)
			}
		}
	}
}
