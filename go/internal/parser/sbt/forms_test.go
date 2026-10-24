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

func TestParseStripsLeadingByteOrderMark(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, "\xEF\xBB\xBFlibraryDependencies += \"a\" % \"b\" % \"1\"\n")
	requireField(t, requireRow(t, payload, "a:b"), "value", "1")

	versioned := parseSource(t, "\xEF\xBB\xBFscalaVersion := \"2.13.12\"\n")
	requireStateField(t, stateOf(t, versioned), "scala_version", "2.13.12")
}

func TestParseSecondDeclarationAfterSemicolon(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies += "a" % "b" % "1"; libraryDependencies += "c" % "d" % "2"; scalaVersion := "3.3.1"
libraryDependencies += unreadable; libraryDependencies += "e" % "f" % "3"
libraryDependencies += "g" %; libraryDependencies += "h" % "i" % "4"
`)
	for _, name := range []string{"a:b", "c:d", "e:f", "h:i"} {
		requireRow(t, payload, name)
	}
	state := stateOf(t, payload)
	requireStateField(t, state, "scala_version", "3.3.1")
	requireStateField(t, state, "unresolved_declaration_count", 2)
}

func TestParseConcatenatedCollections(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1") ++ Seq("c" % "d" % "2")
libraryDependencies ++= Seq("e" % "f" % "3").map(_ % Test) ++ Seq("g" % "h" % "4")
`)
	for name, scope := range map[string]string{"a:b": "compile", "c:d": "compile", "e:f": "test", "g:h": "compile"} {
		requireField(t, requireRow(t, payload, name), "dependency_scope", scope)
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)

	// A non-literal operand is counted, never silently dropped.
	opaque := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1") ++ extraDeps
`)
	requireRow(t, opaque, "a:b")
	requireStateField(t, stateOf(t, opaque), "unresolved_declaration_count", 1)
}

func TestParseBinaryArtifactSkipsPreReleaseScalaVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"2.13.0-M5", "3.0.0-RC1", "2.13.12-bin-typelevel-4"} {
		payload := parseSource(t, "scalaVersion := \""+version+"\"\nlibraryDependencies += \"o\" %% \"n\" % \"1\"\n")
		row := requireRow(t, payload, "o:n")
		if hint, ok := row["sbt_binary_artifact"]; ok {
			t.Errorf("scalaVersion %s derived sbt_binary_artifact %v; sbt's suffix is not the x.y binary version", version, hint)
		}
	}
	stable := parseSource(t, "scalaVersion := \"2.13.12\"\nlibraryDependencies += \"o\" %% \"n\" % \"1\"\n")
	requireField(t, requireRow(t, stable, "o:n"), "sbt_binary_artifact", "o:n_2.13")
}

func TestParseOversizedIdentifierChainIsCountedNotStored(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("v.", 400) + "v"
	payload := parseSource(t, "libraryDependencies += \"a\" % \"b\" % "+long+"\nlibraryDependencies += \"c\" % \"d\" % \"2\"\n")
	rows := dependencyRows(t, payload)
	if len(rows) != 1 || rows[0]["name"] != "c:d" {
		t.Fatalf("rows = %#v, want only c:d", rows)
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
}

// TestParseIdentifierChainAllocationCeiling asserts memory, never time: a
// million-segment chain must not be buffered into the row value or keys.
func TestParseIdentifierChainAllocationCeiling(t *testing.T) {
	// Not parallel: TotalAlloc is process wide.
	source := "libraryDependencies += \"a\" % \"b\" % " + strings.Repeat("v.", 500_000) + "v\n"
	path := filepath.Join(t.TempDir(), "build.sbt")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	payload, err := Parse(path, false, shared.Options{})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if rows := dependencyRows(t, payload); len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
	// The read buffer plus the string copy are 2x the input; the chain must add
	// no buffer proportional to the segment count on top of that.
	if got, ceiling := after.TotalAlloc-before.TotalAlloc, uint64(3*len(source)); got > ceiling {
		t.Fatalf("Parse allocated %d bytes for a %d byte input, ceiling %d", got, len(source), ceiling)
	}
	if mallocs := after.Mallocs - before.Mallocs; mallocs > 1000 {
		t.Fatalf("Parse made %d allocations for one declaration, ceiling 1000", mallocs)
	}
}
