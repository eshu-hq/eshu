// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// licenseHeaderGateID is the registry gate whose local command is
// scripts/verify-license-header.sh.
const licenseHeaderGateID = "license-header"

// trackedGoFiles returns every tracked .go path in the repo, excluding
// vendor/ to match the exclusion scripts/verify-license-header.sh applies.
//
// This approximates, rather than reproduces, the script's own file set: the
// script walks `rg --files -g '*.go'`, which also sees untracked files that
// are not gitignored. Tracked files are the right basis for this assertion
// anyway -- an untracked file is not something a PR's changed-path set can
// contain, so it cannot affect gate selection, which is what this test is
// about. The two sets agree on everything that can reach CI.
func trackedGoFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "*.go")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal("git ls-files failed:", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" || strings.HasPrefix(line, "vendor/") {
			continue
		}
		files = append(files, line)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one tracked .go file")
	}
	return files
}

// TestLicenseHeaderGateTriggersCoverEveryScannedGoFile asserts that the
// license-header gate's declared triggers match every .go file its own verify
// script scans.
//
// This is a scope-agreement check, not a redundant restatement of the script.
// verify-license-header.sh scans the WHOLE repo, but the gate that selects it
// is chosen by matching changed paths against the registry's `triggers`. When
// the trigger set is narrower than the scanned set, a PR touching only a .go
// file in the uncovered region does not select the gate locally, so
// `make pre-pr` passes and CI's verify-contracts job is the first thing to
// fail -- the exact false-green #5535 reports. The two scopes are maintained
// in different files with nothing tying them together, which is why they
// drifted: 312 tracked .go files live outside `go/` (sdk/go/**,
// examples/collector-extensions/**) while the trigger read `go/**`.
//
// Asserting over the real tracked file list rather than a fixed list of
// directories means a future top-level Go directory is covered the day it is
// added, instead of silently reopening the same hole.
func TestLicenseHeaderGateTriggersCoverEveryScannedGoFile(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	regPath := filepath.Join(root, "specs", "ci-gates.v1.yaml")
	if _, err := os.Stat(regPath); os.IsNotExist(err) {
		t.Skip("specs/ci-gates.v1.yaml not yet committed — skipping")
	}

	reg, err := cigates.Load(regPath)
	if err != nil {
		t.Fatal("load registry:", err)
	}

	var triggers []string
	found := false
	for _, g := range reg.Gates {
		if g.ID == licenseHeaderGateID {
			triggers = g.Triggers
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("gate %q not present in the registry", licenseHeaderGateID)
	}

	var uncovered []string
	for _, f := range trackedGoFiles(t, root) {
		matched := false
		for _, trig := range triggers {
			if cigates.MatchGlob(trig, f) {
				matched = true
				break
			}
		}
		if !matched {
			uncovered = append(uncovered, f)
		}
	}

	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		shown := uncovered
		if len(shown) > 10 {
			shown = shown[:10]
		}
		t.Errorf("gate %q triggers %v do not match %d tracked .go file(s) that "+
			"scripts/verify-license-header.sh scans; a PR touching only one of these "+
			"would pass `make pre-pr` and first fail in CI. First offenders:\n  %s",
			licenseHeaderGateID, triggers, len(uncovered), strings.Join(shown, "\n  "))
	}
}
