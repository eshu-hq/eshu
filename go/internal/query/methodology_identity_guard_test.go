// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration || queryplan_profile_live

package query

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func checkMethodologyScratchGitDoesNotStartMaintenance(t *testing.T) {
	profileDir := t.TempDir()
	profile := filepath.Join(profileDir, "forced-maintenance.gitconfig")
	config := "[maintenance]\n\tauto = true\n\tautoDetach = true\n[maintenance \"commit-graph\"]\n\tenabled = true\n\tauto = -1\n[gc]\n\tauto = -1\n"
	if err := os.WriteFile(profile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(profileDir, "git-trace.json")
	t.Setenv("GIT_CONFIG_GLOBAL", profile)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	checkMethodologyProductionTreeScenario(t, "ESHU_METHODOLOGY_IDENTITY_GUARD_CHILD", "", "", "", false, false, false, false, false, "", trace)

	file, err := os.Open(trace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	sawCommit := false
	for scanner.Scan() {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "start" && len(event.Argv) >= 2 && filepath.Base(event.Argv[0]) == "git" && event.Argv[1] == "commit" {
			sawCommit = true
		}
		if event.Event == "child_start" && len(event.Argv) >= 2 && filepath.Base(event.Argv[0]) == "git" && event.Argv[1] == "maintenance" {
			t.Fatalf("scratch Git started automatic maintenance: %s", scanner.Bytes())
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !sawCommit {
		t.Fatal("scratch Git trace did not record the fixture commit")
	}
}

func TestMethodologyProductionTreeRejectsBaseOnlyMigration(t *testing.T) {
	const childMarker = "ESHU_METHODOLOGY_IDENTITY_GUARD_CHILD"
	if base := os.Getenv(childMarker); base != "" {
		methodologyVerifySameProductionTree(t, base, os.Getenv("ESHU_METHODOLOGY_IDENTITY_GUARD_CANDIDATE"), "go/internal/storage/postgres/migrations", "*.sql")
		return
	}
	t.Run("scratch_maintenance_isolation", checkMethodologyScratchGitDoesNotStartMaintenance)

	for _, testCase := range []struct {
		name          string
		remove        string
		add           string
		modify        string
		empty         bool
		commitDeleted bool
		commitAdd     bool
		dirtyAdd      bool
		wantFailure   bool
		wantPath      string
	}{
		{name: "base only", remove: "002_base_only.sql", wantFailure: true, wantPath: "002_base_only.sql"},
		{name: "candidate only addition admitted", add: "003_candidate_only.sql", commitAdd: true},
		{name: "untracked addition still fails", add: "003_candidate_only.sql", wantFailure: true, wantPath: "003_candidate_only.sql"},
		{name: "committed addition with dirty worktree", add: "003_candidate_only.sql", commitAdd: true, dirtyAdd: true, wantFailure: true, wantPath: "003_candidate_only.sql"},
		{name: "committed addition with modified base file", add: "003_candidate_only.sql", commitAdd: true, modify: "001_unchanged.sql", wantFailure: true, wantPath: "001_unchanged.sql"},
		{name: "committed addition with deleted base file", add: "003_candidate_only.sql", commitAdd: true, remove: "002_base_only.sql", wantFailure: true, wantPath: "002_base_only.sql"},
		{name: "modified", modify: "001_unchanged.sql", wantFailure: true, wantPath: "001_unchanged.sql"},
		{name: "committed deletion restored untracked", commitDeleted: true, wantFailure: true, wantPath: "002_base_only.sql"},
		{name: "empty inventory", empty: true, wantFailure: true, wantPath: "no production sources"},
		{name: "unchanged"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkMethodologyProductionTreeScenario(t, childMarker, testCase.remove, testCase.add, testCase.modify, testCase.empty, testCase.commitDeleted, testCase.commitAdd, testCase.dirtyAdd, testCase.wantFailure, testCase.wantPath, "")
		})
	}
}

// TestMethodologyProductionTreeAdmitsAdditiveGraphSchema pins the #8073
// scope decision: additive graph schema files are admitted exactly like
// additive migrations. A committed new schema file passes while every base
// file stays byte-identical; removals, modifications, untracked additions,
// and dirty worktrees still fail.
func TestMethodologyProductionTreeAdmitsAdditiveGraphSchema(t *testing.T) {
	const childMarker = "ESHU_METHODOLOGY_IDENTITY_GUARD_GRAPH_CHILD"
	const candidateMarker = "ESHU_METHODOLOGY_IDENTITY_GUARD_GRAPH_CANDIDATE"
	if base := os.Getenv(childMarker); base != "" {
		methodologyVerifySameProductionTree(t, base, os.Getenv(candidateMarker), "go/internal/graph", "schema*.go")
		return
	}

	for _, testCase := range []struct {
		name          string
		remove        string
		add           string
		modify        string
		empty         bool
		commitDeleted bool
		commitAdd     bool
		dirtyAdd      bool
		wantFailure   bool
		wantPath      string
	}{
		{name: "base only", remove: "schema_base_only.go", wantFailure: true, wantPath: "schema_base_only.go"},
		{name: "candidate only addition admitted", add: "schema_candidate_only.go", commitAdd: true},
		{name: "untracked addition still fails", add: "schema_candidate_only.go", wantFailure: true, wantPath: "schema_candidate_only.go"},
		{name: "committed addition with dirty worktree", add: "schema_candidate_only.go", commitAdd: true, dirtyAdd: true, wantFailure: true, wantPath: "schema_candidate_only.go"},
		{name: "committed addition with modified base file", add: "schema_candidate_only.go", commitAdd: true, modify: "schema_unchanged.go", wantFailure: true, wantPath: "schema_unchanged.go"},
		{name: "committed addition with deleted base file", add: "schema_candidate_only.go", commitAdd: true, remove: "schema_base_only.go", wantFailure: true, wantPath: "schema_base_only.go"},
		{name: "modified", modify: "schema_unchanged.go", wantFailure: true, wantPath: "schema_unchanged.go"},
		{name: "committed deletion restored untracked", commitDeleted: true, wantFailure: true, wantPath: "schema_base_only.go"},
		{name: "empty inventory", empty: true, wantFailure: true, wantPath: "no production sources"},
		{name: "unchanged"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkMethodologyProductionTreeScenarioFor(t, methodologyGraphSchemaGuardScenario(), childMarker, testCase.remove, testCase.add, testCase.modify, testCase.empty, testCase.commitDeleted, testCase.commitAdd, testCase.dirtyAdd, testCase.wantFailure, testCase.wantPath, "")
		})
	}
}

func methodologyScratchGitEnv(trace string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_TRACE2_EVENT=") {
			env = append(env, entry)
		}
	}
	if trace == "" {
		trace = "0"
	}
	return append(env, "GIT_TRACE2_EVENT="+trace)
}

// methodologyGuardTreeScenario describes one production tree pinned by a
// methodology identity guard: the tree directory, its scratch seed files,
// the child test entry that re-executes the verification, and the marker
// that carries the candidate revision to the child.
type methodologyGuardTreeScenario struct {
	directory    string
	seed         []string
	childTest    string
	candidateEnv string
}

// methodologyMigrationsGuardScenario pins the Postgres migrations tree,
// whose additive evolution the migration gates own.
func methodologyMigrationsGuardScenario() methodologyGuardTreeScenario {
	return methodologyGuardTreeScenario{
		directory:    "go/internal/storage/postgres/migrations",
		seed:         []string{"001_unchanged.sql", "002_base_only.sql"},
		childTest:    "^TestMethodologyProductionTreeRejectsBaseOnlyMigration$",
		candidateEnv: "ESHU_METHODOLOGY_IDENTITY_GUARD_CANDIDATE",
	}
}

// methodologyGraphSchemaGuardScenario pins the graph schema tree
// (go/internal/graph schema*.go). Additive graph schema files are admitted
// exactly like additive migrations: every base file stays byte-identical
// and the addition arrives as a committed new file (#8073).
func methodologyGraphSchemaGuardScenario() methodologyGuardTreeScenario {
	return methodologyGuardTreeScenario{
		directory:    "go/internal/graph",
		seed:         []string{"schema_unchanged.go", "schema_base_only.go"},
		childTest:    "^TestMethodologyProductionTreeAdmitsAdditiveGraphSchema$",
		candidateEnv: "ESHU_METHODOLOGY_IDENTITY_GUARD_GRAPH_CANDIDATE",
	}
}

func checkMethodologyProductionTreeScenario(t *testing.T, childMarker, remove, add, modify string, empty, commitDeleted, commitAdd, dirtyAdd, wantFailure bool, wantPath, trace string) {
	t.Helper()
	checkMethodologyProductionTreeScenarioFor(t, methodologyMigrationsGuardScenario(), childMarker, remove, add, modify, empty, commitDeleted, commitAdd, dirtyAdd, wantFailure, wantPath, trace)
}

func checkMethodologyProductionTreeScenarioFor(t *testing.T, scenario methodologyGuardTreeScenario, childMarker, remove, add, modify string, empty, commitDeleted, commitAdd, dirtyAdd, wantFailure bool, wantPath, trace string) {
	t.Helper()
	root := t.TempDir()
	packageDir := filepath.Join(root, "go", "internal", "query")
	treeDir := filepath.Join(append([]string{root}, strings.Split(scenario.directory, "/")...)...)
	for _, directory := range []string{packageDir, treeDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seed := scenario.seed
	if empty {
		seed = []string{"README"}
	}
	for _, name := range seed {
		if err := os.WriteFile(filepath.Join(treeDir, name), []byte("guard seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		command.Env = methodologyScratchGitEnv(trace)
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
	runGit("add", scenario.directory)
	runGit("commit", "--quiet", "-m", "seed guard identity")
	base := runGit("rev-parse", "HEAD")
	if remove != "" {
		if err := os.Remove(filepath.Join(treeDir, remove)); err != nil {
			t.Fatal(err)
		}
	}
	if add != "" {
		if err := os.WriteFile(filepath.Join(treeDir, add), []byte("guard addition\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if modify != "" {
		if err := os.WriteFile(filepath.Join(treeDir, modify), []byte("guard modification\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if commitDeleted {
		path := filepath.Join(treeDir, scenario.seed[len(scenario.seed)-1])
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		runGit("add", "-A")
		runGit("commit", "--quiet", "-m", "remove candidate file")
		if err := os.WriteFile(path, []byte("guard seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if commitAdd {
		runGit("add", "-A")
		runGit("commit", "--quiet", "-m", "add candidate file")
	}
	if dirtyAdd {
		if add == "" {
			t.Fatal("dirtyAdd requires an added file")
		}
		if err := os.WriteFile(filepath.Join(treeDir, add), []byte("guard dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidate := runGit("rev-parse", "HEAD")

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, workingDirectory := range []string{packageDir, root} {
		for _, inheritedWorkTree := range []string{"", "."} {
			command := exec.Command(binary, "-test.run="+scenario.childTest, "-test.v")
			command.Dir = workingDirectory
			command.Env = append(methodologyScratchGitEnv(""), childMarker+"="+base, scenario.candidateEnv+"="+candidate)
			if inheritedWorkTree != "" {
				command.Env = append(command.Env, "GIT_WORK_TREE="+inheritedWorkTree)
			}
			output, err := command.CombinedOutput()
			if !wantFailure {
				if err != nil {
					t.Fatalf("unchanged sources failed identity guard from %s (GIT_WORK_TREE=%q): %v:\n%s", workingDirectory, inheritedWorkTree, err, output)
				}
				continue
			}
			if err == nil {
				t.Fatalf("%s passed identity guard from %s:\n%s", wantPath, workingDirectory, output)
			}
			if !strings.Contains(string(output), wantPath) {
				t.Fatalf("identity guard failed for another reason from %s: %v:\n%s", workingDirectory, err, output)
			}
		}
	}
}

func TestMethodologyCombinedCheckoutUsesIntegratedBase(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "go", "internal", "query")
	migrationDir := filepath.Join(root, "go", "internal", "storage", "postgres", "migrations")
	for _, directory := range []string{packageDir, migrationDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
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
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(migrationDir, name), []byte("SELECT 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("init", "--quiet", "--initial-branch=main")
	runGit("config", "--local", "maintenance.auto", "false")
	runGit("config", "--local", "gc.auto", "0")
	runGit("config", "user.name", "Eshu Test")
	runGit("config", "user.email", "eshu-test@example.invalid")
	write("001.sql")
	runGit("add", ".")
	runGit("commit", "--quiet", "-m", "common baseline")
	runGit("branch", "feature")
	write("169.sql")
	runGit("add", ".")
	runGit("commit", "--quiet", "-m", "target main migration")
	target := runGit("rev-parse", "HEAD")
	runGit("checkout", "--quiet", "feature")
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "--quiet", "-m", "feature only harness")
	feature := runGit("rev-parse", "HEAD")
	check := func(wantFailure bool, wantPath string) {
		t.Helper()
		runGit("merge", "--quiet", "--no-edit", "main")
		candidate := runGit("rev-parse", "HEAD")
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, "-test.run=^TestMethodologyProductionTreeRejectsBaseOnlyMigration$", "-test.v")
		command.Dir = packageDir
		command.Env = append(methodologyScratchGitEnv(""), "ESHU_METHODOLOGY_IDENTITY_GUARD_CHILD="+target, "ESHU_METHODOLOGY_IDENTITY_GUARD_CANDIDATE="+candidate)
		output, err := command.CombinedOutput()
		if wantFailure {
			if err == nil || !strings.Contains(string(output), wantPath) {
				t.Fatalf("combined candidate-only change to %s escaped parity: %v:\n%s", wantPath, err, output)
			}
		} else if err != nil {
			t.Fatalf("combined candidate-only additive change failed parity: %v:\n%s", err, output)
		}
	}
	check(false, "")
	runGit("checkout", "--quiet", "-b", "candidate-only", feature)
	write("207.sql")
	runGit("add", ".")
	runGit("commit", "--quiet", "-m", "candidate migration")
	check(false, "")
	runGit("checkout", "--quiet", "-b", "candidate-delete", "candidate-only")
	runGit("rm", "--quiet", "go/internal/storage/postgres/migrations/169.sql")
	runGit("commit", "--quiet", "-m", "candidate deletes target migration")
	check(true, "169.sql")
}

func TestMethodologyScratchGitIgnoresAmbientTrace(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "production_tree_fixture_and_child_verifier",
			run: func(t *testing.T) {
				checkMethodologyProductionTreeScenario(t, "ESHU_METHODOLOGY_IDENTITY_GUARD_CHILD", "", "", "", false, false, false, false, false, "", "")
			},
		},
		{name: "combined_checkout_fixture_and_child_verifier", run: TestMethodologyCombinedCheckoutUsesIntegratedBase},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			trace := filepath.Join(t.TempDir(), "ambient-trace.json")
			t.Setenv("GIT_TRACE2_EVENT", trace)
			testCase.run(t)
			if events, err := os.ReadFile(trace); err == nil && len(events) != 0 {
				t.Fatalf("scratch Git leaked %d bytes into ambient trace", len(events))
			} else if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			command := exec.Command("git", "--version")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("ambient trace positive control: %v: %s", err, output)
			}
			events, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(events), `"event":"start"`) {
				t.Fatalf("ambient trace positive control did not record git start: %s", events)
			}
		})
	}
}
