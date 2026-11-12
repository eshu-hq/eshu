// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"
	"testing"
	"time"
)

// This file pins the L4 re-key overhead contract (#10408, criterion 3.3): a
// pass that carries advisory-group member sets costs the same statements as
// an ordinary changed pass, and a pass whose groups did not change issues no
// capture statement at all. The member union rides the changed-set helpers in
// memory; it adds payload bytes, never statements.

// overheadQueries renders the statement stream of one write as its ordered
// query names, so two passes can be compared statement for statement.
func overheadQueries(t *testing.T, beginner fakeImpactBeginner) []string {
	t.Helper()
	out := make([]string, 0, len(beginner.state.all))
	for _, call := range beginner.state.all {
		out = append(out, call.Query)
	}
	return out
}

// TestRekeyPassIssuesNoNewStatement pins the statement parity: a re-key merge
// pass (member-carrying rows, one retired group) and an ordinary changed pass
// (same row count, no members) execute the same statements in the same order.
// A member-only refresh, and an unchanged rerun with members, capture one
// batch and zero batches respectively, exactly like their member-less twins.
func TestRekeyPassIssuesNoNewStatement(t *testing.T) {
	t.Parallel()

	// One changed row out of three: the same change shape, with and without
	// the merged member set on the changed row.
	ordinaryWrite := changedTestWrite(
		changedTestFinding("CVE-2026-0901"),
		changedTestFinding("CVE-2026-0902"),
		changedTestFinding("CVE-2026-0903"),
	)
	rekeyWrite := rekeyWrite("generation-changed",
		rekeyFinding("CVE-2026-0901", "CVE-2026-0901", "CVE-2026-0904"),
		rekeyFinding("CVE-2026-0902", "CVE-2026-0902"),
		rekeyFinding("CVE-2026-0903", "CVE-2026-0903"),
	)
	// Seed both beginners with the stored rows of the unchanged findings, so
	// only the first row is new in each arm.
	seedUnchanged := func(beginner fakeImpactBeginner, write SupplyChainImpactWrite, findings []SupplyChainImpactFinding) {
		beginner.state.stored = nil
		for _, finding := range findings[1:] {
			beginner.state.stored = append(beginner.state.stored, storedLike(write, finding))
		}
	}

	ordinaryWriter, ordinaryBeginner, ordinaryBatches, _ := captureWriterRecording()
	seedUnchanged(ordinaryBeginner, ordinaryWrite, ordinaryWrite.Findings)
	if _, err := ordinaryWriter.WriteSupplyChainImpactFindings(context.Background(), ordinaryWrite); err != nil {
		t.Fatalf("ordinary changed pass: %v", err)
	}
	rekeyWriter, rekeyBeginner, rekeyBatches, _ := captureWriterRecording()
	seedUnchanged(rekeyBeginner, rekeyWrite, rekeyWrite.Findings)
	if _, err := rekeyWriter.WriteSupplyChainImpactFindings(context.Background(), rekeyWrite); err != nil {
		t.Fatalf("re-key changed pass: %v", err)
	}

	ordinaryStream, rekeyStream := overheadQueries(t, ordinaryBeginner), overheadQueries(t, rekeyBeginner)
	if len(ordinaryStream) != len(rekeyStream) {
		t.Fatalf("statement counts differ: ordinary %d, re-key %d\nordinary: %q\nre-key: %q",
			len(ordinaryStream), len(rekeyStream), ordinaryStream, rekeyStream)
	}
	for i := range ordinaryStream {
		if ordinaryStream[i] != rekeyStream[i] {
			t.Fatalf("statement %d differs: ordinary %q, re-key %q", i, ordinaryStream[i], rekeyStream[i])
		}
	}
	if len(*ordinaryBatches) != 1 || len(*rekeyBatches) != 1 {
		t.Fatalf("capture batches: ordinary %d, re-key %d, want exactly one each", len(*ordinaryBatches), len(*rekeyBatches))
	}

	// No-group-change path: an unchanged rerun captures nothing and issues
	// the same stream with or without members.
	ordinaryRerun, ordinaryRerunBeginner, ordinaryRerunBatches, _ := captureWriterRecording()
	ordinaryRerunBeginner.state.stored = nil
	for _, finding := range ordinaryWrite.Findings {
		ordinaryRerunBeginner.state.stored = append(ordinaryRerunBeginner.state.stored, storedLike(ordinaryWrite, finding))
	}
	if _, err := ordinaryRerun.WriteSupplyChainImpactFindings(context.Background(), ordinaryWrite); err != nil {
		t.Fatalf("ordinary unchanged rerun: %v", err)
	}
	rekeyRerun, rekeyRerunBeginner, rekeyRerunBatches, _ := captureWriterRecording()
	rekeyRerunBeginner.state.stored = nil
	for _, finding := range rekeyWrite.Findings {
		stored := storedLike(rekeyWrite, finding)
		stored.members = append([]string(nil), finding.AdvisoryGroupMembers...)
		rekeyRerunBeginner.state.stored = append(rekeyRerunBeginner.state.stored, stored)
	}
	if _, err := rekeyRerun.WriteSupplyChainImpactFindings(context.Background(), rekeyWrite); err != nil {
		t.Fatalf("re-key unchanged rerun: %v", err)
	}
	if len(*ordinaryRerunBatches) != 0 || len(*rekeyRerunBatches) != 0 {
		t.Fatalf("unchanged rerun captured batches: ordinary %d, re-key %d, want zero both",
			len(*ordinaryRerunBatches), len(*rekeyRerunBatches))
	}
	ordinaryRerunStream, rekeyRerunStream := overheadQueries(t, ordinaryRerunBeginner), overheadQueries(t, rekeyRerunBeginner)
	if len(ordinaryRerunStream) != len(rekeyRerunStream) {
		t.Fatalf("unchanged-rerun statement counts differ: ordinary %d, re-key %d", len(ordinaryRerunStream), len(rekeyRerunStream))
	}
}

// TestRekeyPassOverheadIsMeasured reports the in-memory writer-core cost of a
// re-key pass against an ordinary changed pass with the same row count. It
// runs the production writer over the fake transaction surface, so the
// measured delta covers finding encode, digest, the member-union helpers,
// and the capture filter, but no Postgres round trip: it is the writer-core
// overhead, not an end-to-end claim.
func TestRekeyPassOverheadIsMeasured(t *testing.T) {
	t.Parallel()

	const rounds = 200
	ordinary := func() SupplyChainImpactWrite {
		return changedTestWrite(
			changedTestFinding("CVE-2026-0911"),
			changedTestFinding("CVE-2026-0912"),
			changedTestFinding("CVE-2026-0913"),
			changedTestFinding("CVE-2026-0914"),
		)
	}
	rekey := func() SupplyChainImpactWrite {
		return rekeyWrite("generation-overhead",
			rekeyFinding("CVE-2026-0911", "CVE-2026-0911", "CVE-2026-0915"),
			rekeyFinding("CVE-2026-0912", "CVE-2026-0912"),
			rekeyFinding("CVE-2026-0913", "CVE-2026-0913", "CVE-2026-0916"),
			rekeyFinding("CVE-2026-0914", "CVE-2026-0914"),
		)
	}
	run := func(build func() SupplyChainImpactWrite) []float64 {
		out := make([]float64, 0, rounds)
		for range rounds {
			write := build()
			writer, _ := changedTestWriter()
			start := time.Now()
			if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
				t.Fatalf("overhead pass: %v", err)
			}
			out = append(out, float64(time.Since(start).Nanoseconds()))
		}
		sort.Float64s(out)
		return out
	}
	ordinaryTimes, rekeyTimes := run(ordinary), run(rekey)
	median := func(xs []float64) float64 { return xs[len(xs)/2] }
	ordinaryMedian, rekeyMedian := median(ordinaryTimes), median(rekeyTimes)
	t.Logf("writer-core overhead rounds=%d rows=4 changed=4: ordinary median=%.1fus re-key median=%.1fus delta=%+.1f%%",
		rounds, ordinaryMedian/1000, rekeyMedian/1000, 100*(rekeyMedian-ordinaryMedian)/ordinaryMedian)
	if rekeyMedian > 2*ordinaryMedian {
		t.Fatalf("re-key median %.1fus exceeds twice the ordinary median %.1fus: the member union is not a bounded in-memory cost",
			rekeyMedian/1000, ordinaryMedian/1000)
	}
}
