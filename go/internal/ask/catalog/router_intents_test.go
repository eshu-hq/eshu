// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	minToolIntents   = 3
	maxToolIntents   = 8
	maxIntentWords   = 8
	readmeFamilyHead = "## Router families"
)

// annotatedMCPTools parses the committed inventory, applies the overlay, and
// returns the implemented MCP tool entries.
func annotatedMCPTools(t *testing.T) []Entry {
	t.Helper()
	inventoryJSON, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read surface inventory: %v", err)
	}
	cat, err := Parse(inventoryJSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	var tools []Entry
	for _, e := range cat.Entries() {
		if e.Kind == KindMCPTool {
			tools = append(tools, e)
		}
	}
	if len(tools) == 0 {
		t.Fatal("inventory has no implemented MCP tools")
	}
	return tools
}

// checkIntentCoverage reports every tool whose intent list is outside the
// minToolIntents..maxToolIntents range or holds a malformed phrase.
func checkIntentCoverage(tools []Entry) []string {
	var problems []string
	for _, e := range tools {
		if n := len(e.Intents); n < minToolIntents || n > maxToolIntents {
			problems = append(problems, fmt.Sprintf("%s has %d intents, want %d to %d", e.Name, n, minToolIntents, maxToolIntents))
		}
		seen := map[string]bool{}
		for _, p := range e.Intents {
			switch {
			case strings.TrimSpace(p) != p || p == "":
				problems = append(problems, fmt.Sprintf("%s intent %q is empty or has edge whitespace", e.Name, p))
			case strings.ToLower(p) != p:
				problems = append(problems, fmt.Sprintf("%s intent %q is not lower case", e.Name, p))
			case len(strings.Fields(p)) > maxIntentWords:
				problems = append(problems, fmt.Sprintf("%s intent %q has more than %d words", e.Name, p, maxIntentWords))
			case seen[p]:
				problems = append(problems, fmt.Sprintf("%s repeats intent %q", e.Name, p))
			}
			seen[p] = true
		}
	}
	return problems
}

// checkFamilies reports family-table defects: unknown or repeated members,
// families with fewer than two members, a family with zero or several canonical
// members, and a canonical flag on a tool outside every family.
func checkFamilies(tools []Entry, families []toolFamily) []string {
	byName := map[string]Entry{}
	for _, e := range tools {
		byName[e.Name] = e
	}
	var problems []string
	owner := map[string]string{}
	famNames := map[string]bool{}
	for _, f := range families {
		if famNames[f.Name] {
			problems = append(problems, fmt.Sprintf("family %q is listed twice", f.Name))
		}
		famNames[f.Name] = true
		if len(f.Members) < 2 {
			problems = append(problems, fmt.Sprintf("family %q has %d member(s); a family needs at least 2", f.Name, len(f.Members)))
		}
		if strings.TrimSpace(f.Reason) == "" {
			problems = append(problems, fmt.Sprintf("family %q has no reason for its canonical choice", f.Name))
		}
		var canonical []string
		for _, m := range f.Members {
			e, ok := byName[m]
			if !ok {
				problems = append(problems, fmt.Sprintf("family %q member %q is not an implemented MCP tool", f.Name, m))
				continue
			}
			if prev, dup := owner[m]; dup {
				problems = append(problems, fmt.Sprintf("tool %q is in families %q and %q", m, prev, f.Name))
			}
			owner[m] = f.Name
			if e.Canonical {
				canonical = append(canonical, m)
			}
		}
		if len(canonical) != 1 {
			problems = append(problems, fmt.Sprintf("family %q has %d canonical tools %v, want exactly 1", f.Name, len(canonical), canonical))
		}
	}
	for _, e := range tools {
		if _, in := owner[e.Name]; e.Canonical && !in {
			problems = append(problems, fmt.Sprintf("tool %q is canonical but belongs to no family", e.Name))
		}
	}
	return problems
}

// checkSharedIntents reports an intent phrase carried by two tools of one
// family unless allowed lists it.
func checkSharedIntents(tools []Entry, families []toolFamily, allowed map[string]string) []string {
	byName := map[string]Entry{}
	for _, e := range tools {
		byName[e.Name] = e
	}
	var problems []string
	for _, f := range families {
		holders := map[string][]string{}
		for _, m := range f.Members {
			for _, p := range byName[m].Intents {
				holders[p] = append(holders[p], m)
			}
		}
		for phrase, who := range holders {
			if len(who) > 1 {
				if _, ok := allowed[phrase]; ok {
					continue
				}
				sort.Strings(who)
				problems = append(problems, fmt.Sprintf("family %q: intent %q is shared by %v", f.Name, phrase, who))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func failWith(t *testing.T, header string, problems []string) {
	t.Helper()
	if len(problems) == 0 {
		return
	}
	t.Errorf("%s (%d):\n  %s", header, len(problems), strings.Join(problems, "\n  "))
}

// TestEveryMCPToolHasIntents fails when an implemented MCP tool has fewer than
// three intent phrases, more than eight, or a malformed phrase.
func TestEveryMCPToolHasIntents(t *testing.T) {
	t.Parallel()
	failWith(t, "MCP tool intent coverage", checkIntentCoverage(annotatedMCPTools(t)))
}

// TestFamiliesHaveExactlyOneCanonicalTool fails when a family has zero or more
// than one canonical tool, or a canonical flag sits outside every family.
func TestFamiliesHaveExactlyOneCanonicalTool(t *testing.T) {
	t.Parallel()
	failWith(t, "family table", checkFamilies(annotatedMCPTools(t), toolFamilies()))
}

// TestNoIntentSharedInsideFamily fails when two tools in one family carry the
// same phrase and sharedIntentAllowed does not list it.
func TestNoIntentSharedInsideFamily(t *testing.T) {
	t.Parallel()
	failWith(t, "shared intents", checkSharedIntents(annotatedMCPTools(t), toolFamilies(), sharedIntentAllowed))
}

// TestIntentSourcesAreConsistent fails on an intent key with no overlay entry
// and on a tool curated in two intent files.
func TestIntentSourcesAreConsistent(t *testing.T) {
	t.Parallel()
	overlay := askToolAnnotations()
	seen := map[string]bool{}
	var problems []string
	for _, source := range toolIntentSources() {
		for name := range source {
			if _, ok := overlay[name]; !ok {
				problems = append(problems, fmt.Sprintf("intent key %q has no overlay entry", name))
			}
			if seen[name] {
				problems = append(problems, fmt.Sprintf("tool %q is curated in two intent files", name))
			}
			seen[name] = true
		}
	}
	sort.Strings(problems)
	failWith(t, "intent sources", problems)
}

var readmeToolName = regexp.MustCompile("`([a-z0-9_]+)`")

// checkReadmeFamilies reports every way the README family table disagrees with
// the family list: a missing or extra row, a missing or extra member, a wrong
// canonical column, a reason that differs from toolFamilies, and an allowed
// shared intent the README does not explain.
func checkReadmeFamilies(readme string, tools []Entry, families []toolFamily, allowed map[string]string) []string {
	_, section, ok := strings.Cut(readme, readmeFamilyHead)
	if !ok {
		return []string{fmt.Sprintf("README has no %q section", readmeFamilyHead)}
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	var problems []string
	rows := map[string][]string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 6 {
			problems = append(problems, fmt.Sprintf("README family row has fewer than 4 columns: %q", line))
			continue
		}
		rows[strings.Trim(strings.TrimSpace(cols[1]), "`")] = cols[2:5]
	}
	canonicalOf := map[string]bool{}
	for _, e := range tools {
		canonicalOf[e.Name] = e.Canonical
	}
	known := map[string]bool{}
	for _, f := range families {
		known[f.Name] = true
		cols, ok := rows[f.Name]
		if !ok {
			problems = append(problems, fmt.Sprintf("README has no row for family %q", f.Name))
			continue
		}
		want := map[string]bool{}
		for _, m := range f.Members {
			want[m] = true
			if !strings.Contains(cols[0], "`"+m+"`") {
				problems = append(problems, fmt.Sprintf("README row %q lacks member %q", f.Name, m))
			}
			if canonicalOf[m] && strings.TrimSpace(cols[1]) != "`"+m+"`" {
				problems = append(problems, fmt.Sprintf("README row %q canonical column is %q, want `%s`", f.Name, strings.TrimSpace(cols[1]), m))
			}
		}
		for _, m := range readmeToolName.FindAllStringSubmatch(cols[0], -1) {
			if !want[m[1]] {
				problems = append(problems, fmt.Sprintf("README row %q lists member %q that toolFamilies does not", f.Name, m[1]))
			}
		}
		if got := strings.TrimSpace(cols[2]); got != f.Reason {
			problems = append(problems, fmt.Sprintf("README row %q reason differs from toolFamilies: %q", f.Name, got))
		}
	}
	for name := range rows {
		if !known[name] {
			problems = append(problems, fmt.Sprintf("README row %q has no family in toolFamilies", name))
		}
	}
	for phrase := range allowed {
		if !strings.Contains(readme, phrase) {
			problems = append(problems, fmt.Sprintf("README does not explain shared intent %q", phrase))
		}
	}
	sort.Strings(problems)
	return problems
}

// TestReadmeFamilyTableMatchesFamilies fails when the README family table and
// toolFamilies disagree on a family, its members (missing or extra), its
// canonical tool, or its reason, and when the README does not explain an
// allowed shared intent.
func TestReadmeFamilyTableMatchesFamilies(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	failWith(t, "README family table", checkReadmeFamilies(string(raw), annotatedMCPTools(t), toolFamilies(), sharedIntentAllowed))
}

// TestIntentCheckersRejectSeededViolations proves each checker fails on a
// planted defect and passes on a clean fixture, so a green real-data run is not
// vacuous.
func TestIntentCheckersRejectSeededViolations(t *testing.T) {
	t.Parallel()
	clean := []Entry{
		{Name: "a", Kind: KindMCPTool, Intents: []string{"one", "two", "three"}, Canonical: true},
		{Name: "b", Kind: KindMCPTool, Intents: []string{"four", "five", "six"}},
	}
	fam := []toolFamily{{Name: "f", Members: []string{"a", "b"}, Reason: "because"}}
	if p := checkIntentCoverage(clean); len(p) != 0 {
		t.Fatalf("clean coverage flagged: %v", p)
	}
	if p := checkFamilies(clean, fam); len(p) != 0 {
		t.Fatalf("clean families flagged: %v", p)
	}
	if p := checkSharedIntents(clean, fam, nil); len(p) != 0 {
		t.Fatalf("clean shared intents flagged: %v", p)
	}

	emptied := slices.Clone(clean)
	emptied[1].Intents = nil
	if len(checkIntentCoverage(emptied)) == 0 {
		t.Error("an emptied intent list was not flagged")
	}
	two := slices.Clone(clean)
	two[1].Canonical = true
	if len(checkFamilies(two, fam)) == 0 {
		t.Error("two canonical tools in one family were not flagged")
	}
	none := slices.Clone(clean)
	none[0].Canonical = false
	if len(checkFamilies(none, fam)) == 0 {
		t.Error("zero canonical tools in a family were not flagged")
	}
	stray := slices.Clone(clean)
	stray = append(stray, Entry{Name: "c", Kind: KindMCPTool, Intents: []string{"x", "y", "z"}, Canonical: true})
	if len(checkFamilies(stray, fam)) == 0 {
		t.Error("a canonical tool outside every family was not flagged")
	}
	shared := slices.Clone(clean)
	shared[1].Intents = []string{"one", "five", "six"}
	if len(checkSharedIntents(shared, fam, nil)) == 0 {
		t.Error("a phrase shared inside a family was not flagged")
	}
	if p := checkSharedIntents(shared, fam, map[string]string{"one": "reason"}); len(p) != 0 {
		t.Errorf("an allowed shared phrase was flagged: %v", p)
	}

	// checkFamilies branches not covered above.
	if len(checkFamilies(clean, []toolFamily{{Name: "f", Members: []string{"a", "ghost"}, Reason: "because"}})) == 0 {
		t.Error("an unknown family member was not flagged")
	}
	twoFam := []toolFamily{
		{Name: "f", Members: []string{"a", "b"}, Reason: "because"},
		{Name: "g", Members: []string{"b", "a"}, Reason: "because"},
	}
	if len(checkFamilies(clean, twoFam)) == 0 {
		t.Error("a tool in two families was not flagged")
	}
	if len(checkFamilies(clean, []toolFamily{{Name: "f", Members: []string{"a"}, Reason: "because"}})) == 0 {
		t.Error("a one-member family was not flagged")
	}
	if len(checkFamilies(clean, []toolFamily{{Name: "f", Members: []string{"a", "b"}, Reason: "  "}})) == 0 {
		t.Error("an empty family reason was not flagged")
	}

	// Phrase-format branches of checkIntentCoverage.
	for name, phrases := range map[string][]string{
		"edge whitespace": {"one ", "two", "three"},
		"upper case":      {"One", "two", "three"},
		"too many words":  {"a b c d e f g h i", "two", "three"},
		"duplicate":       {"one", "one", "three"},
		"empty phrase":    {"", "two", "three"},
		"too many":        {"1", "2", "3", "4", "5", "6", "7", "8", "9"},
	} {
		bad := []Entry{{Name: "a", Kind: KindMCPTool, Intents: phrases}}
		if len(checkIntentCoverage(bad)) == 0 {
			t.Errorf("malformed intents (%s) were not flagged", name)
		}
	}

	// checkReadmeFamilies on a synthetic README.
	good := readmeFamilyHead + "\n\n| Family | Members | Canonical | Why |\n|---|---|---|---|\n" +
		"| `f` | `a`, `b` | `a` | because |\n"
	if p := checkReadmeFamilies(good, clean, fam, nil); len(p) != 0 {
		t.Fatalf("clean README flagged: %v", p)
	}
	for name, readme := range map[string]string{
		"extra member":    strings.Replace(good, "`a`, `b`", "`a`, `b`, `c`", 1),
		"missing member":  strings.Replace(good, "`a`, `b`", "`a`", 1),
		"wrong canonical": strings.Replace(good, "| `a` | because", "| `b` | because", 1),
		"wrong reason":    strings.Replace(good, "because", "otherwise", 1),
		"missing row":     readmeFamilyHead + "\n",
		"extra row":       good + "| `g` | `a`, `b` | `a` | because |\n",
		"no section":      "nothing here",
	} {
		if len(checkReadmeFamilies(readme, clean, fam, nil)) == 0 {
			t.Errorf("README defect (%s) was not flagged", name)
		}
	}
	if len(checkReadmeFamilies(good, clean, fam, map[string]string{"unexplained phrase": "x"})) == 0 {
		t.Error("an unexplained allowed shared intent was not flagged")
	}
}
