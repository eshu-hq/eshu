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
// reproduced execution-count noise stay visible as advisory findings, never
// as a failure.
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
// reproduced divergences of a required kind. Non-reproducing divergences
// and reproduced execution-count noise are each reported as an advisory
// finding so a genuine regression stays visible while scheduling noise —
// leg-local or systematic — cannot red the gate on its own.
func runBackendDiffQuorum(o options, allow *capture.Allowlist, stdout io.Writer, r *Report) error {
	pairs := [][2]string{
		{strings.TrimSpace(o.diffLeft), strings.TrimSpace(o.diffRight)},
		{strings.TrimSpace(o.diffLeft2), strings.TrimSpace(o.diffRight2)},
	}
	unexcused := make([][]backendconformance.DifferentialDifference, 0, len(pairs))
	for i, pair := range pairs {
		remaining, err := unexcusedPairing(pair[0], pair[1], allow)
		if err != nil {
			return fmt.Errorf("pairing %d: %w", i+1, err)
		}
		if _, err := fmt.Fprintf(stdout, "pairing %d: %d unexcused divergence(s)\n", i+1, len(remaining)); err != nil {
			return fmt.Errorf("report pairing %d: %w", i+1, err)
		}
		for _, diff := range remaining {
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
	// Reproduced execution-count noise is reported, never failed: the two
	// backends drain at systematically different speeds, so pass counts
	// reproduce across pairings and quorum cannot filter them (#6782
	// permanent disposition; see backendconformance.AdvisoryKind).
	advisoryDetail := "no reproduced execution-count divergences"
	if len(advisory) != 0 {
		first := advisory[0]
		advisoryDetail = fmt.Sprintf("%d reproduced execution-count divergence(s) with agreeing results held advisory (scheduling noise), first: %s: %s", len(advisory), first.Fingerprint.Statement, first.Detail)
	}
	r.AddCheck("backend-diff", "nornicdb_vs_neo4j_executions", len(advisory) == 0, false, advisoryDetail)
	dropped := len(unexcused[0]) + len(unexcused[1]) - 2*len(reproduced)
	r.AddCheck("backend-diff", "nornicdb_vs_neo4j_nonreproducing", true, false,
		fmt.Sprintf("%d pairing-local divergence(s) did not reproduce across pairings (quorum dropped, see pairing reports above)", dropped))
	return nil
}
