// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillgenFragmentsDir writes a minimal but valid skill-fragments/ tree to
// a temp dir and returns the path. The fragments are the seven canonical
// ids from the S1 design; the body text is the shortest valid Markdown
// for each id, with byte_citation values that always parse and never
// collide with the canonical repo-root fragments.
func skillgenFragmentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ids := []string{
		"operating-standard",
		"truth-labels",
		"capability-profiles",
		"reducer-invariant",
		"local-first",
		"bundle-reproduction",
		"per-collector-matrix",
	}
	for i, id := range ids {
		body := "---\n" +
			"id: " + id + "\n" +
			"version: 1.0.0\n" +
			"byte_citation: docs/test/" + id + ".md#1-1\n" +
			"description: test " + id + "\n" +
			"---\n\n" +
			"# " + id + " Title\n\nbody line " + id + "\n"
		path := filepath.Join(dir, id+".md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fragment %s: %v", path, err)
		}
		_ = i
	}
	return dir
}

// skillgenCatalogFile writes a minimal surface-inventory.v1.yaml fixture
// to a temp dir and returns the path. The fixture has 11 implemented
// collectors matching the package's testCatalogNames; tests pass
// `-catalog` with this path so the cmd does not depend on the
// repo-root editorial overlay.
func skillgenCatalogFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "surface-inventory.v1.yaml")
	const catalog = `version: v1
surfaces:
  - category: collector
    name: git
    readiness: implemented
  - category: collector
    name: documentation
    readiness: implemented
  - category: collector
    name: oci_registry
    readiness: implemented
  - category: collector
    name: aws
    readiness: implemented
  - category: collector
    name: azure
    readiness: implemented
  - category: collector
    name: gcp
    readiness: partial
  - category: collector
    name: kubernetes
    readiness: implemented
  - category: collector
    name: pagerduty
    readiness: implemented
  - category: collector
    name: jira
    readiness: implemented
  - category: collector
    name: package_registry
    readiness: implemented
  - category: collector
    name: grafana
    readiness: not_implemented
`
	if err := os.WriteFile(path, []byte(catalog), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

// runArgs is the common test scaffolding: fragments, expected, and
// catalog paths from the test fixtures, in the cmd's argument form.
func runArgs(command, fragmentsDir, expectedDir, catalogPath string) []string {
	return []string{
		command,
		"-fragments", fragmentsDir,
		"-expected", expectedDir,
		"-catalog", catalogPath,
	}
}

func TestRun_GenWritesAllHostFiles(t *testing.T) {
	t.Parallel()
	fragments := skillgenFragmentsDir(t)
	catalog := skillgenCatalogFile(t)
	expected := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run(runArgs("gen", fragments, expected, catalog), &stdout, &stderr); err != nil {
		t.Fatalf("run gen: %v\nstderr:\n%s", err, stderr.String())
	}
	// Every host must produce a file at <expected>/<host>/<output_path>.
	for _, host := range []string{"claude-code", "cursor", "codex"} {
		hostDir := filepath.Join(expected, host)
		entries, err := os.ReadDir(hostDir)
		if err != nil {
			t.Fatalf("expected/host dir for %s: %v", host, err)
		}
		if len(entries) == 0 {
			t.Errorf("host %s: expected/ dir is empty", host)
		}
	}
	// The summary line should report how many files were written.
	if !strings.Contains(stdout.String(), "wrote 3 host files") {
		t.Errorf("stdout missing summary line:\n%s", stdout.String())
	}
}

func TestRun_CheckExitsZeroOnBaseline(t *testing.T) {
	t.Parallel()
	fragments := skillgenFragmentsDir(t)
	catalog := skillgenCatalogFile(t)
	expected := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run(runArgs("gen", fragments, expected, catalog), &stdout, &stderr); err != nil {
		t.Fatalf("run gen: %v\nstderr:\n%s", err, stderr.String())
	}
	stdout.Reset()
	if err := run(runArgs("check", fragments, expected, catalog), &stdout, &stderr); err != nil {
		t.Fatalf("run check: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "in lockstep") {
		t.Errorf("check stdout missing 'in lockstep' line:\n%s", stdout.String())
	}
}

func TestRun_CheckExitsNonZeroOnDrift(t *testing.T) {
	t.Parallel()
	fragments := skillgenFragmentsDir(t)
	catalog := skillgenCatalogFile(t)
	expected := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run(runArgs("gen", fragments, expected, catalog), &stdout, &stderr); err != nil {
		t.Fatalf("run gen: %v", err)
	}
	// Hand-edit one byte in the on-disk baseline.
	claudePath := filepath.Join(expected, "claude-code", ".claude", "skills", "eshu", "SKILL.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("baseline is empty")
	}
	data[0] ^= 0x01 // flip a bit
	if err := os.WriteFile(claudePath, data, 0o644); err != nil {
		t.Fatalf("write drifted baseline: %v", err)
	}
	stdout.Reset()
	err = run(runArgs("check", fragments, expected, catalog), &stdout, &stderr)
	if err == nil {
		t.Fatal("run check on drifted baseline: error = nil, want drift error")
	}
	if !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("error = %v, want contains 'drifted'", err)
	}
	if !strings.Contains(stdout.String(), "drifted") {
		t.Errorf("stdout missing drift summary:\n%s", stdout.String())
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"bogus"}, &stdout, &stderr); err == nil {
		t.Fatal("run(bogus) error = nil, want unknown subcommand error")
	}
}

func TestRun_NoArgs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{}, &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want usage error")
	}
}

func TestRun_FragmentsDirMissing(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"gen", "-fragments", "/this/path/does/not/exist/anywhere", "-expected", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run gen with missing fragments: error = nil, want error")
	}
}
