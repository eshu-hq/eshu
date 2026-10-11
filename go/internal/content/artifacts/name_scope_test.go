// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rowConstructorKinds reads the two call-site families of the extractors from
// source. scoped holds the evidence kinds passed to appendConfigArtifactRow,
// the only constructor that strips the repository name from the path.
// unscoped holds the kinds the Ansible and kustomize extractors build directly.
func rowConstructorKinds(t *testing.T) (scoped, unscoped map[string]bool) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	scopedCall := regexp.MustCompile(`appendConfigArtifactRow\(.*"([a-z_]+)"\)\s*$`)
	addRowCall := regexp.MustCompile(`addRow\(.*"([a-z_]+)"\)`)
	rowLiteral := regexp.MustCompile(`"evidence_kind":\s*"([a-z_]+)"`)
	scoped, unscoped = map[string]bool{}, map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if m := scopedCall.FindStringSubmatch(line); m != nil {
				scoped[m[1]] = true
			}
			for _, re := range []*regexp.Regexp{addRowCall, rowLiteral} {
				if m := re.FindStringSubmatch(line); m != nil {
					unscoped[m[1]] = true
				}
			}
		}
	}
	return scoped, unscoped
}

// nameScopeViolations compares a name-scope table against the kinds the row
// constructors emit and returns one message per disagreement: a kind in both
// families, a kind in neither, a table value that contradicts the family of
// the constructor, and a table entry no constructor emits.
func nameScopeViolations(table map[string]bool, scoped, unscoped map[string]bool) []string {
	var out []string
	kinds := map[string]struct{}{}
	for kind := range scoped {
		kinds[kind] = struct{}{}
	}
	for kind := range unscoped {
		kinds[kind] = struct{}{}
	}
	for kind := range table {
		kinds[kind] = struct{}{}
	}
	for kind := range kinds {
		value, inTable := table[kind]
		switch {
		case scoped[kind] && unscoped[kind]:
			out = append(out, kind+": built both through appendConfigArtifactRow and directly")
		case !scoped[kind] && !unscoped[kind]:
			out = append(out, kind+": in the table but emitted by no row constructor")
		case !inTable:
			out = append(out, kind+": emitted by a row constructor but missing from the table")
		case scoped[kind] && !value:
			out = append(out, kind+": built through appendConfigArtifactRow but the table says unscoped")
		case unscoped[kind] && value:
			out = append(out, kind+": built directly but the table says name-scoped")
		}
	}
	sort.Strings(out)
	return out
}

// TestNameScopeTableMatchesTheRowConstructors is the second S0 guard of the
// #7126 decision record: each evidence_kind maps to exactly one name_scoped
// value, and the table agrees with how the extractors build the row. A future
// extractor that emits a new kind, or one kind through both constructors, fails
// here instead of storing a row whose read-time name step is ambiguous.
func TestNameScopeTableMatchesTheRowConstructors(t *testing.T) {
	scoped, unscoped := rowConstructorKinds(t)
	if len(scoped) != 10 || len(unscoped) != 6 {
		t.Fatalf("constructor scan found %d scoped kinds %v and %d unscoped kinds %v, want 10 HCL and compose, 5 Ansible and 1 kustomize",
			len(scoped), scoped, len(unscoped), unscoped)
	}
	if violations := nameScopeViolations(nameScopedByEvidenceKind, scoped, unscoped); len(violations) != 0 {
		t.Fatalf("name-scope table disagrees with the row constructors:\n  %s", strings.Join(violations, "\n  "))
	}
	for _, kind := range EvidenceKinds() {
		if _, known := NameScoped(kind); !known {
			t.Errorf("NameScoped(%q) is unknown for a listed kind", kind)
		}
	}
	if _, known := NameScoped("no_such_kind"); known {
		t.Error("NameScoped reports a kind no extractor emits as known")
	}
}

// TestNameScopeGuardCatchesAWrongTable is the seeded RED leg of the guard. Each
// planted fault must produce a violation on the same scan that is clean on the
// real table.
func TestNameScopeGuardCatchesAWrongTable(t *testing.T) {
	scoped, unscoped := rowConstructorKinds(t)

	clone := func() map[string]bool {
		out := make(map[string]bool, len(nameScopedByEvidenceKind))
		for kind, value := range nameScopedByEvidenceKind {
			out[kind] = value
		}
		return out
	}
	if got := nameScopeViolations(clone(), scoped, unscoped); len(got) != 0 {
		t.Fatalf("the clean table has violations: %v", got)
	}

	cases := map[string]func(table map[string]bool, scoped, unscoped map[string]bool){
		"a compose kind flipped to unscoped": func(table map[string]bool, _, _ map[string]bool) {
			table["docker_compose_env_file"] = false
		},
		"an ansible kind flipped to scoped": func(table map[string]bool, _, _ map[string]bool) {
			table["ansible_vars"] = true
		},
		"an emitted kind missing from the table": func(table map[string]bool, _, _ map[string]bool) {
			delete(table, "terraform_var_file")
		},
		"a table kind no constructor emits": func(table map[string]bool, _, _ map[string]bool) {
			table["terraform_made_up_kind"] = true
		},
		"one kind built through both constructors": func(_ map[string]bool, scoped, unscoped map[string]bool) {
			unscoped["terraform_var_file"] = true
		},
	}
	for name, mutate := range cases {
		table := clone()
		s, u := map[string]bool{}, map[string]bool{}
		for kind := range scoped {
			s[kind] = true
		}
		for kind := range unscoped {
			u[kind] = true
		}
		mutate(table, s, u)
		if got := nameScopeViolations(table, s, u); len(got) == 0 {
			t.Errorf("fault %q was not caught", name)
		}
	}
}

// TestEveryEmittedEvidenceKindIsInTheNameScopeTable runs the corpus the
// name-free proof uses and requires every emitted kind to have a table entry,
// so a kind reachable only through content (not through the source scan) is
// still covered.
func TestEveryEmittedEvidenceKindIsInTheNameScopeTable(t *testing.T) {
	agg := nameFreeRunCorpus(t, nameFreeApply{scoped: nameFreeScopedKinds(t)}, 60)
	for kind, rows := range agg.kindRows {
		if _, known := NameScoped(kind); !known {
			t.Errorf("emitted evidence_kind %q (%d rows) has no name-scope table entry", kind, rows)
		}
	}
	if len(agg.kindRows) < 14 {
		t.Errorf("corpus emitted %d distinct evidence kinds; want at least 14 of 16", len(agg.kindRows))
	}
}
