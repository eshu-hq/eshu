// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// measureParse parses body once and returns the bytes and heap objects the
// parse allocated. It is deliberately not parallel: TotalAlloc and Mallocs are
// process-wide, so a concurrent test would pollute the delta. Go runs the
// parallel tests of a package only after every sequential test has finished.
func measureParse(t *testing.T, body string) (payload map[string]any, bytes, mallocs uint64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "build.sbt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	payload, err := Parse(path, false, shared.Options{})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return payload, after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs
}

// requireAllocCeiling fails when a parse allocated more than perInputByte
// bytes for each byte of source plus fixed slack. The ceilings are ratios of
// the input, never wall time, so they hold on a slow shared runner.
func requireAllocCeiling(t *testing.T, name string, inputBytes int, gotBytes uint64, perInputByte float64, slack uint64) {
	t.Helper()
	ceiling := uint64(float64(inputBytes)*perInputByte) + slack
	t.Logf("%s: input=%d bytes, TotalAlloc=%d (%.2fx input), ceiling=%d", name, inputBytes, gotBytes, float64(gotBytes)/float64(inputBytes), ceiling)
	if gotBytes > ceiling {
		t.Errorf("%s: TotalAlloc = %d bytes for %d input bytes (%.1fx), ceiling %d: work is not linear in the input", name, gotBytes, inputBytes, float64(gotBytes)/float64(inputBytes), ceiling)
	}
}

// seqOfDependencies renders `Seq("o" % "n0" % "1", ...)` with count elements.
func seqOfDependencies(count int) string {
	var source strings.Builder
	source.WriteString("Seq(")
	for index := 0; index < count; index++ {
		if index > 0 {
			source.WriteString(", ")
		}
		source.WriteString(`"o" % "n` + itoa(index) + `" % "1"`)
	}
	source.WriteString(")")
	return source.String()
}

// TestParseLongMapConfigIsNotCopiedPerDependency drives a 100 KB configuration
// string through `.map(_ % "...")` over 1,000 dependencies. The string is over
// the configuration bound, so every row is partial and none carries a copy.
func TestParseLongMapConfigIsNotCopiedPerDependency(t *testing.T) {
	const deps = 1_000
	config := strings.Repeat("test;", 20_000)
	body := "libraryDependencies ++= " + seqOfDependencies(deps) + `.map(_ % "` + config + `")` + "\n"

	payload, gotBytes, _ := measureParse(t, body)
	requireAllocCeiling(t, "long map config", len(body), gotBytes, 40, 1<<20)

	rows := dependencyRows(t, payload)
	if len(rows) != deps {
		t.Fatalf("rows = %d, want %d", len(rows), deps)
	}
	for _, row := range rows {
		if row["dependency_resolution_state"] != "partial" {
			t.Fatalf("%v state = %v, want partial for an overlong configuration", row["name"], row["dependency_resolution_state"])
		}
		if section, _ := row["section"].(string); len(section) > 128 {
			t.Fatalf("%v section is %d bytes: the raw configuration was copied into the row", row["name"], len(section))
		}
		if cfg, _ := row["sbt_configuration"].(string); len(cfg) > MaxConfigBytes {
			t.Fatalf("%v sbt_configuration is %d bytes, want at most %d", row["name"], len(cfg), MaxConfigBytes)
		}
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", deps)
}

// TestParseHugeConfigurationListIsNeverSplit feeds one 10 MB `"a,a,a,..."`
// configuration. Splitting it would allocate a part slice per comma.
func TestParseHugeConfigurationListIsNeverSplit(t *testing.T) {
	config := strings.Repeat("a,", 5_000_000)
	body := `libraryDependencies += "a" % "b" % "1" % "` + config + `"` + "\n"

	payload, gotBytes, _ := measureParse(t, body)
	requireAllocCeiling(t, "10 MB config list", len(body), gotBytes, 3, 1<<20)

	row := requireRow(t, payload, "a:b")
	requireField(t, row, "dependency_resolution_state", "partial")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
}

// TestParseChainedMapsStayLinear applies 20,000 chained `.map(_ % Test)` calls
// to 5,000 dependencies. A `.map` is outside the grammar, so the fallback reads
// the chain once and every row is partial; a per-dependency, per-call pass
// would add millions of objects.
func TestParseChainedMapsStayLinear(t *testing.T) {
	const deps = 5_000
	body := "libraryDependencies ++= " + seqOfDependencies(deps) + strings.Repeat(".map(_ % Test)", 20_000) + "\n"

	payload, _, mallocs := measureParse(t, body)
	t.Logf("chained maps: %d dependencies, %d mallocs", deps, mallocs)
	if ceiling := uint64(deps * 40); mallocs > ceiling {
		t.Errorf("Mallocs = %d for %d dependencies, ceiling %d: the chain is not read once", mallocs, deps, ceiling)
	}
	for _, name := range []string{"o:n0", "o:n4999"} {
		requireField(t, requireRow(t, payload, name), "dependency_resolution_state", "partial")
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", deps)
}

// TestParseRepeatedAndChainedConfigurationsStayLinear covers 200,000 trailing
// `% "test"` operands on one coordinate (a load error in sbt, so the fallback
// reads them once and emits nothing for them) and a million-segment
// identifier used as the configuration.
func TestParseRepeatedAndChainedConfigurationsStayLinear(t *testing.T) {
	repeated := `libraryDependencies += "a" % "b" % "1"` + strings.Repeat(` % "test"`, 200_000) + "\n"
	payload, gotBytes, _ := measureParse(t, repeated)
	requireAllocCeiling(t, "200k trailing configs", len(repeated), gotBytes, 3, 1<<20)
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
	if rows := dependencyRows(t, payload); len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: a run of configuration operands is not a coordinate", len(rows))
	}

	chain := `libraryDependencies += "a" % "b" % "1" % ` + strings.Repeat("v.", 500_000) + "v\n"
	payload, gotBytes, _ = measureParse(t, chain)
	requireAllocCeiling(t, "1M-segment config chain", len(chain), gotBytes, 3, 1<<20)
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
}

// TestParseTailAllowlistStaysLinear drives the grammar and its fallback with
// long tails: 100,000 allowlisted infix calls (matched by the grammar),
// 100,000 unknown calls, a 100,000-call `.map` body, and 100,000 leading-infix
// `%` lines on one coordinate (all three read once by the fallback). Each is
// one pass over the tokens, so the allocation stays a small multiple of the
// input. The ceiling stays 8x: the measured ratio is about 2x (the read buffer
// plus the string copy), and a full-package run adds noise from parallel
// tests; a quadratic pass would be thousands of times the input.
func TestParseTailAllowlistStaysLinear(t *testing.T) {
	const calls = 100_000
	cases := map[string]struct {
		body  string
		state string
	}{
		"allowlisted calls": {`libraryDependencies += "a" % "b" % "1"` + strings.Repeat(` exclude("x", "y")`, calls) + "\n", "resolved"},
		"unknown calls":     {`libraryDependencies += "a" % "b" % "1"` + strings.Repeat(` configure(x)`, calls) + "\n", "partial"},
		"long map body":     {`libraryDependencies ++= Seq("a" % "b" % "1").map(_` + strings.Repeat(`.withSources()`, calls) + ")\n", "partial"},
		"leading infix":     {`libraryDependencies += "a" % "b" % "1"` + strings.Repeat("\n  % Test", calls) + "\n", "partial"},
	}
	for name, tc := range cases {
		payload, gotBytes, _ := measureParse(t, tc.body)
		requireAllocCeiling(t, name, len(tc.body), gotBytes, 8, 1<<20)
		requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", tc.state)
	}
}
