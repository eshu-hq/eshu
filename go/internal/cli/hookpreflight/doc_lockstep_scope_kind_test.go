// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Membership half of the scope-kind pin (see doc_lockstep_test.go for the
// conventions the whole guard follows).
//
// TestDocLockstepPlannedCallToolPerScopeKind drives one request per scope kind
// and checks the tool each one gets, so removing a kind fails it. Adding a
// seventh did not: the case list in that test is hand-written, so a new
// candidate in scopeFromInput that nobody thought to probe was simply absent
// from it, and a kind with no plannedCallForScope case is answered by the
// default get_code_relationship_story -- which AGENTS.md itself calls silently
// wrong. Deleting an existing kind's case had the same shape from the other
// side.
//
// This is the same fix scanTaggedStructs made for the json-tagged struct set:
// read the kinds out of the source on both sides, so the comparison is an
// inventory rather than two hand-written lists agreeing with each other.
// scopeFromInput's candidate list is one side, plannedCallForScope's switch is
// the other, and they have to name the same kinds.
//
// What it does not do: it says nothing about which tool a kind is pointed at.
// A case that names get_service_story for `resource` satisfies every rule here
// and is caught, if at all, by TestDocLockstepPlannedCallToolPerScopeKind's
// hand-written expectations. Membership is the property; correctness of the
// mapping stays where it was.
//
// Both scanners read syntax, so both are rigid about the shape they read: a
// candidate whose Kind is a constant instead of a string literal, or a tool
// picked by a table lookup instead of a switch, is reported rather than
// resolved. That is the same bargain doc_lockstep_switch_test.go makes with
// triggerAllowed -- a call the scanner cannot see through is how a rule gets
// satisfied without being kept -- and the cost is teaching the scanner the day
// someone genuinely needs one of those shapes.

// scanScopeCandidateKinds reports the Kind of every candidate scopeFromInput
// offers in dir's non-test files, sorted, plus how many candidates it read.
// Membership is what this file compares, so the sort is deliberate -- the
// candidate ORDER is a separate claim, held by
// TestDocLockstepScopeResolutionIsFirstMatch through Evaluate.
func scanScopeCandidateKinds(dir string) (candidates int, kinds []string, findings []switchFinding, err error) {
	target, err := findFuncDecl(dir, "scopeFromInput")
	if err != nil {
		return 0, nil, nil, err
	}
	report := func(format string, args ...any) {
		findings = append(findings, switchFinding{Func: "scopeFromInput", Detail: fmt.Sprintf(format, args...)})
	}

	lists := 0
	ast.Inspect(target.Body, func(node ast.Node) bool {
		composite, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		arrayType, ok := composite.Type.(*ast.ArrayType)
		if !ok {
			return true
		}
		if elem, ok := arrayType.Elt.(*ast.Ident); !ok || elem.Name != "Scope" {
			return true
		}
		lists++
		for i, element := range composite.Elts {
			candidates++
			kind, ok := compositeStringField(element, "Kind")
			if !ok {
				report("candidate %d names no Kind the scanner can read; a candidate whose kind is computed cannot be compared against plannedCallForScope's cases", i)
				continue
			}
			kinds = append(kinds, kind)
		}
		return true
	})
	if lists != 1 {
		report("holds %d []Scope candidate lists, want exactly one; with two the scanner cannot tell which one resolves a scope", lists)
	}
	sort.Strings(kinds)
	return candidates, kinds, findings, nil
}

// compositeStringField reports the string literal a composite literal assigns
// to field. It reads keyed elements only: a positional `{"repo_path", id}`
// names no field for the scanner to key on, and is reported by the caller
// rather than guessed at.
func compositeStringField(expr ast.Expr, field string) (string, bool) {
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	for _, element := range composite.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := keyValue.Key.(*ast.Ident); !ok || key.Name != field {
			continue
		}
		literal, ok := keyValue.Value.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return "", false
		}
		return value, true
	}
	return "", false
}

// scanPlannedCallKindCases reports the scope kinds plannedCallForScope's switch
// names in a case clause of its own, sorted, plus how many such clauses it
// read. A clause that names no tool is a finding rather than a kind: falling
// through to whatever the default left in `tool` is exactly the silent answer
// this file exists to stop, and it reads the same in a diff as a real choice.
func scanPlannedCallKindCases(dir string) (clauses int, kinds []string, findings []switchFinding, err error) {
	target, err := findFuncDecl(dir, "plannedCallForScope")
	if err != nil {
		return 0, nil, nil, err
	}
	report := func(format string, args ...any) {
		findings = append(findings, switchFinding{Func: "plannedCallForScope", Detail: fmt.Sprintf(format, args...)})
	}

	switches := 0
	defaults := 0
	ast.Inspect(target.Body, func(node ast.Node) bool {
		switchStmt, ok := node.(*ast.SwitchStmt)
		if !ok || switchStmt.Body == nil {
			return true
		}
		selector, ok := switchStmt.Tag.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Kind" {
			return true
		}
		switches++
		for _, stmt := range switchStmt.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				report("the switch holds a %T, want only case clauses", stmt)
				continue
			}
			literals, literalsOK := caseStringLiterals(clause)
			named := assignsValue(clause.Body, "tool")
			switch {
			case clause.List == nil:
				defaults++
				if !named {
					report("the default clause names no fallback tool, so a Scope no candidate produced gets whatever `tool` already held")
				}
			case !literalsOK:
				report("a case compares something other than a string literal, so the kinds it answers for cannot be read")
			case !named:
				report("case %v names no tool of its own, so those kinds are answered by the default rather than by a choice anyone made", literals)
			default:
				clauses++
				kinds = append(kinds, literals...)
			}
		}
		return true
	})
	if switches != 1 {
		report("holds %d switches on a .Kind field, want exactly one; the scanner cannot tell which one picks the tool", switches)
	}
	if switches == 1 && defaults != 1 {
		report("the kind switch has %d default clauses, want exactly one naming the fallback tool", defaults)
	}
	sort.Strings(kinds)
	return clauses, kinds, findings, nil
}

// caseStringLiterals reports the string literals a case clause compares, and
// false when any of them is something else.
func caseStringLiterals(clause *ast.CaseClause) ([]string, bool) {
	values := make([]string, 0, len(clause.List))
	for _, expr := range clause.List {
		literal, ok := expr.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return values, false
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return values, false
		}
		values = append(values, value)
	}
	return values, true
}

// assignsValue reports whether body assigns anything to the local named name.
// A string literal and a named constant both count: what matters is that the
// clause makes the choice, not how the tool name is spelled.
func assignsValue(body []ast.Stmt, name string) bool {
	for _, stmt := range body {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for _, lhs := range assign.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok && ident.Name == name {
				return true
			}
		}
	}
	return false
}

// TestDocLockstepEveryScopeKindNamesItsPlannedCall is the membership gate
// AGENTS.md's "a scope kind and its plannedCallForScope case must change
// together" rule had only in prose. Both sides are read out of the source, so
// a seventh candidate with no case fails here, and so does deleting the case
// for a kind that still has a candidate.
func TestDocLockstepEveryScopeKindNamesItsPlannedCall(t *testing.T) {
	t.Parallel()

	// The behavioural control: a resolved scope really does carry a planned
	// call, so the source comparison below is pinning a live path.
	out := Evaluate(advisableInput())
	if out.Decision != DecisionAdvise || out.PlannedCall == nil {
		t.Fatalf("control request decided %q with PlannedCall %+v, want an advise carrying one; the assertions below would pin a dead path", out.Decision, out.PlannedCall)
	}

	candidates, candidateKinds, candidateFindings, err := scanScopeCandidateKinds(".")
	if err != nil {
		t.Fatalf("scan scope candidates: %v", err)
	}
	clauses, caseKinds, caseFindings, err := scanPlannedCallKindCases(".")
	if err != nil {
		t.Fatalf("scan planned-call cases: %v", err)
	}
	// The findings come first: a scanner that read neither list fails the
	// vacuity check below, and the finding is what says which shape it could
	// not read.
	for _, finding := range append(candidateFindings, caseFindings...) {
		t.Errorf("%s", finding)
	}
	if candidates == 0 || clauses == 0 {
		t.Fatalf("read %d scope candidates and %d tool-naming case clauses; the comparison would be vacuous", candidates, clauses)
	}

	missing, extra := setDifference(candidateKinds, caseKinds), setDifference(caseKinds, candidateKinds)
	for _, kind := range missing {
		t.Errorf("scopeFromInput offers scope kind %q and plannedCallForScope names no case for it, so it is answered by the default tool rather than by a choice anyone made; add its case or drop the candidate", kind)
	}
	for _, kind := range extra {
		t.Errorf("plannedCallForScope has a case for scope kind %q that scopeFromInput can no longer produce; drop the case or restore the candidate", kind)
	}
}

// setDifference reports the members of left that are absent from right.
func setDifference(left, right []string) []string {
	present := make(map[string]bool, len(right))
	for _, value := range right {
		present[value] = true
	}
	var only []string
	for _, value := range left {
		if !present[value] {
			only = append(only, value)
		}
	}
	return only
}

// writeScopeKindFixture writes body as the sole non-test file of a fresh
// directory, next to a _test.go file carrying a decoy candidate list the
// scanners must not read.
func writeScopeKindFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ignored := "package fixture\n\nfunc scopeFromInput(input Input) (Scope, bool) {\n" +
		"\tcandidates := []Scope{{Kind: \"from_a_test_file\", ID: input.Field}}\n\treturn candidates[0], true\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(ignored), 0o600); err != nil {
		t.Fatalf("write ignored fixture: %v", err)
	}
	return dir
}

// scopeKindFixture renders a throwaway package carrying a scopeFromInput and a
// plannedCallForScope of the given shape. An empty tool renders a clause with
// no assignment in it, which is the fall-through-to-the-default shape.
func scopeKindFixture(candidates []string, cases [][]string, tools []string) string {
	var body strings.Builder
	body.WriteString("package fixture\n\ntype Scope struct {\n\tKind string\n\tID   string\n}\n\n")
	body.WriteString("func scopeFromInput(input Input) (Scope, bool) {\n\tcandidates := []Scope{\n")
	for _, kind := range candidates {
		fmt.Fprintf(&body, "\t\t{Kind: %q, ID: input.Field},\n", kind)
	}
	body.WriteString("\t}\n\treturn candidates[0], true\n}\n\n")
	body.WriteString("func plannedCallForScope(scope Scope) string {\n\tvar tool string\n\tswitch scope.Kind {\n")
	for i, group := range cases {
		quoted := make([]string, 0, len(group))
		for _, kind := range group {
			quoted = append(quoted, strconv.Quote(kind))
		}
		fmt.Fprintf(&body, "\tcase %s:\n", strings.Join(quoted, ", "))
		if tools[i] != "" {
			fmt.Fprintf(&body, "\t\ttool = %q\n", tools[i])
		}
	}
	body.WriteString("\tdefault:\n\t\ttool = \"fallback\"\n\t}\n\treturn tool\n}\n")
	return body.String()
}

// TestDocLockstepScopeKindScannersReportMembershipDrift is the negative half,
// driven over fixture directories the way every other scanner in this package
// is. It is what says the comparison above can go red at all -- and, just as
// importantly, that it stays quiet for a reordering, which is an ordinary edit
// and not a membership change.
func TestDocLockstepScopeKindScannersReportMembershipDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		candidates []string
		cases      [][]string
		tools      []string
		wantKinds  string
		wantCases  string
		wantFinds  int
	}{
		{
			name:       "every_kind_has_a_case",
			candidates: []string{"repo_path", "service", "resource"},
			cases:      [][]string{{"repo_path"}, {"service"}, {"resource"}},
			tools:      []string{"story", "service_story", "resource"},
			wantKinds:  "repo_path,resource,service", wantCases: "repo_path,resource,service",
		},
		{
			name:       "reordered_cases_are_the_same_membership",
			candidates: []string{"repo_path", "service", "resource"},
			cases:      [][]string{{"resource"}, {"repo_path"}, {"service"}},
			tools:      []string{"resource", "story", "service_story"},
			wantKinds:  "repo_path,resource,service", wantCases: "repo_path,resource,service",
		},
		{
			name:       "grouped_cases_are_the_same_membership",
			candidates: []string{"repo_path", "service", "resource"},
			cases:      [][]string{{"repo_path", "service"}, {"resource"}},
			tools:      []string{"story", "resource"},
			wantKinds:  "repo_path,resource,service", wantCases: "repo_path,resource,service",
		},
		{
			name:       "seventh_kind_with_no_case",
			candidates: []string{"repo_path", "service", "resource", "cluster"},
			cases:      [][]string{{"repo_path"}, {"service"}, {"resource"}},
			tools:      []string{"story", "service_story", "resource"},
			wantKinds:  "cluster,repo_path,resource,service", wantCases: "repo_path,resource,service",
		},
		{
			name:       "case_for_a_kind_no_candidate_offers",
			candidates: []string{"repo_path", "service"},
			cases:      [][]string{{"repo_path"}, {"service"}, {"resource"}},
			tools:      []string{"story", "service_story", "resource"},
			wantKinds:  "repo_path,service", wantCases: "repo_path,resource,service",
		},
		{
			name:       "case_names_no_tool_of_its_own",
			candidates: []string{"repo_path", "service", "resource"},
			cases:      [][]string{{"repo_path"}, {"service"}, {"resource"}},
			tools:      []string{"story", "service_story", ""},
			wantKinds:  "repo_path,resource,service", wantCases: "repo_path,service", wantFinds: 1,
		},
	}
	if len(cases) != 6 {
		t.Fatalf("fixture cases = %d, want one per drift shape plus the three that must stay quiet", len(cases))
	}

	drifted := 0
	for _, tc := range cases {
		dir := writeScopeKindFixture(t, scopeKindFixture(tc.candidates, tc.cases, tc.tools))
		_, kinds, kindFindings, err := scanScopeCandidateKinds(dir)
		if err != nil {
			t.Fatalf("%s: scan candidates: %v", tc.name, err)
		}
		_, caseKinds, caseFindings, err := scanPlannedCallKindCases(dir)
		if err != nil {
			t.Fatalf("%s: scan cases: %v", tc.name, err)
		}
		if got := strings.Join(kinds, ","); got != tc.wantKinds {
			t.Errorf("%s: candidate kinds = %q, want %q", tc.name, got, tc.wantKinds)
		}
		if got := strings.Join(caseKinds, ","); got != tc.wantCases {
			t.Errorf("%s: case kinds = %q, want %q", tc.name, got, tc.wantCases)
		}
		if got := len(kindFindings) + len(caseFindings); got != tc.wantFinds {
			t.Errorf("%s: findings = %d (%v %v), want %d", tc.name, got, kindFindings, caseFindings, tc.wantFinds)
		}
		if tc.wantKinds != tc.wantCases {
			drifted++
		}
	}
	if drifted != 3 {
		t.Fatalf("%d of %d fixtures drifted, want 3; without both a kind-without-a-case and a case-without-a-kind the comparison is only proved in one direction", drifted, len(cases))
	}
}
