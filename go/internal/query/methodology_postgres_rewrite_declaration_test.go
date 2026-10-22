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

const methodologyPostgresReaderPath = "go/internal/query/cloud_resource_list_store.go"

// methodologyCommittedPostgresReaderRewriteDeclaration permits only the
// AppSync security-filter rewrite while recalibrating the Postgres query
// artifact. Migrations and other readers remain under strict source pins.
var methodologyCommittedPostgresReaderRewriteDeclaration = methodologyRewriteDeclaration{
	Issue:  751,
	Reason: "AppSync API-key disclosure remediation adds owner and source-fact exclusion predicates; the paired Postgres methodology run recalibrates this reader without claiming before/after performance.",
	Paths:  []string{methodologyPostgresReaderPath},
}

var methodologyPostgresReaderRewriteDeclarationKind = methodologyDeclarationKind{
	noun: "PostgreSQL reader rewrite declaration", variable: "methodologyCommittedPostgresReaderRewriteDeclaration",
}

func methodologyVerifyPostgresReadersWithDeclaration(t *testing.T, base, candidate string, paths []string, decl methodologyRewriteDeclaration) {
	t.Helper()
	methodologyVerifySameProductionWithDeclaration(t, base, candidate, paths, decl, methodologyPostgresReaderRewriteDeclarationKind)
}

func methodologyVerifyPostgresProduction(t *testing.T, base, candidate string, migrationPaths []string) {
	t.Helper()
	methodologyVerifyPostgresReadersWithDeclaration(t, base, candidate, []string{methodologyPostgresReaderPath}, methodologyCommittedPostgresReaderRewriteDeclaration)
	methodologyVerifySameProduction(t, base, candidate, migrationPaths)
}

func methodologyPostgresReaderBlobSHA(t *testing.T, revision string) string {
	t.Helper()
	output, err := exec.Command("git", "rev-parse", revision+":"+methodologyPostgresReaderPath).Output()
	if err != nil {
		t.Fatalf("resolve Postgres reader blob at %s: %v", revision, err)
	}
	return strings.TrimSpace(string(output))
}

func TestMethodologyPostgresRewriteDeclarationGate(t *testing.T) {
	const childMarker = "ESHU_METHODOLOGY_POSTGRES_REWRITE_CHILD"
	if spec := os.Getenv(childMarker); spec != "" {
		runMethodologyPostgresRewriteChild(t, spec)
		return
	}

	const reader = methodologyPostgresReaderPath
	const graphReader = "go/internal/query/entity_map_traversal.go"
	const migration = "go/internal/storage/postgres/migrations/001_test.sql"
	for _, testCase := range []struct {
		name        string
		modify      []string
		pin         []string
		dirty       string
		mode        string
		wantFailure bool
		wantPath    string
	}{
		{name: "declared postgres reader", modify: []string{reader}, pin: []string{reader}},
		{name: "undeclared postgres reader", modify: []string{reader}, pin: []string{reader}, mode: "undeclared", wantFailure: true, wantPath: reader},
		{name: "foreign backend reader", modify: []string{reader, graphReader}, pin: []string{reader, graphReader}, mode: "foreign", wantFailure: true, wantPath: graphReader},
		{name: "dirty candidate reader", modify: []string{reader}, pin: []string{reader}, dirty: reader, wantFailure: true, wantPath: reader},
		{name: "migration remains strict", modify: []string{migration}, pin: []string{migration}, mode: "migration", wantFailure: true, wantPath: migration},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkMethodologyPostgresRewriteScenario(t, childMarker, testCase.modify, testCase.pin, testCase.dirty, testCase.mode, testCase.wantFailure, testCase.wantPath)
		})
	}
}

func runMethodologyPostgresRewriteChild(t *testing.T, spec string) {
	t.Helper()
	parts := strings.Split(spec, "\x1f")
	if len(parts) != 5 {
		t.Fatalf("malformed Postgres rewrite spec: %q", spec)
	}
	paths := strings.Split(parts[2], "\x1e")
	switch parts[4] {
	case "migration":
		methodologyVerifySameProduction(t, parts[0], parts[1], paths)
	case "undeclared":
		methodologyVerifyPostgresReadersWithDeclaration(t, parts[0], parts[1], paths, methodologyRewriteDeclaration{})
	case "foreign":
		methodologyVerifyPostgresReadersWithDeclaration(t, parts[0], parts[1], paths, methodologyCommittedPostgresReaderRewriteDeclaration)
	default:
		methodologyVerifyPostgresProduction(t, parts[0], parts[1], nil)
	}
}

func checkMethodologyPostgresRewriteScenario(t *testing.T, childMarker string, modify, pin []string, dirty, mode string, wantFailure bool, wantPath string) {
	t.Helper()
	root := t.TempDir()
	for _, path := range append(append([]string(nil), modify...), pin...) {
		file := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(file); os.IsNotExist(err) {
			if err := os.WriteFile(file, []byte("package query\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
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
	runGit("add", ".")
	runGit("commit", "--quiet", "-m", "seed methodology sources")
	base := runGit("rev-parse", "HEAD")
	for _, path := range modify {
		if err := os.WriteFile(filepath.Join(root, path), []byte("package query\n// changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("add", ".")
	runGit("commit", "--quiet", "--allow-empty", "-m", "candidate methodology sources")
	candidate := runGit("rev-parse", "HEAD")
	if dirty != "" {
		if err := os.WriteFile(filepath.Join(root, dirty), []byte("package query\n// uncommitted edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	spec := strings.Join([]string{base, candidate, strings.Join(pin, "\x1e"), dirty, mode}, "\x1f")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, workingDirectory := range []string{root} {
		command := exec.Command(binary, "-test.run=^TestMethodologyPostgresRewriteDeclarationGate$")
		command.Dir = workingDirectory
		command.Env = append(methodologyScratchGitEnv(""), childMarker+"="+spec)
		output, err := command.CombinedOutput()
		if !wantFailure {
			if err != nil {
				t.Fatalf("declared Postgres rewrite failed from %s: %v:\n%s", workingDirectory, err, output)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s passed Postgres methodology gate from %s:\n%s", wantPath, workingDirectory, output)
		}
		if !strings.Contains(string(output), wantPath) {
			t.Fatalf("Postgres methodology gate failed for another reason from %s: %v:\n%s", workingDirectory, err, output)
		}
	}
}
