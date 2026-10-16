// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration || queryplan_profile_live

package query

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// methodologyCommittedSchemaDeclaration is the graph schema change
// declaration the repository carries (#8073). The tree pin requires every
// existing go/internal/graph schema*.go file to match the base revision,
// because the paired Base/Candidate sides execute one test binary. A PR that
// changes existing schema files on purpose fills this value: Issue (the
// tracking issue), Reason (why the schema changes and why the paired sides
// may both measure candidate schema), and Paths (the exact repository-relative
// schema*.go files the PR modifies, for example
// "go/internal/graph/schema_application.go"). The rules match
// methodologyCommittedRewriteDeclaration. The declaration activates only when
// a declared file differs from base. While active, the differing set must
// equal the declared set exactly, each declared file must be a pinned schema
// file present at both revisions, and the working tree must match the
// committed candidate. A committed new schema file needs no declaration: the
// additive admission covers it. The reader rewrite declaration must be inert
// in the same run. Past the change's merge the declared files agree again,
// the declaration goes inert, and the pin behaves as before. Any other PR
// leaves the zero value alone.
var methodologyCommittedSchemaDeclaration = methodologyRewriteDeclaration{}

// methodologySchemaDeclarationActive reports whether any declared schema
// path differs between the base and candidate tree inventories.
func methodologySchemaDeclarationActive(schema methodologyRewriteDeclaration, baseEntries, candidateEntries map[string]string) bool {
	for _, path := range schema.Paths {
		before, baseExists := baseEntries[path]
		after, candidateExists := candidateEntries[path]
		if (baseExists || candidateExists) && (baseExists != candidateExists || before != after) {
			return true
		}
	}
	return false
}

// methodologyRewriteDeclarationActive reports whether any declared reader
// path differs from base, using the same per-file rule as
// methodologyVerifySameProductionWithDeclaration. A non-zero reader
// declaration whose files all agree with base is inert.
func methodologyRewriteDeclarationActive(t *testing.T, base, candidate string, reader methodologyRewriteDeclaration) bool {
	t.Helper()
	for _, path := range reader.Paths {
		baseEntry, err := exec.Command("git", "ls-tree", "--full-tree", base, "--", path).Output()
		if err != nil {
			t.Fatalf("read base reader entry %s: %v", path, err)
		}
		candidateEntry, err := exec.Command("git", "ls-tree", "--full-tree", candidate, "--", path).Output()
		if err != nil {
			t.Fatalf("read candidate reader entry %s: %v", path, err)
		}
		if len(baseEntry) == 0 || !bytes.Equal(baseEntry, candidateEntry) {
			return true
		}
	}
	return false
}

// methodologySchemaDeclarationReader is the scratch reader file the gate
// scenarios use to drive the reader rewrite declaration's activity.
const methodologySchemaDeclarationReader = "go/internal/query/codemodel/reader.go"

// methodologySchemaDeclarationCase is one seeded scratch-repository state for
// TestMethodologySchemaDeclarationGate. A file name without a slash resolves
// inside the graph schema tree.
type methodologySchemaDeclarationCase struct {
	name         string
	modify       []string
	add          string
	remove       string
	dirtyModify  string
	modifyReader bool
	declare      []string
	declareRead  bool
	wantFailure  bool
	wantText     string
}

// TestMethodologySchemaDeclarationGate is the RED/GREEN contract for the
// committed graph schema change declaration (#8073). A PR may declare the
// existing go/internal/graph schema*.go files it changes on purpose. The
// tree pin then requires the base-vs-candidate diff to equal the declared
// set exactly. Every wantFailure arm proves the pin still fails closed.
func TestMethodologySchemaDeclarationGate(t *testing.T) {
	const childMarker = "ESHU_METHODOLOGY_SCHEMA_DECLARATION_CHILD"
	if spec := os.Getenv(childMarker); spec != "" {
		runMethodologySchemaDeclarationChild(t, spec)
		return
	}
	for _, testCase := range []methodologySchemaDeclarationCase{
		{name: "unchanged with zero declaration"},
		{name: "undeclared edit with zero declaration", modify: []string{"schema_unchanged.go"}, wantFailure: true, wantText: "schema_unchanged.go"},
		{name: "declared modify", modify: []string{"schema_unchanged.go"}, declare: []string{"schema_unchanged.go"}},
		{name: "declared modify with committed addition", modify: []string{"schema_unchanged.go"}, add: "schema_candidate_only.go", declare: []string{"schema_unchanged.go"}},
		{name: "undeclared modify beside declared modify", modify: []string{"schema_unchanged.go", "schema_base_only.go"}, declare: []string{"schema_unchanged.go"}, wantFailure: true, wantText: "schema_base_only.go"},
		{name: "declared but unchanged in an active run", modify: []string{"schema_unchanged.go"}, declare: []string{"schema_unchanged.go", "schema_base_only.go"}, wantFailure: true, wantText: "schema_base_only.go"},
		{name: "fully inert declaration passes", declare: []string{"schema_unchanged.go"}},
		{name: "inert declaration does not excuse another edit", modify: []string{"schema_base_only.go"}, declare: []string{"schema_unchanged.go"}, wantFailure: true, wantText: "schema_base_only.go"},
		{name: "declared modify with dirty worktree", modify: []string{"schema_unchanged.go"}, dirtyModify: "schema_unchanged.go", declare: []string{"schema_unchanged.go"}, wantFailure: true, wantText: "schema_unchanged.go"},
		{name: "declared modify with base only deletion", modify: []string{"schema_unchanged.go"}, remove: "schema_base_only.go", declare: []string{"schema_unchanged.go"}, wantFailure: true, wantText: "schema_base_only.go"},
		{name: "declared path outside pinned schema set", modify: []string{"schema_unchanged.go"}, declare: []string{"schema_unchanged.go", "go/internal/graph/builder.go"}, wantFailure: true, wantText: "builder.go"},
		{name: "declared added file is not a pinned base source", add: "schema_candidate_only.go", declare: []string{"schema_candidate_only.go"}, wantFailure: true, wantText: "schema_candidate_only.go"},
		{name: "schema and reader declarations both active", modify: []string{"schema_unchanged.go"}, modifyReader: true, declare: []string{"schema_unchanged.go"}, declareRead: true, wantFailure: true, wantText: "reader rewrite declaration"},
		{name: "inert reader declaration beside active schema declaration", modify: []string{"schema_unchanged.go"}, declare: []string{"schema_unchanged.go"}, declareRead: true},
		{name: "active reader declaration beside inert schema declaration", modifyReader: true, declare: []string{"schema_unchanged.go"}, declareRead: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkMethodologySchemaDeclarationScenario(t, childMarker, testCase)
		})
	}
}

// runMethodologySchemaDeclarationChild runs one scenario inside the
// re-executed test binary, where CWD is the scratch repository. The spec
// carries base, candidate, declared schema paths, and declared reader paths.
func runMethodologySchemaDeclarationChild(t *testing.T, spec string) {
	t.Helper()
	parts := strings.Split(spec, "\x1f")
	if len(parts) != 4 {
		t.Fatalf("malformed schema declaration spec: %q", spec)
	}
	split := func(value string) []string {
		if value == "" {
			return nil
		}
		return strings.Split(value, "\x1e")
	}
	schema := methodologyRewriteDeclaration{Issue: 8073, Reason: "scenario schema change", Paths: split(parts[2])}
	reader := methodologyRewriteDeclaration{Issue: 7347, Reason: "scenario reader rewrite", Paths: split(parts[3])}
	methodologyVerifySameProductionTreeWithDeclaration(t, parts[0], parts[1], "go/internal/graph", "schema*.go", schema, reader)
}

// checkMethodologySchemaDeclarationScenario seeds the graph schema guard tree
// plus one reader file in a scratch repository, commits the candidate change,
// applies any dirty edit, then re-executes the gate child from the package
// directory and the root, with and without an inherited GIT_WORK_TREE.
func checkMethodologySchemaDeclarationScenario(t *testing.T, childMarker string, testCase methodologySchemaDeclarationCase) {
	t.Helper()
	scenario := methodologyGraphSchemaGuardScenario()
	root := t.TempDir()
	packageDir := filepath.Join(root, "go", "internal", "query")
	treeDir := filepath.Join(append([]string{root}, strings.Split(scenario.directory, "/")...)...)
	readerPath := filepath.Join(root, filepath.FromSlash(methodologySchemaDeclarationReader))
	for _, directory := range []string{packageDir, treeDir, filepath.Dir(readerPath)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range scenario.seed {
		write(filepath.Join(treeDir, name), "guard seed\n")
	}
	write(readerPath, "package codemodel\n")
	runGit := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		command.Env = methodologyScratchGitEnv("")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init", "--quiet")
	runGit("config", "--local", "maintenance.auto", "false")
	runGit("config", "--local", "gc.auto", "0")
	runGit("config", "user.name", "Eshu Test")
	runGit("config", "user.email", "eshu-test@example.invalid")
	runGit("add", "-A")
	runGit("commit", "--quiet", "-m", "seed schema declaration identity")
	base := runGit("rev-parse", "HEAD")
	for _, name := range testCase.modify {
		write(filepath.Join(treeDir, name), "guard declared modification\n")
	}
	if testCase.add != "" {
		write(filepath.Join(treeDir, testCase.add), "guard addition\n")
	}
	if testCase.remove != "" {
		if err := os.Remove(filepath.Join(treeDir, testCase.remove)); err != nil {
			t.Fatal(err)
		}
	}
	if testCase.modifyReader {
		write(readerPath, "package codemodel\n// rewritten\n")
	}
	runGit("add", "-A")
	runGit("commit", "--quiet", "--allow-empty", "-m", "candidate schema change")
	candidate := runGit("rev-parse", "HEAD")
	if testCase.dirtyModify != "" {
		write(filepath.Join(treeDir, testCase.dirtyModify), "guard uncommitted edit\n")
	}

	declared := make([]string, 0, len(testCase.declare))
	for _, name := range testCase.declare {
		if strings.Contains(name, "/") {
			declared = append(declared, name)
		} else {
			declared = append(declared, scenario.directory+"/"+name)
		}
	}
	readers := ""
	if testCase.declareRead {
		readers = methodologySchemaDeclarationReader
	}
	spec := strings.Join([]string{base, candidate, strings.Join(declared, "\x1e"), readers}, "\x1f")

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, workingDirectory := range []string{packageDir, root} {
		for _, inheritedWorkTree := range []string{"", "."} {
			command := exec.Command(binary, "-test.run=^TestMethodologySchemaDeclarationGate$", "-test.v")
			command.Dir = workingDirectory
			command.Env = append(methodologyScratchGitEnv(""), childMarker+"="+spec)
			if inheritedWorkTree != "" {
				command.Env = append(command.Env, "GIT_WORK_TREE="+inheritedWorkTree)
			}
			output, err := command.CombinedOutput()
			if !testCase.wantFailure {
				if err != nil {
					t.Fatalf("schema declaration scenario failed from %s (GIT_WORK_TREE=%q): %v:\n%s", workingDirectory, inheritedWorkTree, err, output)
				}
				continue
			}
			if err == nil {
				t.Fatalf("%s passed schema declaration gate from %s (GIT_WORK_TREE=%q):\n%s", testCase.wantText, workingDirectory, inheritedWorkTree, output)
			}
			if !strings.Contains(string(output), testCase.wantText) {
				t.Fatalf("schema declaration gate failed for another reason from %s (GIT_WORK_TREE=%q): %v:\n%s", workingDirectory, inheritedWorkTree, err, output)
			}
		}
	}
}
