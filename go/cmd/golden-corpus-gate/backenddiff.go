// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph/capture"
)

// runBackendDiff compares the differential capture recordings from a
// NornicDB run against a Neo4j run and records the verdict as a required
// gate finding (#6782). A divergence the allowlist does not excuse fails
// the gate; so does a run that recorded only one backend — comparing a
// backend against itself is not a differential proof.
func runBackendDiff(o options, stdout io.Writer, r *Report) error {
	left := strings.TrimSpace(o.diffLeft)
	right := strings.TrimSpace(o.diffRight)
	if left == "" || right == "" {
		return fmt.Errorf("requires -diff-left and -diff-right: directories of differential capture recordings")
	}
	raw, err := os.ReadFile(o.diffAllowlist)
	if err != nil {
		return fmt.Errorf("read divergence allowlist: %w", err)
	}
	allow, err := capture.ParseAllowlist(raw)
	if err != nil {
		return fmt.Errorf("parse divergence allowlist: %w", err)
	}
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
