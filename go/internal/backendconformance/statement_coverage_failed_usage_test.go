// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"errors"
	"slices"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

var errFailedUsageSentinel = errors.New("sentinel failure")

// Failed reads as exemption usage (#7505, deferred P3 from #7501):
// exemptionUsed skips digest-less records, so an exemption reads stale
// when its matching read was recorded on every backend but failed
// everywhere (capture leaves Digest empty for query or digest errors).
// A matching failed READ counts as usage; a failed WRITE with
// anchor-matching text provably does not (the read/write discriminator
// on DifferentialRecord is the guard).

func failedUsageRead(backend, callsite, statement string) DifferentialRecord {
	return DifferentialRecord{
		Backend:     backend,
		Callsite:    callsite,
		Fingerprint: DifferentialFingerprint{Statement: statement, Parameters: `{}`},
		Failed:      true,
		Error:       "connection refused",
		Kind:        DifferentialRecordRead,
	}
}

func failedUsageWrite(backend, callsite, statement string, failed bool) DifferentialRecord {
	return DifferentialRecord{
		Backend:     backend,
		Callsite:    callsite,
		Fingerprint: DifferentialFingerprint{Statement: statement, Parameters: `{}`},
		Failed:      failed,
		Error:       "write failed",
		Kind:        DifferentialRecordWrite,
	}
}

// (AC2) an exemption whose read failed on ALL backends (anchor-matching
// failed records, digest-less) counts as used — no stale failure — while
// the read itself stays advisory failed-only (reported, never failing).
func TestAllBackendsFailedReadsCountAsExemptionUsage(t *testing.T) {
	records := map[string][]DifferentialRecord{
		"neo4j":    {failedUsageRead("neo4j", identityCallsite, identityText)},
		"nornicdb": {failedUsageRead("nornicdb", identityCallsite, identityText)},
	}
	report := ComputeStatementCoverage(identityManifest(), records)
	if failures := report.Failures(); len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: all-backends-failed reads count as exemption usage", failures)
	}
	want := normalizeCoverageText(identityText)
	for _, backend := range []string{"neo4j", "nornicdb"} {
		if !slices.Contains(report.ByBackend[backend].FailedReads, want) {
			t.Fatalf("%s FailedReads = %v, want it to contain %q: the read stays advisory failed-only", backend, report.ByBackend[backend].FailedReads, want)
		}
	}
}

// (AC3) WRITE-contamination proof: a failed write with anchor-matching
// text — even one hostile enough to carry the matching callsite —
// provably does not count as usage. A successful anchor-matching write
// does not either.
func TestFailedWritesDoNotCountAsExemptionUsage(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		failedUsageWrite("neo4j", identityCallsite, identityText, true),
		failedUsageWrite("neo4j", identityCallsite, identityText, false),
	}}
	failures := ComputeStatementCoverage(identityManifest(), records).Failures()
	assertFailure(t, failures, "", CoverageStaleExemption, identityCallsite+" :: "+identityAnchor)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the stale exemption: writes never count as usage", failures)
	}
}

// Fail-closed default: a digest-less failed record predating the
// discriminator (no Kind, as in captures written before #7505) still
// reads stale. Unknown kind never counts as usage.
func TestUnknownKindFailedRecordsStayStale(t *testing.T) {
	record := failedUsageRead("neo4j", identityCallsite, identityText)
	record.Kind = ""
	records := map[string][]DifferentialRecord{"neo4j": {record}}
	failures := ComputeStatementCoverage(identityManifest(), records).Failures()
	assertFailure(t, failures, "", CoverageStaleExemption, identityCallsite+" :: "+identityAnchor)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the stale exemption: unknown kind fails closed", failures)
	}
}

// A fingerprinting failure carries no statement text, so it can match
// no anchor (usage fails closed) and must not render as a blank
// failed-read line either: unmarshalable params force the fingerprint
// error path at the read seam.
func TestFingerprintFailureReadStaysInvisible(t *testing.T) {
	record := captureRead(identityText, map[string]any{"fn": func() {}}, nil, errFailedUsageSentinel, "neo4j")
	if record.Kind != DifferentialRecordRead || !record.Failed || record.Digest != "" {
		t.Fatalf("fingerprint-failure record = %+v, want a failed digest-less read", record)
	}
	if record.Fingerprint.Statement != "" {
		t.Fatalf("fingerprint-failure record statement = %q, want empty", record.Fingerprint.Statement)
	}
	report := ComputeStatementCoverage(identityManifest(), map[string][]DifferentialRecord{"neo4j": {record}})
	if slices.Contains(report.ByBackend["neo4j"].FailedReads, "") {
		t.Fatalf("FailedReads = %v, want no blank entry for a text-less failure", report.ByBackend["neo4j"].FailedReads)
	}
}

// (AC1) the seams mark the kind: the read seam records reads (success
// and failure alike) and the write seam records writes, so the guard
// never depends on text, digest, or callsite inference.
func TestCaptureSeamsMarkRecordKind(t *testing.T) {
	if got := captureRead(identityText, nil, nil, nil, "neo4j"); got.Kind != DifferentialRecordRead {
		t.Fatalf("captureRead success Kind = %q, want %q", got.Kind, DifferentialRecordRead)
	}
	if got := captureRead(identityText, nil, nil, errFailedUsageSentinel, "neo4j"); got.Kind != DifferentialRecordRead || !got.Failed || got.Digest != "" {
		t.Fatalf("captureRead failure = %+v, want a failed digest-less read", got)
	}
	write := sourcecypher.Statement{Cypher: "MERGE (n:File {path: $path})", Parameters: map[string]any{"path": "a"}}
	if got := captureWrite(write, errFailedUsageSentinel, "neo4j"); got.Kind != DifferentialRecordWrite {
		t.Fatalf("captureWrite failure Kind = %q, want %q", got.Kind, DifferentialRecordWrite)
	}
	if got := captureWrite(write, nil, "neo4j"); got.Kind != DifferentialRecordWrite {
		t.Fatalf("captureWrite success Kind = %q, want %q", got.Kind, DifferentialRecordWrite)
	}
}
