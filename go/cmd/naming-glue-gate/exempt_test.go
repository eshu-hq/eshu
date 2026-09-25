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

func writeLedger(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "naming-glue-exempt.tsv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing ledger fixture: %v", err)
	}
	return path
}

func TestLoadExemptPathsSkipsCommentsAndBlanks(t *testing.T) {
	path := writeLedger(t, "# owner-approved exemptions\n\ngo/internal/testutil/contentreader\tDriver leaf; nesting stutters\t#6818\n")
	exempt, err := loadExemptPaths(path)
	if err != nil {
		t.Fatalf("loadExemptPaths() error = %v", err)
	}
	if len(exempt) != 1 || exempt["go/internal/testutil/contentreader"] != "Driver leaf; nesting stutters" {
		t.Fatalf("loadExemptPaths() = %v, want the contentreader row", exempt)
	}
}

func TestLoadExemptPathsMissingFileMeansNone(t *testing.T) {
	exempt, err := loadExemptPaths(filepath.Join(t.TempDir(), "absent.tsv"))
	if err != nil {
		t.Fatalf("loadExemptPaths() error = %v, want nil for a missing ledger", err)
	}
	if len(exempt) != 0 {
		t.Fatalf("loadExemptPaths() = %v, want empty", exempt)
	}
}

func TestLoadExemptPathsMalformedRowFails(t *testing.T) {
	for _, body := range []string{
		"go/internal/testutil/contentreader\n",
		"go/internal/testutil/contentreader\tonly reason\n",
		"\tno path\t#0000\n",
		"go/internal/testutil/contentreader\t\t#0000\n",
		"go/internal/testutil/contentreader\tDriver leaf; nesting stutters\t\n",
	} {
		if _, err := loadExemptPaths(writeLedger(t, body)); err == nil {
			t.Errorf("loadExemptPaths(%q) = nil error, want a malformed-row failure", body)
		}
	}
}

func TestFilterExemptMatchesFullPathOnly(t *testing.T) {
	candidates := []Candidate{
		{Path: "go/internal/testutil/contentreader", Name: "contentreader"},
		{Path: "go/internal/other/contentreader", Name: "contentreader"},
		{Path: "go/internal/reducer/workloadinstance", Name: "workloadinstance"},
	}
	exempt := map[string]string{"go/internal/testutil/contentreader": "owner-approved"}

	kept := filterExempt(candidates, exempt)

	if len(kept) != 2 || kept[0].Path != "go/internal/other/contentreader" || kept[1].Path != "go/internal/reducer/workloadinstance" {
		t.Fatalf("filterExempt() = %v, want the same-named other dir and the unlisted dir kept in order", kept)
	}
}

func TestFilterExemptEmptyLedgerKeepsAll(t *testing.T) {
	candidates := []Candidate{{Path: "go/internal/reducer/workloadinstance", Name: "workloadinstance"}}
	if kept := filterExempt(candidates, nil); len(kept) != 1 {
		t.Fatalf("filterExempt() with nil ledger = %v, want all kept", kept)
	}
}

func TestRunExemptCandidateSkipsClassifierAndPasses(t *testing.T) {
	root := t.TempDir()
	ledgerDir := filepath.Join(root, "scripts", "lib")
	if err := os.MkdirAll(ledgerDir, 0o755); err != nil {
		t.Fatalf("creating ledger dir: %v", err)
	}
	ledger := "go/internal/testutil/contentreader\tDriver leaf; nesting stutters\t#6818\n"
	if err := os.WriteFile(filepath.Join(ledgerDir, "naming-glue-exempt.tsv"), []byte(ledger), 0o600); err != nil {
		t.Fatalf("writing ledger: %v", err)
	}

	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/testutil/contentreader"},
	}}
	classifier := &fakeClassifier{}
	var stdout, stderr bytes.Buffer

	args := append(baseArgs(), "-repo-root", root)
	code := run(args, &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr: %s", code, stderr.String())
	}
	if classifier.called {
		t.Error("run() called the classifier for an exempt directory, want it filtered first")
	}
}

func TestRunExplicitExemptFileFilters(t *testing.T) {
	path := writeLedger(t, "go/internal/query/taghistory\tHistorical name, grandfathered\t#0000\n")
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/query/taghistory"},
	}}
	classifier := &fakeClassifier{}
	var stdout, stderr bytes.Buffer

	args := append(baseArgs(), "-exempt-file", path)
	code := run(args, &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr: %s", code, stderr.String())
	}
	if classifier.called {
		t.Error("run() called the classifier despite -exempt-file covering the candidate")
	}
}

func TestRunMalformedLedgerExitsTwo(t *testing.T) {
	path := writeLedger(t, "go/internal/query/taghistory\n")
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/query/taghistory"},
	}}
	classifier := &fakeClassifier{}
	var stdout, stderr bytes.Buffer

	args := append(baseArgs(), "-exempt-file", path)
	code := run(args, &stdout, &stderr, classifier, runner, fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "k"}))

	if code != 2 {
		t.Fatalf("run() = %d, want 2 for a malformed ledger; stderr: %s", code, stderr.String())
	}
	if classifier.called {
		t.Error("run() called the classifier despite the ledger failing to parse")
	}
	if !strings.Contains(stderr.String(), "exemption ledger") {
		t.Errorf("stderr should name the exemption ledger, got: %s", stderr.String())
	}
}
