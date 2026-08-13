// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// This file pins the claims README.md, doc.go, AGENTS.md, and the in-source
// comments make about this package, so a future edit to the code fails a test
// instead of silently making a sentence false. It follows the lockstep shape
// mcpsetup/doc_lockstep_test.go introduced for issue #5169 (F-8): the doc's
// copy of a fact and the code's actual behavior must independently agree with
// a pinned set here.
//
// It exists because four rounds of hand-swept doc corrections on this package
// each fixed the sentence that was reported and left the next one standing --
// including a commit that removed four instances of "the CLI serializes
// Output" and left a fifth in the file it was editing. Prose cannot be
// regression-tested by reading it again.
//
// Two conventions hold throughout. Failures name the offending field, import,
// or reason code rather than reporting a bare mismatch. And every assertion
// that loops proves it examined something, because a loop over an empty set
// passes while guarding nothing.
//
// The two filesystem-backed checks (imports, doc literals) are split into a
// pure scanner plus a caller, so the negative test can run the same scanner
// over a fixture directory and prove it reports what it should.

// docJSONFieldNames pins the wire name of every json-tagged field in the
// package, hardcoded rather than read back off the struct, so a renamed tag
// fails here instead of silently changing a wire contract.
//
// The first four types are the decision shape README.md says carries tags
// naming the assistant_fast_path_hook.v1 fields. Input is deliberately absent
// -- it is the untagged request struct, and the README says so.
//
// The three Claude types matter more than the rest: they are the only shapes
// anything actually serializes, so a renamed tag there breaks the Claude Code
// hook integration at runtime with nothing else to catch it.
func docJSONFieldNames() map[string]map[string]string {
	return map[string]map[string]string{
		"Output": {
			"Schema": "schema", "Decision": "decision", "Reason": "reason",
			"Message": "message", "Host": "host", "Trigger": "trigger",
			"Tool": "tool", "Scope": "scope", "PlannedCall": "planned_call",
			"Truth": "truth", "BudgetMS": "budget_ms", "ElapsedMS": "elapsed_ms",
		},
		"Scope": {"Kind": "kind", "ID": "id"},
		"PlannedCall": {
			"Tool": "tool", "Arguments": "arguments",
			"Limit": "limit", "TimeoutMS": "timeout_ms",
		},
		"Truth": {
			"Level": "level", "Profile": "profile",
			"FreshnessState": "freshness_state", "Truncated": "truncated",
		},
		"ClaudePreToolUseInput": {
			"HookEventName": "hook_event_name", "CWD": "cwd",
			"ToolName": "tool_name", "ToolInput": "tool_input",
		},
		"ClaudePreToolUseOutput": {"HookSpecificOutput": "hookSpecificOutput"},
		"ClaudePreToolUseSpecificOutput": {
			"HookEventName": "hookEventName", "AdditionalContext": "additionalContext",
		},
	}
}

// docTaggedStructs maps each pinned type name to a zero value to reflect over.
func docTaggedStructs() map[string]any {
	return map[string]any{
		"Output":                         Output{},
		"Scope":                          Scope{},
		"PlannedCall":                    PlannedCall{},
		"Truth":                          Truth{},
		"ClaudePreToolUseInput":          ClaudePreToolUseInput{},
		"ClaudePreToolUseOutput":         ClaudePreToolUseOutput{},
		"ClaudePreToolUseSpecificOutput": ClaudePreToolUseSpecificOutput{},
	}
}

// TestDocLockstepInputCarriesNoJSONTags pins README.md's claim that Input is a
// plain flag carrier that nothing decodes into. Adding a json tag to any Input
// field makes that sentence false, so it fails here.
func TestDocLockstepInputCarriesNoJSONTags(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(Input{})
	if typ.NumField() == 0 {
		t.Fatal("Input has no fields; the tag assertion below would be vacuous")
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if tag, ok := field.Tag.Lookup("json"); ok {
			t.Errorf("Input.%s carries json tag %q; README.md documents Input as the untagged request struct", field.Name, tag)
		}
	}
}

// TestDocLockstepJSONFieldNames pins the other half of the same README
// sentence, and pins it by wire name rather than by "a tag is present".
// Checking only for presence would miss the mutation an agent actually makes
// -- renaming a field and updating its tag to match -- which changes the wire
// contract while leaving every field tagged.
//
// It also fails on a field added or removed without a matching update here, so
// the pinned map cannot quietly fall behind the struct.
func TestDocLockstepJSONFieldNames(t *testing.T) {
	t.Parallel()

	pinned := docJSONFieldNames()
	structs := docTaggedStructs()
	if len(pinned) == 0 || len(structs) == 0 {
		t.Fatal("no structs to check; the assertion would be vacuous")
	}
	if len(pinned) != len(structs) {
		t.Fatalf("docJSONFieldNames covers %d types, docTaggedStructs has %d; keep them in step", len(pinned), len(structs))
	}

	checked := 0
	for name, value := range structs {
		want, ok := pinned[name]
		if !ok {
			t.Errorf("%s has no pinned json field names; add it to docJSONFieldNames", name)
			continue
		}
		typ := reflect.TypeOf(value)
		if typ.NumField() == 0 {
			t.Fatalf("%s has no fields; the tag assertion would be vacuous", name)
		}
		if typ.NumField() != len(want) {
			t.Errorf("%s has %d fields, docJSONFieldNames pins %d; a field was added or removed without updating the pinned set", name, typ.NumField(), len(want))
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag, tagged := field.Tag.Lookup("json")
			if !tagged || strings.TrimSpace(tag) == "" {
				t.Errorf("%s.%s has no json tag; README.md says every field of this struct names a wire field", name, field.Name)
				continue
			}
			gotName := strings.Split(tag, ",")[0]
			wantName, pinnedField := want[field.Name]
			if !pinnedField {
				t.Errorf("%s.%s is not in docJSONFieldNames; pin its wire name %q or drop the field", name, field.Name, gotName)
				continue
			}
			if gotName != wantName {
				t.Errorf("%s.%s serializes as %q, pinned as %q; renaming it changes the wire contract", name, field.Name, gotName, wantName)
			}
			checked++
		}
	}
	if checked < len(structs) {
		t.Fatalf("checked %d fields across %d structs, want at least one per struct", checked, len(structs))
	}
}

// docAllowedImports is the exact set README.md's "Dependencies" section names.
// It is the strong form of that section's package-count claim: every
// transitive dependency is standard library because every direct import is,
// and a count in prose rots on the next Go release while this set does not.
func docAllowedImports() map[string]bool {
	return map[string]bool{
		"fmt":           true,
		"io":            true,
		"path/filepath": true,
		"strings":       true,
		"time":          true,
	}
}

// importFinding is one import a non-test file declares that the doc does not
// allow.
type importFinding struct {
	File string
	Path string
}

// scanNonTestImports parses every non-test .go file in dir and reports which
// imports fall outside allowed, how many files it parsed, and the full set it
// saw. It returns findings instead of failing so the positive test (against
// the real package) and the negative test (against a fixture directory) share
// one implementation.
func scanNonTestImports(dir string, allowed map[string]bool) (files int, seen map[string]bool, findings []importFinding, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil, nil, err
	}
	seen = map[string]bool{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if parseErr != nil {
			return files, seen, findings, parseErr
		}
		files++
		for _, spec := range file.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return files, seen, findings, unquoteErr
			}
			seen[path] = true
			if !allowed[path] {
				findings = append(findings, importFinding{File: name, Path: path})
			}
		}
	}
	return files, seen, findings, nil
}

// TestDocLockstepNonTestImports pins README.md's "None internal" dependency
// claim and the packages it rules out by name: os/exec, net/http, and
// encoding/json. A new import in production code fails here rather than in a
// doc review.
func TestDocLockstepNonTestImports(t *testing.T) {
	t.Parallel()

	allowed := docAllowedImports()
	files, seen, findings, err := scanNonTestImports(".", allowed)
	if err != nil {
		t.Fatalf("scan package imports: %v", err)
	}
	if files == 0 {
		t.Fatal("scanned 0 non-test Go files; the import assertion would be vacuous")
	}
	if len(seen) == 0 {
		t.Fatalf("scanned %d non-test files but found 0 imports; the assertion would be vacuous", files)
	}
	for _, finding := range findings {
		t.Errorf("%s imports %q, which README.md's Dependencies section does not list; either drop the import or update the doc", finding.File, finding.Path)
	}
	for path := range allowed {
		if !seen[path] {
			t.Errorf("README.md's Dependencies section lists %q but no non-test file imports it; drop it from the doc", path)
		}
	}
}

// TestDocLockstepImportScannerReportsForbiddenImport is the negative half of
// the test above: it runs the same scanner over a fixture package that imports
// encoding/json and proves the scanner names it. Without this, a scanner that
// silently found nothing would look identical to a clean package.
func TestDocLockstepImportScannerReportsForbiddenImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixture := "package fixture\n\nimport (\n\t\"fmt\"\n\t_ \"encoding/json\"\n)\n\nvar _ = fmt.Sprint\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ignored := "package fixture\n\nimport _ \"net/http\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(ignored), 0o600); err != nil {
		t.Fatalf("write ignored fixture: %v", err)
	}

	files, _, findings, err := scanNonTestImports(dir, docAllowedImports())
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if files != 1 {
		t.Fatalf("scanned %d files, want 1 (the _test.go fixture must be skipped)", files)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one for encoding/json", findings)
	}
	if findings[0].Path != "encoding/json" || findings[0].File != "fixture.go" {
		t.Errorf("finding = %+v, want fixture.go importing encoding/json", findings[0])
	}
}

// docsMissingLiteral returns the named files in dir that do not contain
// literal. Like scanNonTestImports it reports rather than fails, so the
// negative test can drive the same code.
func docsMissingLiteral(dir string, names []string, literal string) ([]string, error) {
	var missing []string
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(data), literal) {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

// TestDocLockstepSchemaLiteral pins the assistant_fast_path_hook.v1 literal in
// two independent places: the value Evaluate stamps into Output.Schema, and
// the three package docs that quote it. Renaming the contract in one without
// the other fails here.
func TestDocLockstepSchemaLiteral(t *testing.T) {
	t.Parallel()

	const literal = "assistant_fast_path_hook.v1"

	out := Evaluate(Input{
		Host:     supportedHostClaude,
		Enabled:  true,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   DefaultBudget,
	})
	if out.Schema != literal {
		t.Errorf("advise Output.Schema = %q, want %q", out.Schema, literal)
	}
	skipped := Evaluate(Input{Host: "codex", Budget: DefaultBudget})
	if skipped.Schema != literal {
		t.Errorf("skip Output.Schema = %q, want %q", skipped.Schema, literal)
	}

	docs := []string{"README.md", "doc.go", "AGENTS.md"}
	missing, err := docsMissingLiteral(".", docs, literal)
	if err != nil {
		t.Fatalf("read package docs: %v", err)
	}
	for _, name := range missing {
		t.Errorf("%s does not mention the %s contract literal", name, literal)
	}
}

// TestDocLockstepDocScannerReportsMissingLiteral is the negative half of the
// doc-literal check: it proves the scanner reports a file that lacks the
// literal, and stays quiet for one that has it.
func TestDocLockstepDocScannerReportsMissingLiteral(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "has.md"), []byte("names assistant_fast_path_hook.v1 here\n"), 0o600); err != nil {
		t.Fatalf("write has.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lacks.md"), []byte("says nothing about the contract\n"), 0o600); err != nil {
		t.Fatalf("write lacks.md: %v", err)
	}

	missing, err := docsMissingLiteral(dir, []string{"has.md", "lacks.md"}, "assistant_fast_path_hook.v1")
	if err != nil {
		t.Fatalf("scan fixture docs: %v", err)
	}
	if len(missing) != 1 || missing[0] != "lacks.md" {
		t.Fatalf("missing = %v, want exactly [lacks.md]", missing)
	}
}
