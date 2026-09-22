// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
package capture

import (
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

// MaxReportedDiffs bounds the failure report: a systemic backend break
// could diverge thousands of statements, and the gate log must stay
// readable. The full recording files remain in the CI artifact for the
// unbounded case.
const MaxReportedDiffs = 20

// Compare diffs the NornicDB and Neo4j recordings in left and right and
// reports the divergences the allowlist does not excuse to w. It returns
// nil when the two sides agree; execution-count and row-total divergences
// with agreeing results are printed as advisory and do not fail the
// comparison. Either directory may hold either backend:
// sides are identified by the backend labels inside the recordings, not by
// argument order. A side with no recordings fails the comparison instead
// of passing vacuously, so a half-finished run can never look green.
func Compare(left, right string, allow *Allowlist, w io.Writer) error {
	leftByBackend, err := LoadDir(left)
	if err != nil {
		return fmt.Errorf("load left recordings: %w", err)
	}
	rightByBackend, err := LoadDir(right)
	if err != nil {
		return fmt.Errorf("load right recordings: %w", err)
	}
	merged := make(map[string][]backendconformance.DifferentialRecord)
	for backend, records := range leftByBackend {
		merged[backend] = append(merged[backend], records...)
	}
	for backend, records := range rightByBackend {
		merged[backend] = append(merged[backend], records...)
	}
	nornic, okNornic := merged["nornicdb"]
	neo, okNeo := merged["neo4j"]
	if !okNornic || !okNeo {
		return fmt.Errorf("differential comparison needs both backends, have nornicdb=%v neo4j=%v", okNornic, okNeo)
	}
	diffs := backendconformance.CompareRecordings(nornic, neo)
	unexcused, err := allow.Excuse(diffs)
	if err != nil {
		return fmt.Errorf("apply divergence allowlist: %w", err)
	}
	remaining, advisory := backendconformance.SplitAdvisory(unexcused)
	if err := reportAdvisory(w, advisory); err != nil {
		return err
	}
	if len(remaining) == 0 {
		return report(w, "differential comparison clean: %d nornicdb records, %d neo4j records, %d allowlisted, %d advisory\n",
			len(nornic), len(neo), len(diffs)-len(unexcused), len(advisory))
	}
	if err := report(w, "differential comparison found %d unexcused divergence(s) (%d nornicdb records, %d neo4j records):\n",
		len(remaining), len(nornic), len(neo)); err != nil {
		return err
	}
	for i, diff := range remaining {
		if i >= MaxReportedDiffs {
			if err := report(w, "... and %d more (see recording artifacts)\n", len(remaining)-MaxReportedDiffs); err != nil {
				return err
			}
			break
		}
		if err := report(w, "- %s [%s]: %s\n", diff.Fingerprint.Statement, diff.Fingerprint.Parameters, diff.Detail); err != nil {
			return err
		}
	}
	return fmt.Errorf("%d unexcused backend divergence(s), first: %s", len(remaining), summarizeDiff(remaining[0]))
}

// reportAdvisory prints the execution-count and row-total divergences the
// gate holds advisory (see [backendconformance.AdvisoryKind]): named so a
// doubled write stays visible in the CI log, bounded like the failure
// report, never an error. Nothing prints when there are none.
func reportAdvisory(w io.Writer, advisory []backendconformance.DifferentialDifference) error {
	if len(advisory) == 0 {
		return nil
	}
	if err := report(w, "differential comparison: %d advisory scheduling-noise divergence(s) with agreeing results (execution counts or row totals, not gate-failing):\n", len(advisory)); err != nil {
		return err
	}
	for i, diff := range advisory {
		if i >= MaxReportedDiffs {
			return report(w, "... and %d more advisory (see recording artifacts)\n", len(advisory)-MaxReportedDiffs)
		}
		if err := report(w, "- advisory: %s [%s]: %s\n", diff.Fingerprint.Statement, diff.Fingerprint.Parameters, diff.Detail); err != nil {
			return err
		}
	}
	return nil
}

// report writes one formatted line to the comparison report. A short write
// or a closed pipe fails the comparison rather than printing a truncated
// report that looks clean.
func report(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("write differential report: %w", err)
	}
	return nil
}

// summarizeDiff renders one divergence for the error return: the statement
// truncated to its first line so a multi-line statement stays a one-line
// error.
func summarizeDiff(diff backendconformance.DifferentialDifference) string {
	statement, _, _ := strings.Cut(diff.Fingerprint.Statement, "\n")
	return fmt.Sprintf("%s: %s", statement, diff.Detail)
}
