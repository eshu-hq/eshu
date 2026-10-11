// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration || queryplan_profile_live

package query

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMethodologyProductionTreeRejectsBaseOnlyMigration(t *testing.T) {
	const childMarker = "ESHU_METHODOLOGY_IDENTITY_GUARD_CHILD"
	if base := os.Getenv(childMarker); base != "" {
		methodologyVerifySameProductionTree(t, base, os.Getenv("ESHU_METHODOLOGY_IDENTITY_GUARD_CANDIDATE"), "go/internal/storage/postgres/migrations", "*.sql")
		return
	}

	for _, testCase := range []struct {
		name          string
		remove        string
		add           string
		modify        string
		empty         bool
		commitDeleted bool
		wantFailure   bool
		wantPath      string
	}{
		{name: "base only", remove: "002_base_only.sql", wantFailure: true, wantPath: "002_base_only.sql"},
		{name: "candidate only", add: "003_candidate_only.sql", wantFailure: true, wantPath: "003_candidate_only.sql"},
		{name: "modified", modify: "001_unchanged.sql", wantFailure: true, wantPath: "001_unchanged.sql"},
		{name: "committed deletion restored untracked", commitDeleted: true, wantFailure: true, wantPath: "002_base_only.sql"},
		{name: "empty inventory", empty: true, wantFailure: true, wantPath: "no production sources"},
		{name: "unchanged"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkMethodologyProductionTreeScenario(t, childMarker, testCase.remove, testCase.add, testCase.modify, testCase.empty, testCase.commitDeleted, testCase.wantFailure, testCase.wantPath)
		})
	}
}

func checkMethodologyProductionTreeScenario(t *testing.T, childMarker, remove, add, modify string, empty, commitDeleted, wantFailure bool, wantPath string) {
	t.Helper()
	root := t.TempDir()
	packageDir := filepath.Join(root, "go", "internal", "query")
	migrationDir := filepath.Join(root, "go", "internal", "storage", "postgres", "migrations")
	for _, directory := range []string{packageDir, migrationDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seed := []string{"001_unchanged.sql", "002_base_only.sql"}
	if empty {
		seed = []string{"README"}
	}
	for _, name := range seed {
		if err := os.WriteFile(filepath.Join(migrationDir, name), []byte("SELECT 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init", "--quiet")
	runGit("config", "user.name", "Eshu Test")
	runGit("config", "user.email", "eshu-test@example.invalid")
	runGit("add", "go/internal/storage/postgres/migrations")
	runGit("commit", "--quiet", "-m", "seed migration identity")
	base := runGit("rev-parse", "HEAD")
	if remove != "" {
		if err := os.Remove(filepath.Join(migrationDir, remove)); err != nil {
			t.Fatal(err)
		}
	}
	if add != "" {
		if err := os.WriteFile(filepath.Join(migrationDir, add), []byte("SELECT 2;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if modify != "" {
		if err := os.WriteFile(filepath.Join(migrationDir, modify), []byte("SELECT 3;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if commitDeleted {
		path := filepath.Join(migrationDir, "002_base_only.sql")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		runGit("add", "-A")
		runGit("commit", "--quiet", "-m", "remove candidate migration")
		if err := os.WriteFile(path, []byte("SELECT 1;\n"), 0o644); err != nil {
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
			command := exec.Command(binary, "-test.run=^TestMethodologyProductionTreeRejectsBaseOnlyMigration$", "-test.v")
			command.Dir = workingDirectory
			command.Env = append(os.Environ(), childMarker+"="+base, "ESHU_METHODOLOGY_IDENTITY_GUARD_CANDIDATE="+candidate)
			if inheritedWorkTree != "" {
				command.Env = append(command.Env, "GIT_WORK_TREE="+inheritedWorkTree)
			}
			output, err := command.CombinedOutput()
			if !wantFailure {
				if err != nil {
					t.Fatalf("unchanged migrations failed identity guard from %s (GIT_WORK_TREE=%q): %v:\n%s", workingDirectory, inheritedWorkTree, err, output)
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
	check := func(wantFailure bool) {
		t.Helper()
		runGit("merge", "--quiet", "--no-edit", "main")
		candidate := runGit("rev-parse", "HEAD")
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, "-test.run=^TestMethodologyProductionTreeRejectsBaseOnlyMigration$", "-test.v")
		command.Dir = packageDir
		command.Env = append(os.Environ(), "ESHU_METHODOLOGY_IDENTITY_GUARD_CHILD="+target, "ESHU_METHODOLOGY_IDENTITY_GUARD_CANDIDATE="+candidate)
		output, err := command.CombinedOutput()
		if wantFailure {
			if err == nil || !strings.Contains(string(output), "207.sql") {
				t.Fatalf("combined candidate-only migration escaped parity: %v:\n%s", err, output)
			}
		} else if err != nil {
			t.Fatalf("integrated target migration failed parity: %v:\n%s", err, output)
		}
	}
	check(false)
	runGit("checkout", "--quiet", "-b", "candidate-only", feature)
	write("207.sql")
	runGit("add", ".")
	runGit("commit", "--quiet", "-m", "candidate migration")
	check(true)
}
