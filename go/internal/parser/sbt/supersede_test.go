// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// earlierRow is the resolved row each superseding case declares first.
const earlierRow = "libraryDependencies += \"a\" % \"b\" % \"1\"\n"

// TestDottedNonSettingMemberDoesNotSupersede pins the read side of the member
// rule. In sbt's SettingKey API (main-settings Structure.scala) only `:=`,
// `+=`, `++=`, `-=`, `--=`, `~=`, `<+=`, `<++=`, `<<=`, `set`, `transform`,
// `append1`, `appendN`, `remove1` and `removeN` return a Setting; `map`,
// `flatMap`, `get`, `or` and the rest return an Initialize or a plain value,
// which cannot change the key. Such a statement is counted once and leaves the
// earlier row resolved, wherever the key sits.
func TestDottedNonSettingMemberDoesNotSupersede(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"map":            "libraryDependencies.map(_.withSources())",
		"filter":         "libraryDependencies.filter(_.name != \"x\")",
		"flatMap":        "libraryDependencies.flatMap(x => x)",
		"distinct":       "libraryDependencies.distinct",
		"previous":       "libraryDependencies.previous",
		"zipWith":        "libraryDependencies.zipWith(scalaVersion)((a, b) => a)",
		"unknown":        "libraryDependencies.foo(1)",
		"after opener":   "check := (libraryDependencies.map(_.size)).value",
		"settings arg":   "lazy val core = project.settings(libraryDependencies.map(_.size))",
		"settings val":   "lazy val common = Seq(libraryDependencies.map(_.size))",
		"next statement": "libraryDependencies.map(_.size); libraryDependencies.filter(x => true)",
	} {
		payload := parseSource(t, earlierRow+source+"\n")
		if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got != "resolved" {
			t.Errorf("%s: a:b state = %v, want resolved: a member that returns no Setting cannot change the key", name, got)
		}
		if got := len(dependencyRows(t, payload)); got != 1 {
			t.Errorf("%s: rows = %d, want 1", name, got)
		}
	}

	// In a project's settings the earlier row of the same project stays
	// resolved too.
	project := parseSource(t, "lazy val core = project.settings(libraryDependencies += \"a\" % \"b\" % \"1\", libraryDependencies.map(_.size))\n")
	requireField(t, requireRow(t, project, "a:b"), "dependency_resolution_state", "resolved")

	// The statement is counted once; the earlier row is not.
	counted := parseSource(t, earlierRow+"libraryDependencies.map(_.withSources())\n")
	requireStateField(t, stateOf(t, counted), "unresolved_declaration_count", 1)
}

// TestDottedSettingMemberSupersedes pins the write side: the members that
// return a Setting which replaces or removes what came before (`set`, sbt 2's
// `set0`, `transform`, `remove1`, `removeN`) supersede the earlier row, and so
// does `withRank`, which returns the key itself so an assignment the scanner
// does not read may follow.
func TestDottedSettingMemberSupersedes(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"transform":         "libraryDependencies.transform(_.map(_.withSources()), pos)",
		"set":               "libraryDependencies.set(Def.setting(Nil), pos)",
		"set0":              "libraryDependencies.set0(Def.setting(Nil), pos)",
		"remove1":           "libraryDependencies.remove1(Def.setting(x), pos)",
		"removeN":           "libraryDependencies.removeN(Def.setting(xs), pos)",
		"withRank assign":   "libraryDependencies.withRank(KeyRanks.Invisible) := Nil",
		"withRank append":   "libraryDependencies.withRank(KeyRanks.Invisible) ++= Seq(\"c\" % \"d\" % \"2\")",
		"nested transform":  "lazy val common = Seq(libraryDependencies.transform(identity, pos))",
		"dotted assignment": "libraryDependencies.:=(Seq(\"c\" % \"d\" % \"2\"))",
	} {
		payload := parseSource(t, earlierRow+source+"\n")
		if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got == "resolved" {
			t.Errorf("%s: a:b resolved; a member that rewrites the key supersedes it", name)
		}
	}

	ranked := parseSource(t, earlierRow+"libraryDependencies.withRank(KeyRanks.Invisible) ++= Seq(\"c\" % \"d\" % \"2\")\n")
	requireField(t, requireRow(t, ranked, "c:d"), "dependency_resolution_state", "partial")
}

// TestDottedAppendMemberFallsBackWithoutSuperseding pins `append1` and
// `appendN`: they add like `+=` and `++=`, so their literal coordinates are
// partial rows and the earlier row stays resolved.
func TestDottedAppendMemberFallsBackWithoutSuperseding(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"append1": "libraryDependencies.append1(Def.setting(\"c\" % \"d\" % \"2\"), pos)",
		"appendN": "libraryDependencies.appendN(Def.setting(Seq(\"c\" % \"d\" % \"2\")), pos)",
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, earlierRow+source+"\n")
			requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "resolved")
			requireField(t, requireRow(t, payload, "c:d"), "dependency_resolution_state", "partial")
			requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
		})
	}
}

// TestLegacyAndUnknownAssignmentOperatorsFallBack pins the operators outside
// the grammar. sbt 0.13's `<<=` replaces the key, so it supersedes; `<+=` and
// `<++=` append; any other Scala assignment operator is not an sbt setting
// operator. Each statement is counted and its literal coordinates are emitted
// partial, never dropped.
func TestLegacyAndUnknownAssignmentOperatorsFallBack(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		source    string
		supersede bool
	}{
		{"replace", "libraryDependencies <<= Def.setting(Seq(\"c\" % \"d\" % \"2\"))", true},
		{"append one", "libraryDependencies <+= Def.setting(\"c\" % \"d\" % \"2\")", false},
		{"append many", "libraryDependencies <++= Def.setting(Seq(\"c\" % \"d\" % \"2\"))", false},
		{"unknown", "libraryDependencies :+= \"c\" % \"d\" % \"2\"", false},
		{"unknown pipe", "libraryDependencies |= Seq(\"c\" % \"d\" % \"2\")", false},
	}
	for _, tc := range cases {
		payload := parseSource(t, earlierRow+tc.source+"\n")
		requireField(t, requireRow(t, payload, "c:d"), "dependency_resolution_state", "partial")
		earlier := requireRow(t, payload, "a:b")["dependency_resolution_state"]
		if tc.supersede && earlier == "resolved" {
			t.Errorf("%s: a:b resolved; `<<=` replaces the key", tc.name)
		}
		if !tc.supersede && earlier != "resolved" {
			t.Errorf("%s: a:b state = %v, want resolved", tc.name, earlier)
		}
		if got, _ := stateOf(t, payload)["unresolved_declaration_count"].(int); got < 1 {
			t.Errorf("%s: unresolved_declaration_count = %d, want at least 1", tc.name, got)
		}
	}

	// A statement with no coordinate is still counted, not dropped.
	bare := parseSource(t, "libraryDependencies <<= otherDeps\n")
	requireStateField(t, stateOf(t, bare), "unresolved_declaration_count", 1)

	// Comparison and other non-assignment operators are not settings.
	for _, source := range []string{
		"check := libraryDependencies == Nil",
		"check := libraryDependencies <= x",
		"check := libraryDependencies != Nil",
		"def libraryDependencies = Nil",
	} {
		payload := parseSource(t, source+"\n")
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
	}
}
