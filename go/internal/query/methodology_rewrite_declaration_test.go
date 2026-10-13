// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration || queryplan_profile_live

package query

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestMethodologyRewriteDeclarationGate is the RED/GREEN contract for the
// author-side recalibration procedure: a rewrite PR may declare the pinned
// reader files it intentionally changes, and the pin then requires the
// base-vs-candidate diff to equal the declared set exactly. Every
// wantFailure arm proves the gate still fails closed.
func TestMethodologyRewriteDeclarationGate(t *testing.T) {
	const childMarker = "ESHU_METHODOLOGY_REWRITE_DECLARATION_CHILD"
	if spec := os.Getenv(childMarker); spec != "" {
		runMethodologyRewriteDeclarationChild(t, spec)
		return
	}

	pinned := []string{
		"go/internal/query/codemodel/file_a.go",
		"go/internal/query/codemodel/file_b.go",
	}
	for _, testCase := range []struct {
		name        string
		modify      []string
		declare     []string
		dirty       string
		wantFailure bool
		wantPath    string
	}{
		{name: "unchanged without declaration"},
		{name: "declared exact change", modify: []string{"file_a.go"}, declare: []string{"file_a.go"}},
		{name: "undeclared change", modify: []string{"file_a.go", "file_b.go"}, declare: []string{"file_a.go"}, wantFailure: true, wantPath: "file_b.go"},
		{name: "stale declaration", modify: []string{"file_a.go"}, declare: []string{"file_a.go", "file_b.go"}, wantFailure: true, wantPath: "file_b.go"},
		{name: "no declaration with diff", modify: []string{"file_a.go"}, wantFailure: true, wantPath: "file_a.go"},
		{name: "unknown declared path", declare: []string{"elsewhere/unknown.go"}, wantFailure: true, wantPath: "unknown.go"},
		{name: "working tree divergence", modify: []string{"file_a.go"}, declare: []string{"file_a.go"}, dirty: "file_a.go", wantFailure: true, wantPath: "file_a.go"},
		{name: "declaration inert when untouched", modify: []string{"file_b.go"}, declare: []string{"file_a.go"}, wantFailure: true, wantPath: "file_b.go"},
		{name: "fully stale declaration passes", declare: []string{"file_a.go"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkMethodologyRewriteDeclarationScenario(t, childMarker, pinned, testCase.modify, testCase.declare, testCase.dirty, testCase.wantFailure, testCase.wantPath)
		})
	}
}

// runMethodologyRewriteDeclarationChild executes one scenario inside the
// re-executed test binary, where CWD is the scratch repo. The spec encodes
// base, candidate, pinned paths, and declared paths.
func runMethodologyRewriteDeclarationChild(t *testing.T, spec string) {
	t.Helper()
	parts := strings.Split(spec, "\x1f")
	if len(parts) != 5 {
		t.Fatalf("malformed rewrite declaration spec: %q", spec)
	}
	issue, err := strconv.Atoi(parts[4])
	if err != nil {
		t.Fatalf("malformed rewrite declaration issue: %q", parts[4])
	}
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, "\x1e")
	}
	decl := methodologyRewriteDeclaration{Issue: issue, Reason: "scenario", Paths: split(parts[3])}
	methodologyVerifySameProductionWithDeclaration(t, parts[0], parts[1], split(parts[2]), decl)
}

func checkMethodologyRewriteDeclarationScenario(t *testing.T, childMarker string, pinned, modify, declare []string, dirty string, wantFailure bool, wantPath string) {
	t.Helper()
	root := t.TempDir()
	packageDir := filepath.Join(root, "go", "internal", "query", "codemodel")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range pinned {
		if err := os.WriteFile(filepath.Join(root, path), []byte("package codemodel\n"), 0o644); err != nil {
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
	runGit("init", "--quiet")
	runGit("config", "--local", "maintenance.auto", "false")
	runGit("config", "--local", "gc.auto", "0")
	runGit("config", "user.name", "Eshu Test")
	runGit("config", "user.email", "eshu-test@example.invalid")
	runGit("add", "go/internal/query/codemodel")
	runGit("commit", "--quiet", "-m", "seed pinned readers")
	base := runGit("rev-parse", "HEAD")
	for _, name := range modify {
		if err := os.WriteFile(filepath.Join(packageDir, name), []byte("package codemodel\n// changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("add", "go/internal/query/codemodel")
	runGit("commit", "--quiet", "--allow-empty", "-m", "candidate readers")
	candidate := runGit("rev-parse", "HEAD")
	if dirty != "" {
		if err := os.WriteFile(filepath.Join(packageDir, dirty), []byte("package codemodel\n// uncommitted edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	declared := make([]string, 0, len(declare))
	for _, name := range declare {
		if strings.Contains(name, "/") {
			declared = append(declared, name)
		} else {
			declared = append(declared, "go/internal/query/codemodel/"+name)
		}
	}
	spec := strings.Join([]string{
		base,
		candidate,
		strings.Join(pinned, "\x1e"),
		strings.Join(declared, "\x1e"),
		"7347",
	}, "\x1f")

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, workingDirectory := range []string{packageDir, root} {
		command := exec.Command(binary, "-test.run=^TestMethodologyRewriteDeclarationGate$")
		command.Dir = workingDirectory
		command.Env = append(methodologyScratchGitEnv(""), childMarker+"="+spec)
		output, err := command.CombinedOutput()
		if !wantFailure {
			if err != nil {
				t.Fatalf("declared scope failed rewrite gate from %s: %v:\n%s", workingDirectory, err, output)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s passed rewrite gate from %s:\n%s", wantPath, workingDirectory, output)
		}
		if !strings.Contains(string(output), wantPath) {
			t.Fatalf("rewrite gate failed for another reason from %s: %v:\n%s", workingDirectory, err, output)
		}
	}
}
