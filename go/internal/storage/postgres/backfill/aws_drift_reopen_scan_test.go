// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// errFakeRows replays zero rows then reports a terminal iteration error,
// covering the rows.Err leg of the listing scanners.
type errFakeRows struct {
	queueFakeRows
	iterErr error
}

func (r *errFakeRows) Err() error { return r.iterErr }

// TestScanAWSDriftReopenCandidatesSplitsReopenAndSkipped pins the core
// listing contract: changed rows come back in listing order, unchanged rows
// count as skipped_unchanged instead of reopening.
func TestScanAWSDriftReopenCandidatesSplitsReopenAndSkipped(t *testing.T) {
	t.Parallel()
	rows := &queueFakeRows{rows: [][]any{
		{"work-a", true},
		{"work-b", false},
		{"work-c", true},
	}}
	reopen, skipped, err := ScanAWSDriftReopenCandidates(rows)
	if err != nil {
		t.Fatalf("ScanAWSDriftReopenCandidates returned error: %v", err)
	}
	if len(reopen) != 2 || reopen[0] != "work-a" || reopen[1] != "work-c" {
		t.Fatalf("reopen = %q, want [work-a work-c] in listing order", reopen)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

// TestScanAWSDriftReopenCandidatesSkipsBlankIDs pins that blank work-item
// IDs never reopen and never inflate the skipped_unchanged count: they are
// malformed listing output, not evidence of unchanged input.
func TestScanAWSDriftReopenCandidatesSkipsBlankIDs(t *testing.T) {
	t.Parallel()
	rows := &queueFakeRows{rows: [][]any{
		{"", true},
		{"   ", false},
		{"\t", true},
		{"work-ok", true},
	}}
	reopen, skipped, err := ScanAWSDriftReopenCandidates(rows)
	if err != nil {
		t.Fatalf("ScanAWSDriftReopenCandidates returned error: %v", err)
	}
	if len(reopen) != 1 || reopen[0] != "work-ok" {
		t.Fatalf("reopen = %q, want [work-ok]", reopen)
	}
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0 (blank IDs are dropped, not counted)", skipped)
	}
}

// TestScanAWSDriftReopenCandidatesEmptyResult pins the all-skipped shape:
// zero rows yield an empty (non-nil, caller-appendable) reopen list and a
// zero skipped count.
func TestScanAWSDriftReopenCandidatesEmptyResult(t *testing.T) {
	t.Parallel()
	reopen, skipped, err := ScanAWSDriftReopenCandidates(&queueFakeRows{})
	if err != nil {
		t.Fatalf("ScanAWSDriftReopenCandidates returned error: %v", err)
	}
	if reopen == nil {
		t.Fatal("reopen is nil, want empty non-nil slice")
	}
	if len(reopen) != 0 {
		t.Fatalf("reopen = %q, want empty", reopen)
	}
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0", skipped)
	}
}

// TestScanAWSDriftReopenCandidatesScanErrorFails pins that a malformed row
// surfaces a wrapped scan error instead of silently dropping the candidate.
func TestScanAWSDriftReopenCandidatesScanErrorFails(t *testing.T) {
	t.Parallel()
	rows := &queueFakeRows{rows: [][]any{
		{42, true},
	}}
	_, _, err := ScanAWSDriftReopenCandidates(rows)
	if err == nil {
		t.Fatal("ScanAWSDriftReopenCandidates succeeded on a malformed row, want error")
	}
	if !strings.Contains(err.Error(), "scan aws_cloud_runtime_drift reopen candidate") {
		t.Fatalf("error = %q, want the scan wrap", err)
	}
}

// TestScanAWSDriftReopenCandidatesRowsErrorFails pins that a terminal
// iteration error surfaces a wrapped listing error instead of reporting a
// partial (possibly empty) reopen set as complete.
func TestScanAWSDriftReopenCandidatesRowsErrorFails(t *testing.T) {
	t.Parallel()
	rows := &errFakeRows{iterErr: errors.New("connection reset")}
	_, _, err := ScanAWSDriftReopenCandidates(rows)
	if err == nil {
		t.Fatal("ScanAWSDriftReopenCandidates succeeded on rows.Err, want error")
	}
	if !strings.Contains(err.Error(), "list aws_cloud_runtime_drift reopen candidates") {
		t.Fatalf("error = %q, want the listing wrap", err)
	}
}

// TestAWSDriftFingerprintEmptySetIsNeverNull pins the empty-scope edge: the
// scalar is COALESCE-wrapped to the contract's empty fingerprint so the
// listing never compares NULL, and the digest stays sha256 (OpenSSL FIPS
// rejects md5 in production SQL, #6753).
func TestAWSDriftFingerprintEmptySetIsNeverNull(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{
		AWSDriftARNFingerprintSQL("work.scope_id", "scope.active_generation_id"),
		AWSDriftARNFingerprintSQL("$1", "$2"),
	} {
		if !strings.HasPrefix(strings.TrimSpace(expr), "COALESCE((") {
			t.Fatalf("fingerprint scalar is not COALESCE-wrapped:\n%s", expr)
		}
		if !strings.Contains(expr, reducercontract.EmptyARNInputFingerprint) {
			t.Fatalf("fingerprint scalar misses the empty-set constant:\n%s", expr)
		}
		if strings.Contains(expr, "md5(") {
			t.Fatalf("fingerprint scalar uses md5:\n%s", expr)
		}
		if !strings.Contains(expr, "sha256(") {
			t.Fatalf("fingerprint scalar misses sha256:\n%s", expr)
		}
	}
	if strings.Contains(AWSDriftSnapshotRowsSQL, "md5(") {
		t.Fatalf("snapshot rows use md5:\n%s", AWSDriftSnapshotRowsSQL)
	}
}

// TestAWSDriftPairsFragmentCollapseShape pins the D4 collapse rule's SQL
// edges: DISTINCT payload hashes per ARN, one GROUP BY on the ARN only, the
// digest ordered deterministically, and non-ARN facts excluded before the
// fold.
func TestAWSDriftPairsFragmentCollapseShape(t *testing.T) {
	t.Parallel()
	pairs := awsDriftARNPairsSQL("work.scope_id", "scope.active_generation_id")
	if !strings.Contains(pairs, "SELECT DISTINCT") {
		t.Fatalf("pair fragment misses DISTINCT:\n%s", pairs)
	}
	if !strings.Contains(pairs, "ORDER BY pair.payload_hash") {
		t.Fatalf("pair fragment digest is not deterministically ordered:\n%s", pairs)
	}
	if !strings.Contains(pairs, "fact.fact_kind = 'aws_resource'") {
		t.Fatalf("pair fragment misses the aws_resource filter:\n%s", pairs)
	}
	if !strings.Contains(pairs, "COALESCE(fact.payload->>'arn', '')") {
		t.Fatalf("pair fragment misses the blank-ARN guard:\n%s", pairs)
	}
}

// TestAWSDriftReopenSelectCarriesAllFiveGates pins every input_changed OR
// leg of the gated listing: missing row, moved logic version, changed
// fingerprint, and both audit-stale shapes (never audited, older than the
// 24h audit interval twin of awsDriftAuditInterval).
func TestAWSDriftReopenSelectCarriesAllFiveGates(t *testing.T) {
	t.Parallel()
	if !strings.Contains(awsDriftReopenSelect, "fingerprint.work_item_id IS NULL") {
		t.Fatalf("reopen select misses the missing-row leg:\n%s", awsDriftReopenSelect)
	}
	wantVersion := "fingerprint.logic_version IS DISTINCT FROM " +
		strconv.Itoa(reducercontract.AWSRuntimeDriftLogicVersion)
	if !strings.Contains(awsDriftReopenSelect, wantVersion) {
		t.Fatalf("reopen select misses the logic-version leg %q:\n%s", wantVersion, awsDriftReopenSelect)
	}
	if !strings.Contains(awsDriftReopenSelect, "fingerprint.arn_fingerprint IS DISTINCT FROM") {
		t.Fatalf("reopen select misses the fingerprint-mismatch leg:\n%s", awsDriftReopenSelect)
	}
	if !strings.Contains(awsDriftReopenSelect, "fingerprint.last_audit_at IS NULL") {
		t.Fatalf("reopen select misses the never-audited leg:\n%s", awsDriftReopenSelect)
	}
	if !strings.Contains(awsDriftReopenSelect, "INTERVAL '24 hours'") {
		t.Fatalf("reopen select misses the 24h audit-stale leg:\n%s", awsDriftReopenSelect)
	}
	if !strings.Contains(awsDriftReopenSelect, "AS input_changed") {
		t.Fatalf("reopen select misses the input_changed alias:\n%s", awsDriftReopenSelect)
	}
}

// TestListAWSDriftReopenCandidatesQueryComposition pins that the listing is
// the succeeded-items replay floor for the drift domain plus the gated
// select: same CTE, joins, fingerprint join, stage marker, domain bind, and
// bound.
func TestListAWSDriftReopenCandidatesQueryComposition(t *testing.T) {
	t.Parallel()
	q := ListAWSDriftReopenCandidatesQuery
	for _, want := range []string{
		scopeReplayFloorCTE,
		scopeReplayFloorJoins,
		awsDriftReopenFingerprintJoin,
		scopeReplayFloorStage,
		"work.domain = $1",
		scopeReplayFloorBound,
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("listing misses %q", want)
		}
	}
	if !strings.Contains(q, awsDriftReopenSelect) {
		t.Fatal("listing misses the gated input_changed select")
	}
}

// TestRecordAWSDriftReopenFingerprintQueryAuditClock pins the audit-clock
// contract in the record statement: a full run stamps last_audit_at, a delta
// insert leaves it NULL, and a delta never overwrites an existing stamp.
func TestRecordAWSDriftReopenFingerprintQueryAuditClock(t *testing.T) {
	t.Parallel()
	q := RecordAWSDriftReopenFingerprintQuery
	if !strings.Contains(q, "CASE WHEN $9 THEN clock_timestamp() ELSE NULL END") {
		t.Fatalf("record misses the full-run audit-clock CASE:\n%s", q)
	}
	if !strings.Contains(q, "COALESCE(EXCLUDED.last_audit_at, aws_drift_reopen_fingerprints.last_audit_at)") {
		t.Fatalf("record misses the delta audit-clock preserve:\n%s", q)
	}
	if !strings.Contains(q, "FOR NO KEY UPDATE") {
		t.Fatalf("record misses the claim fence lock:\n%s", q)
	}
	if !strings.Contains(q, "aws_drift_reopen_fingerprints.claimed_at <= EXCLUDED.claimed_at") {
		t.Fatalf("record misses the newest-claim-wins guard:\n%s", q)
	}
}
