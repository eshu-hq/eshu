// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit creates a throwaway git repo under dir with the given commits, so
// tests can exercise real `git show <ref>:<path>` baseline resolution rather
// than a stand-in. Each entry in commits is (schema file contents) applied in
// order to schema/aws_resource.v1.schema.json, one commit per entry.
func gitInit(t *testing.T, dir string, commits []string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	schemaDir := filepath.Join(dir, "sdk", "go", "factschema", "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	schemaFile := filepath.Join(schemaDir, "aws_resource.v1.schema.json")
	for i, contents := range commits {
		if err := os.WriteFile(schemaFile, []byte(contents), 0o644); err != nil { //nolint:gosec // test fixture
			t.Fatalf("WriteFile: %v", err)
		}
		run("add", "-A")
		run("commit", "-q", "-m", "commit "+string(rune('a'+i)))
	}
}

// TestRunDetectsBreakAgainstExplicitBaseRef proves that, given an explicit
// -base-ref, the CLI catches a breaking change introduced on a feature branch
// and reports the field + violation type on stderr. The default merge-base
// resolution (the path CI runs) is covered by TestRunResolvesMergeBaseDefault.
func TestRunDetectsBreakAgainstExplicitBaseRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitInit(t, dir, []string{baselineSchema})

	cmd := exec.Command("git", "checkout", "-q", "-b", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	broken := `{
  "properties": {
    "account_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`
	schemaFile := filepath.Join(dir, "sdk", "go", "factschema", "schema", "aws_resource.v1.schema.json")
	if err := os.WriteFile(schemaFile, []byte(broken), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("WriteFile: %v", err)
	}
	commitCmd := exec.Command("git", "commit", "-q", "-am", "break schema")
	commitCmd.Dir = dir
	commitCmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"-repo-root", dir, "-base-ref", "main"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run() error = nil, want a breaking-change failure")
	}
	got := stderr.String()
	if !strings.Contains(got, "resource_id") {
		t.Fatalf("stderr = %q, want it to name the removed field resource_id", got)
	}
	if !strings.Contains(got, string(ViolationRemovedRequiredField)) {
		t.Fatalf("stderr = %q, want it to name the violation type %q", got, ViolationRemovedRequiredField)
	}
}

// TestRunResolvesMergeBaseDefault proves the default baseline resolution — the
// merge-base of HEAD against origin/main, used when -base-ref is omitted (the
// path the CI workflow actually runs) — resolves and catches a break.
func TestRunResolvesMergeBaseDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitInit(t, dir, []string{baselineSchema})

	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// The default path resolves `git merge-base HEAD origin/main`, so the
	// baseline must exist as a remote-tracking ref, not just a local branch.
	gitRun("update-ref", "refs/remotes/origin/main", "main")
	gitRun("checkout", "-q", "-b", "feature")

	broken := `{
  "properties": {
    "account_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`
	schemaFile := filepath.Join(dir, "sdk", "go", "factschema", "schema", "aws_resource.v1.schema.json")
	if err := os.WriteFile(schemaFile, []byte(broken), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("WriteFile: %v", err)
	}
	gitRun("commit", "-q", "-am", "break schema")

	var stdout, stderr bytes.Buffer
	// No -base-ref: exercise the merge-base-against-origin/main default.
	if err := run([]string{"-repo-root", dir}, &stdout, &stderr); err == nil {
		t.Fatalf("run() error = nil, want a breaking-change failure via the merge-base default")
	}
	got := stderr.String()
	if !strings.Contains(got, "resource_id") || !strings.Contains(got, string(ViolationRemovedRequiredField)) {
		t.Fatalf("stderr = %q, want it to name resource_id and %q", got, ViolationRemovedRequiredField)
	}
}

// TestRunPassesWhenNoBaselineCounterpartExists proves a newly added schema
// file (no counterpart in the baseline ref) is not treated as a break — the
// documented behavior for a repo with no contracts release tag yet, and the
// forward-compatible behavior once one exists.
func TestRunPassesWhenNoBaselineCounterpartExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitInit(t, dir, []string{baselineSchema})

	cmd := exec.Command("git", "checkout", "-q", "-b", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	newSchema := `{
  "properties": {"kind": {"type": "string"}},
  "additionalProperties": false,
  "type": "object",
  "required": ["kind"],
  "title": "Eshu incident.event Payload (schema version 1)"
}`
	schemaFile := filepath.Join(dir, "sdk", "go", "factschema", "schema", "incident_event.v1.schema.json")
	if err := os.WriteFile(schemaFile, []byte(newSchema), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("WriteFile: %v", err)
	}
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = dir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "commit", "-q", "-m", "add new schema")
	commitCmd.Dir = dir
	commitCmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"-repo-root", dir, "-base-ref", "main"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, want nil (new schema with no baseline counterpart is not a break); stderr=%s", err, stderr.String())
	}
}

// TestRunHelpDocumentsBaselineBehavior proves --help documents the baseline
// resolution and the new-schema-is-not-a-break behavior explicitly, per
// issue #4569's design decision.
func TestRunHelpDocumentsBaselineBehavior(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"-help"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run(-help) error = nil, want flag.ErrHelp")
	}
	got := stderr.String()
	for _, want := range []string{"base-ref", "merge-base", "new", "not"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Fatalf("-help output = %q, want it to mention %q", got, want)
		}
	}
}

// TestRunDetectsDeletedSchemaFile proves that deleting an entire schema file
// on a feature branch fails the gate. Deleting a fact-kind payload contract
// is a major break, and it must not slip through just because the current
// working tree no longer enumerates the file. The gate must enumerate the
// baseline schema set too and flag any baseline path absent from the current
// tree.
func TestRunDetectsDeletedSchemaFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitInit(t, dir, []string{baselineSchema})

	cmd := exec.Command("git", "checkout", "-q", "-b", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	rmCmd := exec.Command("git", "rm", "-q", "sdk/go/factschema/schema/aws_resource.v1.schema.json")
	rmCmd.Dir = dir
	if out, err := rmCmd.CombinedOutput(); err != nil {
		t.Fatalf("git rm: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "commit", "-q", "-m", "delete aws_resource schema")
	commitCmd.Dir = dir
	commitCmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"-repo-root", dir, "-base-ref", "main"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run() error = nil, want a deleted-schema failure; stdout=%s", stdout.String())
	}
	got := stderr.String()
	if !strings.Contains(got, "aws_resource.v1.schema.json") {
		t.Fatalf("stderr = %q, want it to name the deleted schema file", got)
	}
	if !strings.Contains(got, string(ViolationRemovedSchema)) {
		t.Fatalf("stderr = %q, want it to name the violation type %q", got, ViolationRemovedSchema)
	}
}
