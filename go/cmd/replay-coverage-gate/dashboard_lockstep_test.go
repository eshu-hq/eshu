// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// updateDashboard regenerates the committed coverage dashboard from the live
// registries + manifest. Run after a change that moves coverage (a new scenario,
// a registry edit), then review the diff:
//
//	cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard
//
// Without it, TestCommittedDashboardIsCurrent fails on any drift — so the
// committed dashboard can never go stale relative to the real coverage.
var updateDashboard = flag.Bool("update-dashboard", false, "regenerate the committed replay-coverage dashboard")

// committedDashboardPath is the C-7 deliverable: the docs-discoverable Markdown
// dashboard, relative to the repository root.
const committedDashboardRelPath = "docs/public/reference/replay-coverage.md"

// repoRootFromTest resolves the repository root three levels above this package
// (replay-coverage-gate -> cmd -> go -> repo root).
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// renderLiveDashboard builds the coverage report from the real source-of-truth
// registries, manifest, and snapshot, then renders the CI-enforced blocking
// dashboard Markdown.
func renderLiveDashboard(t *testing.T) ([]byte, replaycoverage.CoverageReport) {
	t.Helper()
	root := repoRootFromTest(t)
	o := options{
		specsDir: filepath.Join(root, "specs"),
		snapshot: filepath.Join(root, "testdata", "golden", "e2e-20repo-snapshot.json"),
		repoRoot: root,
	}
	o.manifest = filepath.Join(o.specsDir, replaycoverage.ManifestFileName)
	inputs, err := loadInputs(o)
	if err != nil {
		t.Fatalf("loadInputs: %v", err)
	}
	inputs.Blocking = true
	_, report, _ := replaycoverage.RunGate(inputs)
	return replaycoverage.RenderDashboard(report), report
}

// TestCommittedDashboardIsCurrent is the C-7 (#4179) lockstep gate: the committed
// dashboard must equal what the live coverage renders, so the burn-down stays
// honest in the PR diff. `-update-dashboard` regenerates it.
func TestCommittedDashboardIsCurrent(t *testing.T) {
	rendered, report := renderLiveDashboard(t)
	path := filepath.Join(repoRootFromTest(t), committedDashboardRelPath)

	if *updateDashboard {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir docs dir: %v", err)
		}
		// #nosec G306 -- committed, docs-discoverable artifact, not a secret.
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			t.Fatalf("write dashboard: %v", err)
		}
		t.Logf("updated %s (%d/%d satisfied)", path, report.Totals.Covered+report.Totals.Exempt, report.Totals.Total)
		return
	}

	committed, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative docs path
	if err != nil {
		t.Fatalf("read committed dashboard %q (run with -update-dashboard to create it): %v", path, err)
	}
	if string(committed) != string(rendered) {
		t.Errorf("committed dashboard %q is stale; re-run with -update-dashboard and review the diff", committedDashboardRelPath)
	}
}
