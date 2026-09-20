// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
)

// TestFileFactEnvelopeCollapsesFingerprintWallClock pins the durability
// boundary for parser timing: file facts persist fingerprint outcome counts
// as durable evidence, but micros_total is wall-clock noise that must never
// reach Postgres fact_records (collector telemetry already recorded the
// real timing at prescan). The input map must not be mutated: prescan
// readers and later emission stages still see live values.
func TestFileFactEnvelopeCollapsesFingerprintWallClock(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"path":     "a.go",
		"language": "go",
		fingerprint.StatsKey: map[string]any{
			"fingerprinted": 2,
			"below_floor":   1,
			"has_error":     0,
			"no_body":       0,
			"micros_total":  int64(424242),
		},
	}

	env := FileFactEnvelope("/repo", "repo-x", "scope-x", "gen-x", time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC), fileData, false)

	pfd, ok := env.Payload["parsed_file_data"].(map[string]any)
	if !ok {
		t.Fatalf("file fact payload must embed parsed_file_data, got %#v", env.Payload)
	}
	stats, ok := pfd[fingerprint.StatsKey].(map[string]any)
	if !ok {
		t.Fatalf("file fact must keep the fingerprint outcome counts, got %#v", pfd)
	}
	if got := stats["micros_total"]; got != int64(0) {
		t.Fatalf("durable micros_total = %#v, want 0", got)
	}
	if got := stats["fingerprinted"]; got != 2 {
		t.Fatalf("durable fingerprinted = %#v, want 2 (counts preserved)", got)
	}
	// The live input map keeps its timing for telemetry readers.
	if got := fileData[fingerprint.StatsKey].(map[string]any)["micros_total"]; got != int64(424242) {
		t.Fatalf("input map mutated: micros_total = %#v, want 424242", got)
	}
}
