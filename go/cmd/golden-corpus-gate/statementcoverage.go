// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// maxReportedCoverageLines bounds each advisory list in the CI log, like
// MaxReportedDiffs bounds the differential report: the full recordings
// persist in the CI artifact, so past the cap only the remainder count
// prints.
const maxReportedCoverageLines = 20

// runStatementCoverage joins the checked-in builder manifest against the
// differential capture recordings in each -coverage-dirs directory and
// records the verdict as a required gate finding (#6783). An inventoried,
// unexempted builder with no matching recording on a backend fails the
// gate, as does a recorded read whose successful executions all returned
// zero rows. A run that loaded no recordings at all fails too: passing on
// an empty capture would prove nothing.
func runStatementCoverage(o options, stdout io.Writer, r *Report) error {
	manifestPath := strings.TrimSpace(o.coverageManifest)
	if manifestPath == "" {
		return fmt.Errorf("requires -coverage-manifest: path to statement-builders.yaml")
	}
	dirs := make([]string, 0)
	for _, dir := range strings.Split(o.coverageDirs, ",") {
		if dir = strings.TrimSpace(dir); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return fmt.Errorf("requires -coverage-dirs: comma-separated directories of differential capture recordings")
	}
	manifest, err := queryplan.LoadBuilderManifest(manifestPath)
	if err != nil {
		return err
	}
	records := make(map[string][]backendconformance.DifferentialRecord)
	for _, dir := range dirs {
		byBackend, err := capture.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("load capture recordings from %s: %w", dir, err)
		}
		for backend, recs := range byBackend {
			records[backend] = append(records[backend], recs...)
		}
	}
	total := 0
	for _, recs := range records {
		total += len(recs)
	}
	if total == 0 {
		r.AddCheck("statement-coverage", "statements_executed", false, true,
			"no differential recordings loaded from -coverage-dirs: an empty capture proves nothing")
		return nil
	}
	report := backendconformance.ComputeStatementCoverage(manifest, records)
	printCoverageReport(report, stdout)
	failures := report.Failures()
	detail := "every inventoried builder executed on every backend with recordings; no always-empty reads"
	if len(failures) != 0 {
		first := failures[0]
		detail = fmt.Sprintf("statement coverage found %d failure(s), first: %s [%s]: %s",
			len(failures), first.Backend, first.Kind, first.ID)
	}
	r.AddCheck("statement-coverage", "statements_executed", len(failures) == 0, true, detail)
	return nil
}

// printCoverageReport writes the per-backend coverage detail: executed,
// never-executed, and exempted manifest keys, always-empty and failed-only
// reads, advisory same-parameter sibling misses (dispatch-miss), advisory
// counter gaps, and
// unattributed executions. Failures print
// in full (they are usually few); advisory lists are capped.
func printCoverageReport(report backendconformance.StatementCoverageReport, stdout io.Writer) {
	backends := make([]string, 0, len(report.ByBackend))
	for backend := range report.ByBackend {
		backends = append(backends, backend)
	}
	sort.Strings(backends)
	for _, backend := range backends {
		coverage := report.ByBackend[backend]
		_, _ = fmt.Fprintf(stdout, "backend %s: %d executed, %d never-executed, %d exempted\n",
			backend, len(coverage.Executed), len(coverage.NeverExecuted), len(coverage.Exempted))
		printCoverageList(stdout, "executed", coverage.Executed, 0)
		printCoverageList(stdout, "never-executed", coverage.NeverExecuted, 0)
		printCoverageList(stdout, "exempted", coverage.Exempted, 0)
		printCoverageList(stdout, "always-empty-read", coverage.AlwaysEmptyReads, 0)
		printCoverageList(stdout, "failed-read", coverage.FailedReads, maxReportedCoverageLines)
		printCoverageList(stdout, "dispatch-miss", coverage.DispatchMisses, maxReportedCoverageLines)
		printCoverageList(stdout, "write-without-counters", coverage.WritesWithoutCounters, maxReportedCoverageLines)
		printCoverageList(stdout, "unattributed", coverage.Unattributed, maxReportedCoverageLines)
	}
}

// printCoverageList prints every item when limit is 0 (failures are
// usually few and each needs its line); otherwise it caps the list and
// prints the remainder count.
func printCoverageList(stdout io.Writer, name string, items []string, limit int) {
	show := items
	remainder := ""
	if limit > 0 && len(items) > limit {
		show = items[:limit]
		remainder = fmt.Sprintf(" ... and %d more (see recording artifacts)", len(items)-limit)
	}
	for _, item := range show {
		_, _ = fmt.Fprintf(stdout, "- %s: %s\n", name, item)
	}
	if remainder != "" {
		_, _ = fmt.Fprintf(stdout, "- %s%s\n", name, remainder)
	}
}
