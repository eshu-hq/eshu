// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// The generator builds version text from the Gradle user manual ("Declaring
// Versions and Ranges", "Declaring Rich Versions", artifact-only notation),
// not from the implementation. Its family label is the oracle: an exact
// family must resolve to the text as written, every selector family must stay
// unresolved with the literal kept.
var (
	fuzzQualifiers = []string{"", "", "-SNAPSHOT", "-rc1", "-RC2", ".Final", "-jre", "-M1", "_beta", "-alpha.3", "-android"}
	fuzzStatuses   = []string{"release", "integration", "milestone", "Release", "INTEGRATION", "nightly"}
	fuzzExtensions = []string{"jar", "aar", "pom", "zip", "war"}
)

func fuzzExact(rng *rand.Rand) string {
	parts := make([]string, 1+rng.Intn(4))
	for i := range parts {
		parts[i] = strconv.Itoa(rng.Intn(40))
	}
	return strings.Join(parts, ".") + fuzzQualifiers[rng.Intn(len(fuzzQualifiers))]
}

func fuzzPrefix(rng *rand.Rand) string {
	switch rng.Intn(4) {
	case 0:
		return "+"
	case 1:
		return strconv.Itoa(rng.Intn(9)) + "+"
	default:
		parts := make([]string, 1+rng.Intn(3))
		for i := range parts {
			parts[i] = strconv.Itoa(rng.Intn(20))
		}
		return strings.Join(parts, ".") + ".+"
	}
}

func fuzzLatest(rng *rand.Rand) string {
	head := []string{"latest.", "Latest.", "LATEST."}[rng.Intn(3)]
	return head + fuzzStatuses[rng.Intn(len(fuzzStatuses))]
}

func fuzzRange(rng *rand.Rand) string {
	lower, upper := fuzzExact(rng), fuzzExact(rng)
	switch rng.Intn(3) {
	case 0:
		lower = ""
	case 1:
		upper = ""
	}
	if lower == "" && upper == "" {
		upper = "2.0"
	}
	open := []string{"[", "(", "]"}[rng.Intn(3)]
	closer := []string{"]", ")", "["}[rng.Intn(3)]
	if rng.Intn(6) == 0 {
		return "[" + lower + upper + "]"
	}
	return open + lower + "," + upper + closer
}

func fuzzStrict(rng *rand.Rand) string {
	if rng.Intn(2) == 0 {
		return fuzzExact(rng) + "!!"
	}
	return fuzzRange(rng) + "!!" + fuzzExact(rng)
}

func fuzzArtifactOnly(rng *rand.Rand) string {
	return fuzzExact(rng) + "@" + fuzzExtensions[rng.Intn(len(fuzzExtensions))]
}

// fuzzVersion returns generated version text and whether the Gradle docs say
// it names one exact version.
func fuzzVersion(rng *rand.Rand) (string, bool) {
	switch rng.Intn(6) {
	case 0:
		return fuzzPrefix(rng), false
	case 1:
		return fuzzLatest(rng), false
	case 2:
		return fuzzRange(rng), false
	case 3:
		return fuzzStrict(rng), false
	case 4:
		return fuzzArtifactOnly(rng), false
	default:
		return fuzzExact(rng), true
	}
}

type fuzzWant struct {
	resolved bool
	value    string
}

// fuzzStatement renders one dependency in a notation the reader exposes. It
// returns the statement, any property declaration it needs, the coordinate,
// and the literal the row must keep when unresolved.
func fuzzStatement(rng *rand.Rand, kotlin bool, i int, version string) (string, string, string, string) {
	group, artifact := fmt.Sprintf("org.f%d", i), fmt.Sprintf("a%d", i)
	coord := group + ":" + artifact
	prop := fmt.Sprintf("v%d", i)
	q := "'"
	if kotlin {
		q = "\""
	}
	switch rng.Intn(5) {
	case 0:
		if kotlin {
			return fmt.Sprintf("implementation(\"%s:%s\")", coord, version), "", coord, version
		}
		return fmt.Sprintf("implementation '%s:%s'", coord, version), "", coord, version
	case 1:
		if kotlin {
			return fmt.Sprintf("api(group = \"%s\", name = \"%s\", version = \"%s\")", group, artifact, version), "", coord, version
		}
		return fmt.Sprintf("api group: '%s', name: '%s', version: '%s'", group, artifact, version), "", coord, version
	case 2:
		return fmt.Sprintf("implementation(platform(%s%s:%s%s))", q, coord, version, q), "", coord, version
	case 3:
		decl := fmt.Sprintf("def %s = '%s'", prop, version)
		if kotlin {
			decl = fmt.Sprintf("val %s = \"%s\"", prop, version)
		}
		literal := "${" + prop + "}"
		return fmt.Sprintf("testImplementation(\"%s:%s\")", coord, literal), decl, coord, literal
	default:
		if kotlin {
			name := fmt.Sprintf("x%d", i)
			return fmt.Sprintf("implementation(kotlin(\"%s\", \"%s\"))", name, version), "",
				"org.jetbrains.kotlin:kotlin-" + name, version
		}
		return fmt.Sprintf("runtimeOnly \"%s:%s\"", coord, version), "", coord, version
	}
}

func fuzzRounds(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("ESHU_GRADLE_SELECTOR_FUZZ_ROUNDS")
	if raw == "" {
		return 3000
	}
	rounds, err := strconv.Atoi(raw)
	if err != nil || rounds <= 0 {
		t.Fatalf("ESHU_GRADLE_SELECTOR_FUZZ_ROUNDS = %q, want a positive integer", raw)
	}
	return rounds
}

// TestParseGeneratedVersionsMatchTheGradleOracle drives seeded build scripts
// (3,000 by default, ESHU_GRADLE_SELECTOR_FUZZ_ROUNDS to raise it) through
// Parse and checks every row against the generator's family label.
func TestParseGeneratedVersionsMatchTheGradleOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(8988))
	dir := t.TempDir()
	rounds := fuzzRounds(t)
	for round := 0; round < rounds; round++ {
		kotlin := rng.Intn(2) == 0
		name := "build.gradle"
		if kotlin {
			name = "build.gradle.kts"
		}
		var decls, body strings.Builder
		want := map[string]fuzzWant{}
		for i, n := 0, 1+rng.Intn(8); i < n; i++ {
			version, exact := fuzzVersion(rng)
			statement, decl, coord, literal := fuzzStatement(rng, kotlin, i, version)
			if decl != "" {
				decls.WriteString(decl + "\n")
			}
			body.WriteString("    " + statement + "\n")
			if exact {
				want[coord] = fuzzWant{resolved: true, value: version}
			} else {
				want[coord] = fuzzWant{value: literal}
			}
		}
		source := decls.String() + "dependencies {\n" + body.String() + "}\n"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("round %d: Parse error = %v\n%s", round, err, source)
		}
		rows, _ := payload["variables"].([]map[string]any)
		if len(rows) != len(want) {
			t.Fatalf("round %d: %d rows, want %d\n%s", round, len(rows), len(want), source)
		}
		for _, row := range rows {
			coord, _ := row["name"].(string)
			exp, ok := want[coord]
			if !ok {
				t.Fatalf("round %d: unexpected row %q\n%s", round, coord, source)
			}
			state := row["dependency_resolution_state"]
			if exp.resolved && (state != "resolved" || row["value"] != exp.value) {
				t.Fatalf("round %d: %s = %#v, want resolved %q\n%s", round, coord, row, exp.value, source)
			}
			if !exp.resolved && (state != "unresolved" || row["value"] != exp.value) {
				t.Fatalf("round %d: %s = %#v, want unresolved keeping %q\n%s", round, coord, row, exp.value, source)
			}
		}
	}
}
