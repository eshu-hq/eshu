// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
)

// maxReportedWriterFindings bounds the CI log like the other capture phases:
// the recordings persist in the artifact, so past the cap only the remainder
// count prints.
const maxReportedWriterFindings = 20

// runWriterCoverage is the replay half of the writer-coverage gate (#7212).
// Over the differential capture recordings in each -coverage-dirs directory it
// requires every statement that writes a node id to name at least one label in
// the union of the uid-constrained and id-constrained label sets, because the
// Neo4j entity-context anchor can reach a node only through such a label. The
// verdict is a required finding. An empty capture, or one that never sees an id
// write, fails: either would pass a broken analyzer.
func runWriterCoverage(o options, stdout io.Writer, r *Report) error {
	dirs := splitCSV(o.coverageDirs)
	if len(dirs) == 0 {
		return fmt.Errorf("requires -coverage-dirs: comma-separated directories of differential capture recordings")
	}
	var statements []anchor.Statement
	for _, dir := range dirs {
		byBackend, err := capture.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("load capture recordings from %s: %w", dir, err)
		}
		for _, records := range byBackend {
			statements = append(statements, writerStatements(records)...)
		}
	}
	if len(statements) == 0 {
		r.AddCheck("writer-coverage", "id_writers_anchored", false, true,
			"no differential recordings loaded from -coverage-dirs: an empty capture proves nothing")
		return nil
	}
	report := anchor.CheckWriters(statements, anchor.Labels())
	if report.IDWrites == 0 {
		r.AddCheck("writer-coverage", "id_writers_anchored", false, true,
			fmt.Sprintf("%d distinct statements recorded and none writes a node id: the analyzer or the capture is blind", report.Statements))
		return nil
	}
	printWriterFindings(report, stdout)
	detail := fmt.Sprintf("%d distinct statements, %d id-writing; every id write names a uid-constrained or id-constrained label", report.Statements, report.IDWrites)
	if len(report.Findings) != 0 {
		first := report.Findings[0]
		detail = fmt.Sprintf("%d id write(s) outside the anchor label set (of %d id-writing), first: %s on %s labels=[%s] callsite=%q",
			len(report.Findings), report.IDWrites, first.Kind, first.Variable, strings.Join(first.Labels, ":"), first.Callsite)
	}
	r.AddCheck("writer-coverage", "id_writers_anchored", len(report.Findings) == 0, true, detail)
	return nil
}

// writerStatements adapts capture records to the analyzer's input.
func writerStatements(records []backendconformance.DifferentialRecord) []anchor.Statement {
	out := make([]anchor.Statement, 0, len(records))
	for _, record := range records {
		out = append(out, anchor.Statement{
			Text:       record.Fingerprint.Statement,
			Parameters: record.Fingerprint.Parameters,
			Callsite:   record.Callsite,
		})
	}
	return out
}

// printWriterFindings writes each uncovered id write, capped.
func printWriterFindings(report anchor.Report, stdout io.Writer) {
	for i, finding := range report.Findings {
		if i >= maxReportedWriterFindings {
			_, _ = fmt.Fprintf(stdout, "- ... and %d more (see recording artifacts)\n", len(report.Findings)-maxReportedWriterFindings)
			return
		}
		_, _ = fmt.Fprintf(stdout, "- id write outside the anchor labels: %s on %s labels=[%s] callsite=%q statement=%s\n",
			finding.Kind, finding.Variable, strings.Join(finding.Labels, ":"), finding.Callsite, finding.Statement)
	}
}
