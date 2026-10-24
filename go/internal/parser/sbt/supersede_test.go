// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// earlierRow is the resolved row each superseding case declares first.
const earlierRow = "libraryDependencies += \"a\" % \"b\" % \"1\"\n"

// TestDottedNonSettingMemberIsDenied pins the default-deny side of the member
// rule. Only a read member (`value`, `key`, `scopedKey`, `taskValue`) leaves
// the key alone. Any other member that returns no Setting (`map`, `flatMap`,
// `get`, an unknown name) is an occurrence the scanner does not recognise, so
// it supersedes every row of the file and is counted once, wherever the key
// sits.
func TestDottedNonSettingMemberIsDenied(t *testing.T) {
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
		if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got != "partial" {
			t.Errorf("%s: a:b state = %v, want partial: an unrecognised member is denied", name, got)
		}
		if got := len(dependencyRows(t, payload)); got != 1 {
			t.Errorf("%s: rows = %d, want 1", name, got)
		}
	}

	// In a project's settings the earlier row of the same project is
	// superseded too.
	project := parseSource(t, "lazy val core = project.settings(libraryDependencies += \"a\" % \"b\" % \"1\", libraryDependencies.map(_.size))\n")
	requireField(t, requireRow(t, project, "a:b"), "dependency_resolution_state", "partial")

	// The statement is counted once, and the superseded row once.
	counted := parseSource(t, earlierRow+"libraryDependencies.map(_.withSources())\n")
	requireStateField(t, stateOf(t, counted), "unresolved_declaration_count", 2)
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
		"nested transform":  "lazy val common = Seq(libraryDependencies.transform(identity, pos))",
		"dotted assignment": "libraryDependencies.:=(Seq(\"c\" % \"d\" % \"2\"))",
	} {
		payload := parseSource(t, earlierRow+source+"\n")
		if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got == "resolved" {
			t.Errorf("%s: a:b resolved; a member that rewrites the key supersedes it", name)
		}
	}
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

	// A comparison, another non-assignment operator, or a local definition
	// that shadows the key is no recognised occurrence: it is denied and
	// counted once.
	for _, source := range []string{
		"check := libraryDependencies == Nil",
		"check := libraryDependencies <= x",
		"check := libraryDependencies != Nil",
		"def libraryDependencies = Nil",
	} {
		payload := parseSource(t, source+"\n")
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
	}
}

// TestRewriteMemberAnywhereSupersedes pins a rewriting member at any position.
// A Setting-returning member (`set`, `set0`, `transform`, `remove1`,
// `removeN`, `withRank` before an assignment) changes the key wherever the
// Setting it returns is applied, so a member outside statement position
// supersedes like a `:=` in the same place: out of a grammar context it is a
// global reset, every row becomes partial, and the statement is counted once.
func TestRewriteMemberAnywhereSupersedes(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"val rhs transform": "lazy val s = libraryDependencies.transform(_ => Nil, NoPosition)",
		"typed val set":     "lazy val s: Setting[_] = libraryDependencies.set(Def.setting(Nil), pos)",
		"after :+":          "commonSettings :+ libraryDependencies.transform(identity, pos)",
		"nested":            "lazy val common = Seq(foo(1), bar(libraryDependencies.remove1(Def.setting(x), pos)))",
		"after opener":      "check := (libraryDependencies.removeN(Def.setting(xs), pos))",
		"lambda body":       "check := { deps => libraryDependencies.set0(Def.setting(Nil), pos) }",
		"withRank rhs":      "lazy val s = libraryDependencies.withRank(KeyRanks.Invisible) := Nil",
		"after operator":    "lazy val all = base ++ libraryDependencies.transform(identity, pos)",
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, earlierRow+source+"\n")
			requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
			requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
		})
	}

	// The reset is global, so a project row is superseded too.
	project := parseSource(t, "lazy val core = project.settings(libraryDependencies += \"a\" % \"b\" % \"1\")\nlazy val s = libraryDependencies.transform(identity, pos)\n")
	requireField(t, requireRow(t, project, "a:b"), "dependency_resolution_state", "partial")

	// Each occurrence is counted, so both branches of an if count.
	branches := parseSource(t, earlierRow+"if (ci) libraryDependencies.set(Def.setting(Nil), pos) else libraryDependencies.set(Def.setting(Seq()), pos)\n")
	requireField(t, requireRow(t, branches, "a:b"), "dependency_resolution_state", "partial")
	requireStateField(t, stateOf(t, branches), "unresolved_declaration_count", 3)

	// A coordinate later in the same statement is still emitted partial.
	tail := parseSource(t, earlierRow+"if (ci) libraryDependencies.set(Def.setting(Nil), pos) else libraryDependencies += \"c\" % \"d\" % \"2\"\n")
	requireField(t, requireRow(t, tail, "a:b"), "dependency_resolution_state", "partial")
	requireField(t, requireRow(t, tail, "c:d"), "dependency_resolution_state", "partial")

	// Reads stay reads at any position: not counted, nothing superseded.
	for name, source := range map[string]string{
		"value rhs": "lazy val s = libraryDependencies.value",
		"key rhs":   "lazy val k = libraryDependencies.key",
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, earlierRow+source+"\n")
			requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "resolved")
			requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
		})
	}
}

// TestWithRankReadsTheOperatorAfterIt pins `withRank(...)` followed by an
// operator the scanner can read. `+=`, `++=`, `<+=`, `<++=` and any other
// assignment operator append, so the earlier row stays resolved and the
// coordinates are partial; `:=` and `<<=` replace and `~=`, `-=`, `--=`
// rewrite, so they supersede. A continuation the scanner cannot read keeps
// the fail-closed supersede.
func TestWithRankReadsTheOperatorAfterIt(t *testing.T) {
	t.Parallel()

	appends := map[string]string{
		"+=":   "libraryDependencies.withRank(KeyRanks.Invisible) += \"c\" % \"d\" % \"2\"",
		"++=":  "libraryDependencies.withRank(KeyRanks.Invisible) ++= Seq(\"c\" % \"d\" % \"2\")",
		"<+=":  "libraryDependencies.withRank(KeyRanks.Invisible) <+= Def.setting(\"c\" % \"d\" % \"2\")",
		"<++=": "libraryDependencies.withRank(KeyRanks.Invisible) <++= Def.setting(Seq(\"c\" % \"d\" % \"2\"))",
		":+=":  "libraryDependencies.withRank(KeyRanks.Invisible) :+= \"c\" % \"d\" % \"2\"",
	}
	for name, source := range appends {
		t.Run("append "+name, func(t *testing.T) {
			payload := parseSource(t, earlierRow+source+"\n")
			requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "resolved")
			requireField(t, requireRow(t, payload, "c:d"), "dependency_resolution_state", "partial")
			requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
		})
	}

	replaces := map[string]string{
		":=":      "libraryDependencies.withRank(KeyRanks.Invisible) := Nil",
		"<<=":     "libraryDependencies.withRank(KeyRanks.Invisible) <<= otherDeps",
		"~=":      "libraryDependencies.withRank(KeyRanks.Invisible) ~= identity",
		"-=":      "libraryDependencies.withRank(KeyRanks.Invisible) -= \"c\" % \"d\" % \"2\"",
		"--=":     "libraryDependencies.withRank(KeyRanks.Invisible) --= otherDeps",
		"unknown": "libraryDependencies.withRank(KeyRanks.Invisible).foo(1)",
	}
	for name, source := range replaces {
		t.Run("replace "+name, func(t *testing.T) {
			payload := parseSource(t, earlierRow+source+"\n")
			requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
			if got := len(dependencyRows(t, payload)); got != 1 {
				t.Errorf("rows = %d, want 1: the operand of a rewrite emits no row", got)
			}
			requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
		})
	}
}

// TestAxisThenMemberFollowsMemberRule pins a member called after a legacy
// `.in(...)` axis. The axis takes the statement out of the grammar, so the
// member is judged as it is without the axis and any reset is global: a
// rewriting member or `withRank(...)` supersedes the earlier row, a member
// that returns no Setting is denied like one without the axis, and the dotted
// `.:=(...)` call replaces the key like `:=`.
func TestAxisThenMemberFollowsMemberRule(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"transform":       "libraryDependencies.in(Test).transform(f, pos)",
		"set":             "libraryDependencies.in(Test).set(Def.setting(Nil), pos)",
		"withRank assign": "libraryDependencies.in(Test).withRank(r) := Nil",
		"dot assign":      "libraryDependencies.in(Test).:=(Nil)",
		"plain dot call":  "libraryDependencies.:=(Nil)",
		"non-Setting":     "libraryDependencies.in(Test).map(_.withSources())",
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, earlierRow+source+"\n")
			if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got == "resolved" {
				t.Errorf("a:b state = %v, want superseded", got)
			}
			requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
		})
	}

	read := parseSource(t, earlierRow+"lazy val s = libraryDependencies.in(Test).value\n")
	requireField(t, requireRow(t, read, "a:b"), "dependency_resolution_state", "resolved")
	requireStateField(t, stateOf(t, read), "unresolved_declaration_count", 0)
}
