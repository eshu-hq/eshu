// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
)

// runBackendDiff compares the differential capture recordings from a
// NornicDB run against a Neo4j run and records the verdict as a required
// gate finding (#6782). A divergence the allowlist does not excuse fails
// the gate; so does a run that recorded only one backend — comparing a
// backend against itself is not a differential proof.
//
// With -diff-left2/-diff-right2 set, the phase runs multi-leg quorum mode:
// both pairings are excused independently (stale-allowlist enforcement is
// unchanged per pairing) and only divergences reproducing across pairings —
// same fingerprint and kind — fail the gate. Pairing-local noise and
// reproduced scheduling noise (execution counts or row totals with agreeing
// results) stay visible as advisory findings, never as a failure.
func runBackendDiff(o options, stdout io.Writer, r *Report) error {
	left := strings.TrimSpace(o.diffLeft)
	right := strings.TrimSpace(o.diffRight)
	if left == "" || right == "" {
		return fmt.Errorf("requires -diff-left and -diff-right: directories of differential capture recordings")
	}
	raw, err := os.ReadFile(strings.TrimSpace(o.diffAllowlist))
	if err != nil {
		return fmt.Errorf("read divergence allowlist: %w", err)
	}
	allow, err := capture.ParseAllowlist(raw)
	if err != nil {
		return fmt.Errorf("parse divergence allowlist: %w", err)
	}
	if strings.TrimSpace(o.diffLeft2) == "" && strings.TrimSpace(o.diffRight2) == "" {
		// The comparison report goes to stdout for the CI log; the gate
		// verdict itself is the required finding below.
		cmpErr := capture.Compare(left, right, allow, stdout)
		detail := "nornicdb and neo4j recordings agree"
		if cmpErr != nil {
			detail = cmpErr.Error()
		}
		r.AddCheck("backend-diff", "nornicdb_vs_neo4j", cmpErr == nil, true, detail)
		return nil
	}
	if strings.TrimSpace(o.diffLeft2) == "" || strings.TrimSpace(o.diffRight2) == "" {
		return fmt.Errorf("quorum mode requires both -diff-left2 and -diff-right2, or neither")
	}
	if strings.TrimSpace(o.diffLeft2) == left && strings.TrimSpace(o.diffRight2) == right {
		return fmt.Errorf("quorum mode requires two distinct pairings: -diff-left2/-diff-right2 repeat the first pairing, which would silently degrade quorum to single-pair")
	}
	return runBackendDiffQuorum(o, allow, stdout, r)
}

// unexcusedPairing diffs one leg pairing and applies the allowlist, exactly
// as single-pair mode does: stale entries fail here, not in the quorum.
func unexcusedPairing(leftDir, rightDir string, allow *capture.Allowlist) ([]backendconformance.DifferentialDifference, error) {
	merged := make(map[string][]backendconformance.DifferentialRecord)
	for _, dir := range []string{leftDir, rightDir} {
		byBackend, err := capture.LoadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("load %s recordings: %w", dir, err)
		}
		for backend, records := range byBackend {
			merged[backend] = append(merged[backend], records...)
		}
	}
	nornic, okNornic := merged["nornicdb"]
	neo, okNeo := merged["neo4j"]
	if !okNornic || !okNeo {
		return nil, fmt.Errorf("differential comparison needs both backends, have nornicdb=%v neo4j=%v", okNornic, okNeo)
	}
	return allow.Excuse(backendconformance.CompareRecordings(nornic, neo))
}

// runBackendDiffQuorum compares two leg pairings and fails the gate only on
// reproduced divergences of a required kind. Non-reproducing divergences,
// reproduced scheduling noise (execution counts or row totals with
// agreeing results), and divergences on registered transient reads are
// each reported as an advisory finding so a genuine regression stays
// visible while scheduling noise — leg-local or systematic — and
// timing-dependent reads cannot red the gate on their own.
func runBackendDiffQuorum(o options, allow *capture.Allowlist, stdout io.Writer, r *Report) error {
	pairs := [][2]string{
		{strings.TrimSpace(o.diffLeft), strings.TrimSpace(o.diffRight)},
		{strings.TrimSpace(o.diffLeft2), strings.TrimSpace(o.diffRight2)},
	}
	unexcused := make([][]backendconformance.DifferentialDifference, 0, len(pairs))
	var transient []backendconformance.DifferentialDifference
	for i, pair := range pairs {
		remaining, err := unexcusedPairing(pair[0], pair[1], allow)
		if err != nil {
			return fmt.Errorf("pairing %d: %w", i+1, err)
		}
		// Registered transient reads are excluded per pairing like the
		// allowlist, before quorum intersection: an orphan page diverging
		// in both pairings is timing in both, never reproduced truth.
		remaining, excluded := allow.ExcludeTransient(remaining)
		transient = append(transient, excluded...)
		if _, err := fmt.Fprintf(stdout, "pairing %d: %d unexcused divergence(s)\n", i+1, len(remaining)); err != nil {
			return fmt.Errorf("report pairing %d: %w", i+1, err)
		}
		// Bounded like every other gate report: the full recordings persist
		// in the CI artifact, so past the cap only the remainder count prints.
		for j, diff := range remaining {
			if j >= capture.MaxReportedDiffs {
				if _, err := fmt.Fprintf(stdout, "... and %d more (see recording artifacts)\n", len(remaining)-capture.MaxReportedDiffs); err != nil {
					return fmt.Errorf("report pairing %d: %w", i+1, err)
				}
				break
			}
			if _, err := fmt.Fprintf(stdout, "- %s [%s]: %s\n", diff.Fingerprint.Statement, diff.Fingerprint.Parameters, diff.Detail); err != nil {
				return fmt.Errorf("report pairing %d: %w", i+1, err)
			}
		}
		unexcused = append(unexcused, remaining)
	}
	reproduced := backendconformance.QuorumIntersection(unexcused[0], unexcused[1])
	kept, advisory := backendconformance.SplitAdvisory(reproduced)
	detail := "nornicdb and neo4j recordings agree across both pairings"
	if len(kept) != 0 {
		first := kept[0]
		detail = fmt.Sprintf("differential comparison found %d reproduced divergence(s), first: %s: %s", len(kept), first.Fingerprint.Statement, first.Detail)
	}
	r.AddCheck("backend-diff", "nornicdb_vs_neo4j_quorum", len(kept) == 0, true, detail)
	// Reproduced scheduling noise is reported, never failed: the two backends
	// drain at systematically different speeds, so pass counts reproduce
	// across pairings, and one leg observes a converged row in one more poll
	// iteration than the other, so row totals reproduce too — quorum cannot
	// filter either (#6782 permanent disposition as extended by the option-2
	// slice; see backendconformance.AdvisoryKind). The detail names the top
	// statements by reproduced-divergence count, not only the first
	// recorded, so a systemic regression concentrated on a handful of
	// statements is visible without reading the full pairing dump (#6941).
	advisoryDetail := "no reproduced scheduling-noise divergences"
	if len(advisory) != 0 {
		top := backendconformance.TopAdvisoryStatementReports(advisory, topAdvisoryStatementCount, backendconformance.AdvisoryStatementMaxLen)
		advisoryDetail = fmt.Sprintf("%d reproduced scheduling-noise divergence(s) with agreeing results held advisory (execution counts or row totals), top: %s", len(advisory), strings.Join(top, "; "))
	}
	r.AddCheck("backend-diff", "nornicdb_vs_neo4j_executions", len(advisory) == 0, false, advisoryDetail)
	// The advisory total is otherwise unbounded (#6941): a backend regression
	// that triples drain passes would still report as an advisory WARN and
	// pass the gate. -diff-executions-advisory-max is a systemic-regression
	// tripwire, not a tuning target — see the golden-corpus-gate README for
	// the observed-count calibration.
	if o.diffExecutionsAdvisoryMax > 0 && len(advisory) > o.diffExecutionsAdvisoryMax {
		top := backendconformance.TopAdvisoryStatementReports(advisory, topAdvisoryStatementCount, backendconformance.AdvisoryStatementMaxLen)
		ceilingDetail := fmt.Sprintf("%d reproduced scheduling-noise divergence(s) exceed the advisory ceiling of %d (systemic-regression tripwire, #6941), top: %s", len(advisory), o.diffExecutionsAdvisoryMax, strings.Join(top, "; "))
		r.AddCheck("backend-diff", "nornicdb_vs_neo4j_executions_ceiling", false, true, ceilingDetail)
	}
	// Transient-read exclusions report as their own advisory finding, not
	// inside the executions advisory: their digests disagree, so the
	// "agreeing results" wording would be false. Non-required, always
	// visible: a real regression on an orphan page must stay readable in
	// the CI log (#6782 option 1).
	transientDetail := "no transient-read divergences excluded"
	if len(transient) != 0 {
		top := backendconformance.TopAdvisoryStatementReports(transient, topAdvisoryStatementCount, backendconformance.AdvisoryStatementMaxLen)
		transientDetail = fmt.Sprintf("%d transient-read divergence(s) excluded by registration (timing-dependent state), top: %s", len(transient), strings.Join(top, "; "))
	}
	r.AddCheck("backend-diff", "nornicdb_vs_neo4j_transient", len(transient) == 0, false, transientDetail)
	dropped := len(unexcused[0]) + len(unexcused[1]) - 2*len(reproduced)
	r.AddCheck("backend-diff", "nornicdb_vs_neo4j_nonreproducing", true, false,
		fmt.Sprintf("%d pairing-local divergence(s) did not reproduce across pairings (quorum dropped, see pairing reports above)", dropped))
	return nil
}

// topAdvisoryStatementCount bounds how many statements the advisory and
// ceiling findings name in their detail (#6941): enough to identify a
// concentrated regression, short enough to stay a one-line report.
const topAdvisoryStatementCount = 3
