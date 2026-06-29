// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "replay-coverage-gate:", err)
		os.Exit(1)
	}
}

type options struct {
	specsDir  string
	snapshot  string
	manifest  string
	repoRoot  string
	reportOut string
	blocking  bool
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("replay-coverage-gate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.specsDir, "specs-dir", "specs", "directory holding the registry specs")
	fs.StringVar(&o.snapshot, "snapshot", "testdata/golden/e2e-20repo-snapshot.json", "path to the B-12 golden snapshot (for correlation/query-shape scenarios)")
	fs.StringVar(&o.manifest, "manifest", "", "path to the coverage manifest (default: <specs-dir>/replay-coverage-manifest.v1.yaml)")
	fs.StringVar(&o.repoRoot, "repo-root", ".", "repository root that cassette/parser-fixture refs resolve against")
	fs.StringVar(&o.reportOut, "report-out", "", "path to write the JSON coverage report (empty: do not write)")
	fs.BoolVar(&o.blocking, "blocking", false, "fail the gate on any uncovered, unresolved, or stale surface (default: advisory, report only)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if o.manifest == "" {
		o.manifest = filepath.Join(o.specsDir, replaycoverage.ManifestFileName)
	}
	return o, nil
}

func run(args []string, stdout, stderr io.Writer) error {
	o, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}

	inputs, err := loadInputs(o)
	if err != nil {
		return err
	}

	cov, report, gate := replaycoverage.RunGate(inputs)
	gate.Write(stdout)
	writeCoverageSummary(stdout, report)

	if o.reportOut != "" {
		if err := writeReport(o.reportOut, report); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "\ncoverage report written: %s\n", o.reportOut)
	}

	if o.blocking && gate.Failed() {
		return fmt.Errorf("replay coverage gate failed (blocking): %d surface(s) uncovered/unresolved, %d stale manifest entr(ies)",
			len(report.Gaps), len(cov.Stale))
	}
	return nil
}

func loadInputs(o options) (replaycoverage.Inputs, error) {
	inv, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return replaycoverage.Inputs{}, fmt.Errorf("load surface inventory: %w", err)
	}
	ledger, err := replaycoverage.LoadParserLedger(filepath.Join(o.specsDir, replaycoverage.ParserLedgerFileName))
	if err != nil {
		return replaycoverage.Inputs{}, err
	}
	matrix, err := capabilitycatalog.LoadMatrix(o.specsDir)
	if err != nil {
		return replaycoverage.Inputs{}, fmt.Errorf("load capability matrix: %w", err)
	}
	manifest, err := replaycoverage.LoadManifest(o.manifest)
	if err != nil {
		return replaycoverage.Inputs{}, err
	}
	snapshot, err := goldengate.LoadSnapshot(o.snapshot)
	if err != nil {
		return replaycoverage.Inputs{}, fmt.Errorf("load snapshot: %w", err)
	}
	return replaycoverage.Inputs{
		Inventory: inv,
		FactKinds: facts.FactKindRegistry(),
		Ledger:    ledger,
		Matrix:    matrix,
		Manifest:  manifest,
		Resolver:  replaycoverage.ArtifactResolver{RepoRoot: o.repoRoot, Snapshot: snapshot},
		Blocking:  o.blocking,
	}, nil
}

func writeReport(path string, report replaycoverage.CoverageReport) error {
	payload, err := replaycoverage.MarshalReport(report)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create report dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write coverage report %s: %w", path, err)
	}
	return nil
}

// writeCoverageSummary prints the per-registry satisfied percentages and the
// grand total so an operator reading CI logs sees the coverage posture at a
// glance without parsing the JSON artifact.
func writeCoverageSummary(w io.Writer, report replaycoverage.CoverageReport) {
	_, _ = fmt.Fprintf(w, "\n== coverage (%s) ==\n", modeLabel(report.Blocking))
	for _, s := range report.Summaries {
		_, _ = fmt.Fprintf(w, "  %-22s %3d/%-3d satisfied (%.2f%%)  uncovered=%d unresolved=%d exempt=%d\n",
			s.Registry, s.Covered+s.Exempt, s.Total, s.PercentSatisfied, s.Uncovered, s.Unresolved, s.Exempt)
	}
	_, _ = fmt.Fprintf(w, "  %-22s %3d/%-3d satisfied (%.2f%%)  gaps=%d stale=%d\n",
		"TOTAL", report.Totals.Covered+report.Totals.Exempt, report.Totals.Total,
		report.Totals.PercentSatisfied, len(report.Gaps), len(report.Stale))
}

func modeLabel(blocking bool) string {
	if blocking {
		return "blocking"
	}
	return "advisory"
}
