// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

func TestParseSingleLibraryDependencyForms(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `name := "demo"

libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
libraryDependencies += "com.google.guava" % "guava" % "33.0.0-jre"
libraryDependencies += "org.scala-js" %%% "scalajs-dom" % "2.8.0"
`)

	if got := len(dependencyRows(t, payload)); got != 3 {
		t.Fatalf("row count = %d, want 3", got)
	}
	cats := requireRow(t, payload, "org.typelevel:cats-core")
	requireField(t, cats, "value", "2.10.0")
	requireField(t, cats, "section", "libraryDependencies")
	requireField(t, cats, "config_kind", "dependency")
	requireField(t, cats, "package_manager", "sbt")
	requireField(t, cats, "lang", "sbt")
	requireField(t, cats, "dependency_scope", "compile")
	requireField(t, cats, "dependency_resolution_state", "resolved")
	requireField(t, cats, "direct_dependency", true)
	requireField(t, cats, "dependency_path_kind", "manifest")
	requireField(t, cats, "dependency_optional", false)
	requireField(t, cats, "sbt_cross_version", "binary")
	requireField(t, cats, "line_number", 3)

	requireField(t, requireRow(t, payload, "com.google.guava:guava"), "sbt_cross_version", "none")
	requireField(t, requireRow(t, payload, "org.scala-js:scalajs-dom"), "sbt_cross_version", "scalajs")
}

func TestParseSeqAndAssignmentForms(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `lazy val root = (project in file("."))
  .settings(
    scalaVersion := "2.13.12",
    libraryDependencies ++= Seq(
      "org.typelevel" %% "cats-effect" % "3.5.2",
      "org.scalatest" %% "scalatest" % "3.2.17" % Test,
      "javax.servlet" % "servlet-api" % "2.5" % "provided",
      "ch.qos.logback" % "logback-classic" % "1.4.14" % Runtime
    ),
    libraryDependencies := Seq("io.circe" %% "circe-core" % "0.14.6")
  )
`)

	if got := len(dependencyRows(t, payload)); got != 5 {
		t.Fatalf("row count = %d, want 5: %#v", got, payload["variables"])
	}
	requireField(t, requireRow(t, payload, "org.typelevel:cats-effect"), "dependency_scope", "compile")
	requireField(t, requireRow(t, payload, "org.scalatest:scalatest"), "dependency_scope", "test")
	requireField(t, requireRow(t, payload, "org.scalatest:scalatest"), "sbt_configuration", "Test")
	requireField(t, requireRow(t, payload, "org.scalatest:scalatest"), "section", "libraryDependencies:Test")
	requireField(t, requireRow(t, payload, "javax.servlet:servlet-api"), "dependency_scope", "provided")
	requireField(t, requireRow(t, payload, "ch.qos.logback:logback-classic"), "dependency_scope", "runtime")
	// The later `:=` in the same settings replaces the `++=` list, so those
	// four rows keep their parsed scope but are superseded and partial.
	for _, name := range []string{"org.typelevel:cats-effect", "org.scalatest:scalatest", "javax.servlet:servlet-api", "ch.qos.logback:logback-classic"} {
		requireField(t, requireRow(t, payload, name), "dependency_resolution_state", "partial")
	}
	circe := requireRow(t, payload, "io.circe:circe-core")
	requireField(t, circe, "value", "0.14.6")
	requireField(t, circe, "dependency_resolution_state", "resolved")
	requireField(t, stateOf(t, payload), "unresolved_declaration_count", 4)
}

func TestParseScalaVersionForms(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`scalaVersion := "2.13.12"`:               "2.13.12",
		`ThisBuild / scalaVersion := "3.3.1"`:     "3.3.1",
		`scalaVersion in ThisBuild := "2.12.18"`:  "2.12.18",
		`ThisBuild / scalaVersion := """3.4.0"""`: "3.4.0",
	}
	for source, want := range cases {
		payload := parseSource(t, source+"\n")
		if got := stateOf(t, payload)["scala_version"]; got != want {
			t.Errorf("%q scala_version = %#v, want %q", source, got, want)
		}
	}
}

// TestParseScalaVersionCountsDistinctAndReportsNoneWhenTwo pins the
// fail-closed envelope: two distinct literals are counted, and no single
// version is reported, not even the ThisBuild one, because the scanner does
// not decide which assignment sbt applies last.
func TestParseScalaVersionCountsDistinctAndReportsNoneWhenTwo(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `lazy val a = project.settings(scalaVersion := "2.12.18")
ThisBuild / scalaVersion := "2.13.12"
lazy val b = project.settings(scalaVersion := "2.13.12")
`)
	state := stateOf(t, payload)
	requireStateField(t, state, "scala_version", "")
	requireStateField(t, state, "scala_version_count", 2)
}

func TestParseNonLiteralScalaVersionIsNotReported(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `scalaVersion := scala213
scalaVersion := s"2.13.${patch}"
`)
	state := stateOf(t, payload)
	requireStateField(t, state, "scala_version", "")
	requireStateField(t, state, "scala_version_count", 0)
	requireStateField(t, state, "unresolved_declaration_count", 2)
}

func TestParseResolvesTopLevelStringValsAndReportsTheRest(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `val catsVersion = "2.10.0"
lazy val circeVersion: String = "0.14.6"
libraryDependencies ++= Seq(
  "org.typelevel" %% "cats-core" % catsVersion,
  "io.circe" %% "circe-core" % circeVersion,
  "org.http4s" %% "http4s-dsl" % http4sVersion,
  "org.tpolecat" %% "doobie-core" % Versions.doobie.value,
  "co.fs2" %% "fs2-core" % s"3.${minor}.0"
)
`)

	cats := requireRow(t, payload, "org.typelevel:cats-core")
	requireField(t, cats, "value", "2.10.0")
	requireField(t, cats, "dependency_resolution_state", "resolved")
	requireField(t, requireRow(t, payload, "io.circe:circe-core"), "value", "0.14.6")

	http4s := requireRow(t, payload, "org.http4s:http4s-dsl")
	requireField(t, http4s, "value", "http4sVersion")
	requireField(t, http4s, "dependency_resolution_state", "unresolved")
	assertKeys(t, http4s, []string{"http4sVersion"})

	doobie := requireRow(t, payload, "org.tpolecat:doobie-core")
	requireField(t, doobie, "dependency_resolution_state", "unresolved")
	assertKeys(t, doobie, []string{"Versions.doobie.value"})

	requireField(t, requireRow(t, payload, "co.fs2:fs2-core"), "dependency_resolution_state", "unresolved")

	// An envelope-only consumer must see the three rows whose revision could
	// not be resolved, the same way a non-literal scalaVersion is counted.
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 3)
}

func TestParseNestedValIsNotResolved(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `lazy val root = project.settings({
  val inner = "1.0.0"
  libraryDependencies += "a" % "b" % inner
})
`)
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "unresolved")
}

func TestParseNonLiteralCoordinatesAreCountedNotEmitted(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies += organization.value %% name.value % "1.0"
libraryDependencies += "org" %% artifactName % "1.0"
libraryDependencies ++= commonDeps
libraryDependencies ++= Seq(Dependencies.cats, "ok" % "ok" % "1")
`)
	rows := dependencyRows(t, payload)
	if len(rows) != 1 || rows[0]["name"] != "ok:ok" {
		t.Fatalf("rows = %#v, want only ok:ok", rows)
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 4)
}

func TestParseToleratesDependencyModifiers(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies ++= Seq(
  "org.apache.spark" %% "spark-core" % "3.5.0" exclude("org.slf4j", "slf4j-log4j12"),
  "com.lihaoyi" % "ammonite" % "3.0.0" cross CrossVersion.full,
  "org.foo" %% "bar" % "1.0".withSources().withJavadoc(),
  ("org.baz" % "qux" % "2.0").intransitive()
)
`)
	// The dotted and parenthesised spellings are outside the grammar, so the
	// whole statement falls back: every coordinate keeps its literal fields
	// and is partial.
	spark := requireRow(t, payload, "org.apache.spark:spark-core")
	requireField(t, spark, "value", "3.5.0")
	requireField(t, requireRow(t, payload, "com.lihaoyi:ammonite"), "sbt_cross_version", "custom")
	requireField(t, requireRow(t, payload, "org.foo:bar"), "value", "1.0")
	requireField(t, requireRow(t, payload, "org.baz:qux"), "value", "2.0")
	if got := len(dependencyRows(t, payload)); got != 4 {
		t.Fatalf("row count = %d, want 4: %#v", got, payload["variables"])
	}
	for _, row := range dependencyRows(t, payload) {
		requireField(t, row, "dependency_resolution_state", "partial")
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 4)

	// The infix spellings alone match the grammar and stay resolved.
	infix := parseSource(t, `libraryDependencies ++= Seq(
  "org.apache.spark" %% "spark-core" % "3.5.0" exclude("org.slf4j", "slf4j-log4j12"),
  "com.lihaoyi" % "ammonite" % "3.0.0" cross CrossVersion.full,
  "org.foo" %% "bar" % "1.0" withSources() withJavadoc()
)
`)
	for _, row := range dependencyRows(t, infix) {
		requireField(t, row, "dependency_resolution_state", "resolved")
	}
	requireField(t, requireRow(t, infix, "com.lihaoyi:ammonite"), "sbt_cross_version", "custom")
	requireStateField(t, stateOf(t, infix), "unresolved_declaration_count", 0)
}

func TestParseDynamicRevisionsAreNotResolved(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies ++= Seq(
  "a" % "plus" % "1.+",
  "a" % "latest" % "latest.release",
  "a" % "range" % "[1.0,2.0)",
  "a" % "snap" % "1.0-SNAPSHOT"
)
`)
	for _, name := range []string{"a:plus", "a:latest", "a:range"} {
		requireField(t, requireRow(t, payload, name), "dependency_resolution_state", "partial")
	}
	requireField(t, requireRow(t, payload, "a:snap"), "dependency_resolution_state", "resolved")
}

func TestParseMissingRevisionAndTypedCollection(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies += "a" % "norev"
libraryDependencies ++= Seq[ModuleID]("b" % "typed" % "2")
libraryDependencies += "c" % "after" % "3"
`)
	rows := dependencyRows(t, payload)
	if len(rows) != 2 {
		t.Fatalf("rows = %#v, want typed and after", rows)
	}
	// A type argument is outside the grammar: the typed row is partial.
	typed := requireRow(t, payload, "b:typed")
	requireField(t, typed, "value", "2")
	requireField(t, typed, "dependency_resolution_state", "partial")
	requireField(t, requireRow(t, payload, "c:after"), "dependency_resolution_state", "resolved")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
}

func TestParseSettingsListKeepsLaterSettings(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `lazy val core = (project in file("core")).settings(libraryDependencies += "a" % "b" % "1", name := "core", libraryDependencies += "c" % "d" % "2")
`)
	if got := len(dependencyRows(t, payload)); got != 2 {
		t.Fatalf("rows = %d, want 2: %#v", got, payload["variables"])
	}
}

func TestParseDerivesBinaryArtifactFromSingleLiteralScalaVersion(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `scalaVersion := "2.13.12"
libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
libraryDependencies += "com.google.guava" % "guava" % "33.0.0-jre"
`)
	requireField(t, requireRow(t, payload, "org.typelevel:cats-core"), "sbt_binary_artifact", "org.typelevel:cats-core_2.13")
	if _, ok := requireRow(t, payload, "com.google.guava:guava")["sbt_binary_artifact"]; ok {
		t.Fatalf("plain %% dependency must not carry sbt_binary_artifact")
	}

	scala3 := parseSource(t, `scalaVersion := "3.3.1"
libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
`)
	requireField(t, requireRow(t, scala3, "org.typelevel:cats-core"), "sbt_binary_artifact", "org.typelevel:cats-core_3")

	ambiguous := parseSource(t, `lazy val a = project.settings(scalaVersion := "2.12.18")
lazy val b = project.settings(scalaVersion := "2.13.12")
libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"
`)
	if _, ok := requireRow(t, ambiguous, "org.typelevel:cats-core")["sbt_binary_artifact"]; ok {
		t.Fatalf("ambiguous scalaVersion must not derive a binary artifact")
	}
}

// TestParseScopedLibraryDependencies pins the axis rule: any axis before the
// key, `Test /` and `ThisBuild /` alike, takes the statement out of the
// grammar, so the rows are partial with a placeholder scope.
func TestParseScopedLibraryDependencies(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `Test / libraryDependencies += "org.scalatest" %% "scalatest" % "3.2.17"
ThisBuild / libraryDependencies += "a" % "b" % "1"
`)
	scoped := requireRow(t, payload, "org.scalatest:scalatest")
	requireField(t, scoped, "dependency_resolution_state", "partial")
	requireField(t, scoped, "dependency_scope", "compile")
	requireField(t, requireRow(t, payload, "a:b"), "dependency_scope", "compile")
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
}

func TestParseIgnoresNonDeclarations(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `// libraryDependencies += "commented" % "out" % "1.0"
/* libraryDependencies += "block" % "comment" % "1.0"
   /* nested */ libraryDependencies += "still" % "comment" % "1.0" */
val note = "libraryDependencies += \"in\" % \"string\" % \"1.0\""
val raw = """libraryDependencies += "in" % "triple" % "1.0""""
val loose = "not" % "a" % "dependency"
lazy val core = project.dependsOn(util)
libraryDependencies.value
scalaVersion.value
`)
	if rows := dependencyRows(t, payload); len(rows) != 0 {
		t.Fatalf("rows = %#v, want none", rows)
	}
	state := stateOf(t, payload)
	requireStateField(t, state, "unresolved_declaration_count", 0)
	requireStateField(t, state, "scala_version", "")
}

func TestParseEmptyAndMalformedInputYieldsEmptyParsedState(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"empty":              "",
		"unterminated":       `libraryDependencies += "org" % "name`,
		"unterminated block": "/* libraryDependencies += \"a\" % \"b\" % \"1\"",
		"dangling":           `libraryDependencies +=`,
		"eof after org":      `libraryDependencies += "org" %%`,
		"stray":              `))))] }} libraryDependencies ++= Seq(`,
	} {
		payload := parseSource(t, source)
		if rows := dependencyRows(t, payload); len(rows) != 0 {
			t.Errorf("%s: rows = %#v, want none", name, rows)
		}
		requireStateField(t, stateOf(t, payload), "state", "parsed")
	}
}

func TestParseHandlesCRLFAndBareCarriageReturns(t *testing.T) {
	t.Parallel()

	crlf := parseSource(t, "// c\r\nlibraryDependencies += \"a\" % \"b\" % \"1\"\r\nlibraryDependencies += \"c\" % \"d\" % \"2\"\r\n")
	if got := len(dependencyRows(t, crlf)); got != 2 {
		t.Fatalf("CRLF row count = %d, want 2", got)
	}
	requireField(t, requireRow(t, crlf, "c:d"), "line_number", 3)

	cr := parseSource(t, "// c\rlibraryDependencies += \"a\" % \"b\" % \"1\"\rlibraryDependencies += \"c\" % \"d\" % \"2\"\r")
	if got := len(dependencyRows(t, cr)); got != 2 {
		t.Fatalf("bare CR row count = %d, want 2", got)
	}
}

func TestParseRowsAreSortedAndDeterministic(t *testing.T) {
	t.Parallel()

	source := `libraryDependencies ++= Seq("z" % "z" % "1", "a" % "a" % "1", "m" % "m" % "1")`
	first := dependencyRows(t, parseSource(t, source))
	second := dependencyRows(t, parseSource(t, source))
	want := []string{"a:a", "m:m", "z:z"}
	for index, name := range want {
		if first[index]["name"] != name || second[index]["name"] != name {
			t.Fatalf("row %d = %v / %v, want %s", index, first[index]["name"], second[index]["name"], name)
		}
	}
}

func requireStateField(t *testing.T, state map[string]any, key string, want any) {
	t.Helper()
	if got := state[key]; got != want {
		t.Fatalf("sbt_state[%s] = %#v, want %#v", key, got, want)
	}
}

func assertKeys(t *testing.T, row map[string]any, want []string) {
	t.Helper()
	got, ok := row["dependency_unresolved_keys"].([]string)
	if !ok || len(got) != len(want) {
		t.Fatalf("%v dependency_unresolved_keys = %#v, want %v", row["name"], row["dependency_unresolved_keys"], want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%v dependency_unresolved_keys = %#v, want %v", row["name"], got, want)
		}
	}
}
