// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// keyListReferences are statements of an allow-listed key-list setting whose
// value names either key as a plain reference. Each is a read of the key, not
// a rewrite.
var keyListReferences = []string{
	`buildInfoKeys := Seq[BuildInfoKey](name, version, scalaVersion, sbtVersion)`,
	`buildInfoKeys ++= Seq[BuildInfoKey](scalaVersion, libraryDependencies)`,
	`buildInfoKeys += scalaVersion`,
	`buildInfoKeys += libraryDependencies`,
	`buildInfoKeys := Seq[BuildInfoKey](name, scalaVersion, BuildInfoKey.action("t")(System.currentTimeMillis))`,
	"buildInfoKeys := Seq[BuildInfoKey](\n  name,\n  scalaVersion,\n  libraryDependencies\n)",
	`Global / excludeLintKeys += libraryDependencies`,
	`Global / excludeLintKeys ++= Set(scalaVersion, libraryDependencies)`,
	`excludeLintKeys := Set(scalaVersion)`,
	`ThisBuild / excludeLintKeys ++= Set(Compile / scalaVersion, Test / libraryDependencies)`,
	`includeLintKeys += libraryDependencies`,
	`lazy val root = project.settings(buildInfoKeys := Seq[BuildInfoKey](name, scalaVersion), buildInfoPackage := "x")`,
	`lazy val root = project.settings(name := "x", excludeLintKeys += scalaVersion)`,
	`excludeLintKeys ++= Set(scalaVersion); name := "x"`,
}

// TestKeyListReferenceIsARead pins that a key named as a plain element of an
// allow-listed key-list setting keeps the literal scalaVersion, the `%%`
// hint, every resolved row and the unresolved count.
func TestKeyListReferenceIsARead(t *testing.T) {
	t.Parallel()

	for _, line := range keyListReferences {
		for _, source := range []string{
			"scalaVersion := \"2.13.12\"\n" + denyCats + "\n" + line + "\n",
			line + "\nscalaVersion := \"2.13.12\"\n" + denyCats + "\n",
		} {
			payload := parseSource(t, source)
			state := stateOf(t, payload)
			if got := state["scala_version"]; got != "2.13.12" {
				t.Errorf("%q: scala_version = %v, want 2.13.12", source, got)
			}
			if got, _ := state["unresolved_declaration_count"].(int); got != 0 {
				t.Errorf("%q: unresolved_declaration_count = %d, want 0", source, got)
			}
			row := requireRow(t, payload, "org.typelevel:cats-core")
			if got := row["dependency_resolution_state"]; got != "resolved" {
				t.Errorf("%q: cats-core state = %v, want resolved", source, got)
			}
			if got := row["sbt_binary_artifact"]; got != "org.typelevel:cats-core_2.13" {
				t.Errorf("%q: sbt_binary_artifact = %v, want org.typelevel:cats-core_2.13", source, got)
			}
		}
	}
}

// keyReferenceHostile are shapes that name a key as a value but may rewrite
// it, or that the scanner cannot classify; %s is the key. Each is denied: a
// reference is a read only as a plain element of an allow-listed key-list
// setting whose list ends the statement.
var keyReferenceHostile = []string{
	`Seq(%s).map(_ := Nil)`,
	`Set(%s).foreach(k => k := Nil)`,
	`Seq(%s).map(k => k := Nil)`,
	`(%s) := Nil`,
	`foo(%s) := Nil`,
	`Seq(%s) := Nil`,
	`lazy val k = %s; k := Nil`,
	`lazy val k: SettingKey[String] = %s`,
	`check := %s.all(ScopeFilter(inAnyProject)).value`,
	`check := %s.?.value`,
	`check := (%s / aggregate).value`,
	`%s(v => v)`,
	`(%s, sbtVersion) { (s, b) => s }`,
	`%s in Test := Nil`,
	`fooKeys := Seq(%s)`,
	`buildInfoKeys := Seq(%s).map(_ := Nil)`,
	`buildInfoKeys := Seq[BuildInfoKey](%s).map(_ := Nil)`,
	`excludeLintKeys ++= Set(%s).foreach(k => k := Nil)`,
	`excludeLintKeys ++= Set(%s) ++ Set(name)`,
	`buildInfoKeys := Seq(%s := Nil)`,
	`buildInfoKeys := Seq(%s.?)`,
	`buildInfoKeys := Seq(%s / aggregate)`,
	`excludeLintKeys ++= Set(%s in Test)`,
	`excludeLintKeys += %s := Nil`,
	`excludeLintKeys += %s.map(identity)`,
	`excludeLintKeys += (%s)`,
	`buildInfoKeys := %s`,
	`buildInfoKeys -= %s`,
	`buildInfoKeys ~= (_ :+ %s)`,
	`buildInfoKeys.map(_ => Seq(%s))`,
	`inThisBuild(buildInfoKeys := Seq(%s))`,
	`core / buildInfoKeys := Seq(%s)`,
	`lazy val s = buildInfoKeys := Seq(%s)`,
	"buildInfoKeys := Seq(%s)\n  .map(_ := Nil)",
	`buildInfoKeys := Seq(foo(%s))`,
	`buildInfoKeys := foo(%s)`,
	`excludeLintKeys ++= keysOf(%s)`,
	`buildInfoKeys := Seq(%s) :+ name`,
	`excludeLintKeys ++= Set(%s, (scalaVersion in Test)).map(f)`,
	`excludeLintKeys ++= Set(%s, (libraryDependencies in Test)).map(f)`,
	`excludeLintKeys ++= Set(%s, libraryDependencies.in(Test)).map(f)`,
}

// TestKeyReferenceHostileShapesAreDenied pins every hostile shape for both
// keys: scalaVersion poisons the envelope and libraryDependencies makes
// every row partial.
func TestKeyReferenceHostileShapesAreDenied(t *testing.T) {
	t.Parallel()

	for _, shape := range keyReferenceHostile {
		scala := sprintfOne(shape, "scalaVersion")
		for _, source := range []string{
			"scalaVersion := \"2.13.12\"\n" + denyCats + "\n" + scala + "\n",
			scala + "\nscalaVersion := \"2.13.12\"\n" + denyCats + "\n",
		} {
			payload := parseSource(t, source)
			state := stateOf(t, payload)
			if got := state["scala_version"]; got != "" {
				t.Errorf("%q: scala_version = %v, want empty", source, got)
			}
			if got, _ := state["unresolved_declaration_count"].(int); got < 1 {
				t.Errorf("%q: unresolved_declaration_count = %d, want at least 1", source, got)
			}
		}
		lib := sprintfOne(shape, "libraryDependencies")
		for _, source := range []string{denyCats + "\n" + lib + "\n", lib + "\n" + denyCats + "\n"} {
			payload := parseSource(t, source)
			if got := requireRow(t, payload, "org.typelevel:cats-core")["dependency_resolution_state"]; got != "partial" {
				t.Errorf("%q: cats-core state = %v, want partial", source, got)
			}
		}
	}
}
