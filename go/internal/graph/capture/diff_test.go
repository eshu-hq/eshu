// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

var errTestWrite = errors.New("test write failure")

// writeDivergentPair stores two recordings of the same statement whose row
// digests differ — the synthetic shape of a backend that misanswers — one
// per backend directory.
func writeDivergentPair(t *testing.T, left, right string) {
	t.Helper()
	nornic, err := OpenDir(left, "nornicdb", "drain-test")
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	if err := nornic.Append(backendconformance.DifferentialRecord{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n", Parameters: "null"},
		Backend:     "nornicdb",
		RowCount:    0,
		Digest:      "empty",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := nornic.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	neo, err := OpenDir(right, "neo4j", "drain-test")
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	if err := neo.Append(backendconformance.DifferentialRecord{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n", Parameters: "null"},
		Backend:     "neo4j",
		RowCount:    3,
		Digest:      "full",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := neo.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestCompareFailsOnDivergence is the gate's seeded RED: a statement whose
// digest differs across backends fails the comparison, and the report names
// the statement so the failure points at its production source.
func TestCompareFailsOnDivergence(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeDivergentPair(t, left, right)
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	var report strings.Builder
	if err := Compare(left, right, allow, &report); err == nil {
		t.Fatal("Compare() error = nil, want divergence failure")
	}
	if !strings.Contains(report.String(), "MATCH (n:Repository) RETURN n") {
		t.Fatalf("Compare() report = %q, want it to name the statement", report.String())
	}
}

// TestComparePassesOnIdenticalRecordings is the GREEN case: byte-identical
// recordings on both sides compare clean.
func TestComparePassesOnIdenticalRecordings(t *testing.T) {
	appendRecord := func(t *testing.T, dir, backend string) {
		t.Helper()
		sink, err := OpenDir(dir, backend, "drain-test")
		if err != nil {
			t.Fatalf("OpenDir() error = %v", err)
		}
		record := backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n", Parameters: "null"},
			Backend:     backend,
			RowCount:    3,
			Digest:      "full",
		}
		if err := sink.Append(record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := sink.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	left := t.TempDir()
	right := t.TempDir()
	appendRecord(t, left, "nornicdb")
	appendRecord(t, right, "neo4j")
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	// Identical multisets on both backend labels is the A/A shape the gate
	// runs to prove the harness is deterministic before trusting A/B.
	var report strings.Builder
	if err := Compare(left, right, allow, &report); err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
}

// TestCompareExcusesAllowlistedDivergence proves the allowlist path through
// the driver: a named divergence with a reason and upstream passes.
func TestCompareExcusesAllowlistedDivergence(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeDivergentPair(t, left, right)
	allow, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: NornicDB drops these rows, tracked upstream
  upstream: https://github.com/orneryd/NornicDB/issues/400
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	var report strings.Builder
	if err := Compare(left, right, allow, &report); err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
}

// writeExecutionsOnlyPair stores two recordings of the same statement with
// the same row digest but different execution counts — the synthetic shape of
// poll-iteration scheduling noise with agreeing results.
func writeExecutionsOnlyPair(t *testing.T, left, right string) {
	t.Helper()
	appendExecutions := func(t *testing.T, dir, backend string, n int) {
		t.Helper()
		sink, err := OpenDir(dir, backend, "drain-test")
		if err != nil {
			t.Fatalf("OpenDir() error = %v", err)
		}
		for range n {
			record := backendconformance.DifferentialRecord{
				Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:EvidenceArtifact) RETURN n", Parameters: "null"},
				Backend:     backend,
				RowCount:    1,
				Digest:      "same",
			}
			if err := sink.Append(record); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
		}
		if err := sink.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	appendExecutions(t, left, "nornicdb", 2)
	appendExecutions(t, right, "neo4j", 3)
}

// TestCompareReportsExecutionsOnlyAsAdvisory pins the #6782 permanent
// disposition: agreeing results with different execution counts are
// scheduling noise by construction (drain passes, retries, regrouped
// batches), so Compare reports them as advisory — named in the report so a
// doubled write stays visible — and returns nil instead of failing the gate.
func TestCompareReportsExecutionsOnlyAsAdvisory(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeExecutionsOnlyPair(t, left, right)
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	var report strings.Builder
	if err := Compare(left, right, allow, &report); err != nil {
		t.Fatalf("Compare() error = %v, want executions-only divergence to be advisory", err)
	}
	out := report.String()
	if !strings.Contains(out, "MATCH (n:EvidenceArtifact) RETURN n") {
		t.Fatalf("Compare() report = %q, want it to name the statement", out)
	}
	if !strings.Contains(out, "advisory") {
		t.Fatalf("Compare() report = %q, want it to label the divergence advisory", out)
	}
}

// TestCompareStillFailsOnResultsWithExecutionsNoise is the RED half beside
// the advisory rule: an executions-only divergence on one statement must not
// soften a results divergence on another in the same comparison.
func TestCompareStillFailsOnResultsWithExecutionsNoise(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeExecutionsOnlyPair(t, left, right)
	appendOne := func(dir, backend, digest string) {
		t.Helper()
		sink, err := OpenDir(dir, backend, "results-test")
		if err != nil {
			t.Fatalf("OpenDir() error = %v", err)
		}
		record := backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (r:Repository) RETURN r", Parameters: "null"},
			Backend:     backend,
			RowCount:    1,
			Digest:      digest,
		}
		if err := sink.Append(record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := sink.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	appendOne(left, "nornicdb", "left-digest")
	appendOne(right, "neo4j", "right-digest")
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	var report strings.Builder
	err = Compare(left, right, allow, &report)
	if err == nil {
		t.Fatal("Compare() error = nil, want the results divergence to fail")
	}
	if !strings.Contains(err.Error(), "1 unexcused backend divergence(s)") {
		t.Fatalf("Compare() error = %v, want exactly the one results divergence counted, not the advisory one", err)
	}
}

// failingWriter errors on every write, proving a truncated report fails the
// comparison instead of printing half a report that looks clean.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errTestWrite }

// TestCompareFailsWhenReportWriteFails pins the report-write contract: even
// a clean comparison fails when its own report cannot be written.
func TestCompareFailsWhenReportWriteFails(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeDivergentPair(t, left, right)
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	if err := Compare(left, right, allow, failingWriter{}); err == nil {
		t.Fatal("Compare() error = nil, want report-write failure")
	}
}

// TestCompareFailsOnMissingBackend keeps a half-finished run from passing
// vacuously: when one side recorded nothing, the comparison fails instead
// of declaring two empty universes equal.
func TestCompareFailsOnMissingBackend(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeDivergentPair(t, left, right)
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	var report strings.Builder
	if err := Compare(left, t.TempDir(), allow, &report); err == nil {
		t.Fatal("Compare() error = nil, want missing-backend failure")
	}
}
