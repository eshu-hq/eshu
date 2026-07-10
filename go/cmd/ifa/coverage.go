// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

type coverageOptions struct {
	specsDir       string
	snapshot       string
	manifest       string
	replayManifest string
	gates          string
	repoRoot       string
	reportOut      string
	blocking       bool
}

func parseCoverageFlags(args []string, stderr io.Writer) (coverageOptions, error) {
	fs := flag.NewFlagSet("ifa coverage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o coverageOptions
	fs.StringVar(&o.specsDir, "specs-dir", "specs", "directory holding the registry specs")
	fs.StringVar(&o.snapshot, "snapshot", "testdata/golden/e2e-20repo-snapshot.json", "path to the B-12 golden snapshot")
	fs.StringVar(&o.manifest, "manifest", "", "path to the ifa coverage manifest (default: <specs-dir>/"+ifa.ManifestFileName+")")
	fs.StringVar(&o.replayManifest, "replay-manifest", "", "path to the replay coverage manifest (default: <specs-dir>/"+replaycoverage.ManifestFileName+")")
	fs.StringVar(&o.gates, "gates", "", "path to the ci-gates registry (default: <specs-dir>/ci-gates.v1.yaml)")
	fs.StringVar(&o.repoRoot, "repo-root", ".", "repository root (unused today; kept for parity with replay-coverage-gate's flag surface)")
	fs.StringVar(&o.reportOut, "report-out", "", "path to write the JSON coverage report (empty: do not write)")
	fs.BoolVar(&o.blocking, "blocking", false, "fail the gate on any uncovered, unresolved, or stale surface (default: local advisory report)")
	if err := fs.Parse(args); err != nil {
		return coverageOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if o.manifest == "" {
		o.manifest = filepath.Join(o.specsDir, ifa.ManifestFileName)
	}
	if o.replayManifest == "" {
		o.replayManifest = filepath.Join(o.specsDir, replaycoverage.ManifestFileName)
	}
	if o.gates == "" {
		o.gates = filepath.Join(o.specsDir, "ci-gates.v1.yaml")
	}
	return o, nil
}

// runCoverageCommand implements `ifa coverage`. Unlike
// cmd/replay-coverage-gate/main.go, it does not hard-fail on every finding: the
// default is a local advisory report and only `-blocking` mode (what `make prove`
// and CI use) returns a non-zero error. Coverage and proof-gate findings are
// surfaced through ifa.RunCoverage's goldengate.Report so they are visible in the
// report and fail the run only under -blocking. Post-P4 (#4397) the
// ifa-contract-layer proof_gate is itself CI-blocking, so it no longer produces a
// "not blocking" finding of its own.
func runCoverageCommand(args []string, stdout, stderr io.Writer) error {
	o, err := parseCoverageFlags(args, stderr)
	if err != nil {
		return err
	}

	entries := facts.FactKindRegistry()
	byKind := make(map[string]facts.FactKindRegistryEntry, len(entries))
	for _, entry := range entries {
		byKind[entry.Kind] = entry
	}

	snap, err := goldengate.LoadSnapshot(o.snapshot)
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	replayManifest, err := replaycoverage.LoadManifest(o.replayManifest)
	if err != nil {
		return err
	}
	ifaManifest, err := replaycoverage.LoadManifest(o.manifest)
	if err != nil {
		return err
	}
	proofGates, err := cigates.Load(o.gates)
	if err != nil {
		return fmt.Errorf("load ci gate registry: %w", err)
	}

	exp, err := ifa.Derive(entries, snap, replayManifest)
	if err != nil {
		return fmt.Errorf("derive expectations: %w", err)
	}

	in := ifa.CoverageInputs{
		Expectations: exp,
		Manifest:     ifaManifest,
		Catalog:      ifa.CatalogByName(),
		Registry:     byKind,
		ProofGates:   proofGates,
		Blocking:     o.blocking,
	}
	cov, rep, gate := ifa.RunCoverage(in)
	gate.Write(stdout)
	writeCoverageSummary(stdout, rep)

	if o.reportOut != "" {
		if err := writeCoverageReport(o.reportOut, rep); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "\ncoverage report written: %s\n", o.reportOut)
	}

	if o.blocking && gate.Failed() {
		return fmt.Errorf("ifa coverage gate failed (blocking): %d surface(s) uncovered/unresolved, %d stale manifest entr(ies)",
			len(rep.Gaps), len(cov.Stale))
	}
	return nil
}

func writeCoverageReport(path string, rep replaycoverage.CoverageReport) error {
	payload, err := replaycoverage.MarshalReport(rep)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create report dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil { // #nosec G703 -- report-out is an explicit operator-controlled CLI output path for this local gate.
		return fmt.Errorf("write coverage report %s: %w", path, err)
	}
	return nil
}

func writeCoverageSummary(w io.Writer, rep replaycoverage.CoverageReport) {
	_, _ = fmt.Fprintf(w, "\n== coverage (%s) ==\n", modeLabel(rep.Blocking))
	for _, s := range rep.Summaries {
		_, _ = fmt.Fprintf(w, "  %-24s %3d/%-3d satisfied (%.2f%%)  uncovered=%d unresolved=%d exempt=%d\n",
			s.Registry, s.Covered+s.Exempt, s.Total, s.PercentSatisfied, s.Uncovered, s.Unresolved, s.Exempt)
	}
	_, _ = fmt.Fprintf(w, "  %-24s %3d/%-3d satisfied (%.2f%%)  gaps=%d stale=%d\n",
		"TOTAL", rep.Totals.Covered+rep.Totals.Exempt, rep.Totals.Total,
		rep.Totals.PercentSatisfied, len(rep.Gaps), len(rep.Stale))
}

func modeLabel(blocking bool) string {
	if blocking {
		return "blocking"
	}
	return "advisory"
}
