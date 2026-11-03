// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// capturingImpactBeginner wraps the fake impact transaction with the winners
// dirty-log capture surface the Postgres transaction implements, and records
// every identifier batch the writer hands it (#9805).
type capturingImpactBeginner struct {
	fakeImpactBeginner
	batches  *[][]WinnersDirtyIdentifier
	recorded map[string]int64
}

func (b capturingImpactBeginner) BeginSupplyChainImpactTx(ctx context.Context) (SupplyChainImpactTx, error) {
	tx, err := b.fakeImpactBeginner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	return capturingImpactTx{SupplyChainImpactTx: tx, batches: b.batches, recorded: b.recorded}, nil
}

type capturingImpactTx struct {
	SupplyChainImpactTx
	batches  *[][]WinnersDirtyIdentifier
	recorded map[string]int64
}

func (t capturingImpactTx) CaptureWinnersDirtyFacts(_ context.Context, ids []WinnersDirtyIdentifier) (int64, error) {
	*t.batches = append(*t.batches, append([]WinnersDirtyIdentifier(nil), ids...))
	return int64(len(ids)), nil
}

// RecordWinnersCapture keeps the rows a writer statement reports by source, as
// the postgres adapter does with the capture counter.
func (t capturingImpactTx) RecordWinnersCapture(_ context.Context, source string, rows int64) {
	t.recorded[source] += rows
}

func captureWriter() (PostgresSupplyChainImpactWriter, fakeImpactBeginner, *[][]WinnersDirtyIdentifier) {
	writer, beginner, batches, _ := captureWriterRecording()
	return writer, beginner, batches
}

func captureWriterRecording() (PostgresSupplyChainImpactWriter, fakeImpactBeginner, *[][]WinnersDirtyIdentifier, map[string]int64) {
	writer, beginner := changedTestWriter()
	batches := &[][]WinnersDirtyIdentifier{}
	recorded := map[string]int64{}
	writer.DB = capturingImpactBeginner{fakeImpactBeginner: beginner, batches: batches, recorded: recorded}
	return writer, beginner, batches, recorded
}

// TestRetractionCountsItsCaptureRowsAtCaptureTime pins the writer-side counter
// for source fact_retract: the writer reports the retract statement's
// affected-row count, which is the number of dirty-log rows its INSERT
// appended, through the capturer in the same transaction. A pass that retracts
// nothing reports nothing.
func TestRetractionCountsItsCaptureRowsAtCaptureTime(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	write := changedTestWrite(finding)
	writer, beginner, _, recorded := captureWriterRecording()
	beginner.state.rowsAffected = map[string]int64{retractSupersededSupplyChainImpactFindingsQuery: 3}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.FactsRetracted != 3 || recorded["fact_retract"] != 3 {
		t.Fatalf("FactsRetracted=%d recorded=%v, want fact_retract = 3 = the retraction's affected rows", result.FactsRetracted, recorded)
	}
}

func capturedIdentifier(finding SupplyChainImpactFinding) WinnersDirtyIdentifier {
	c0 := finding.CVEID
	if c0 == "" {
		c0 = finding.AdvisoryID
	}
	return WinnersDirtyIdentifier{
		PackageID:     finding.PackageID,
		RepositoryID:  finding.RepositoryID,
		SubjectDigest: finding.SubjectDigest,
		C0:            c0,
		AdvisoryID:    finding.AdvisoryID,
	}
}

// TestWriteCapturesWinnerIdentifiersOnlyForNewOrChangedRows proves the capture
// filter of decision 1 of the #9805 ruling. A row is captured when it is new
// (no own-generation stored row for its fact id), or its content digest differs
// from the stored own-generation digest. A rerun that rewrites a row to the
// same digest captures nothing. The first pass of a new generation captures
// every row, even one whose stable key and digest match the previous
// generation, because the new fact id makes the winner a different row.
func TestWriteCapturesWinnerIdentifiersOnlyForNewOrChangedRows(t *testing.T) {
	t.Parallel()

	same, differs, fresh := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002"), changedTestFinding("CVE-2026-0003")
	write := changedTestWrite(same, differs, fresh)

	t.Run("rerun captures only the changed and the new row", func(t *testing.T) {
		t.Parallel()
		writer, beginner, batches := captureWriter()
		changed := storedLike(write, differs)
		changed.digest = "a-digest-from-an-older-pass"
		beginner.state.stored = []fakeStoredFinding{storedLike(write, same), changed}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		want := [][]WinnersDirtyIdentifier{{capturedIdentifier(differs), capturedIdentifier(fresh)}}
		if !reflect.DeepEqual(*batches, want) {
			t.Fatalf("captured = %+v, want %+v", *batches, want)
		}
	})

	t.Run("an unchanged rerun issues no capture", func(t *testing.T) {
		t.Parallel()
		writer, beginner, batches := captureWriter()
		beginner.state.stored = []fakeStoredFinding{storedLike(write, same), storedLike(write, differs), storedLike(write, fresh)}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		if len(*batches) != 0 {
			t.Fatalf("an unchanged rerun captured %+v, want no capture statement", *batches)
		}
	})

	t.Run("a first pass of a new generation captures every row", func(t *testing.T) {
		t.Parallel()
		writer, beginner, batches := captureWriter()
		beginner.state.stored = []fakeStoredFinding{priorLike(write, same), priorLike(write, differs), priorLike(write, fresh)}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		// The three findings share one identifier except for the CVE id.
		want := [][]WinnersDirtyIdentifier{{capturedIdentifier(same), capturedIdentifier(differs), capturedIdentifier(fresh)}}
		if !reflect.DeepEqual(*batches, want) {
			t.Fatalf("captured = %+v, want every row (a new generation mints new fact ids)", *batches)
		}
	})

	t.Run("a stored row with no digest counts as changed", func(t *testing.T) {
		t.Parallel()
		writer, beginner, batches := captureWriter()
		blank := storedLike(write, same)
		blank.digest = ""
		beginner.state.stored = []fakeStoredFinding{blank, storedLike(write, differs), storedLike(write, fresh)}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		want := [][]WinnersDirtyIdentifier{{capturedIdentifier(same)}}
		if !reflect.DeepEqual(*batches, want) {
			t.Fatalf("captured = %+v, want %+v", *batches, want)
		}
	})
}

// TestWriteCapturesAdvisoryOnlyFindingsAndDeduplicates proves the identifier
// shape: the first key component falls back to the advisory id when the CVE id
// is empty, and several findings that share one identifier capture it once.
func TestWriteCapturesAdvisoryOnlyFindingsAndDeduplicates(t *testing.T) {
	t.Parallel()

	advisory := changedTestFinding("")
	advisory.AdvisoryID = "GHSA-aaaa-bbbb-cccc"
	twinA, twinB := changedTestFinding("CVE-2026-0100"), changedTestFinding("CVE-2026-0100")
	twinB.ObservedVersion = "2.0.0"
	write := changedTestWrite(advisory, twinA, twinB)

	writer, _, batches := captureWriter()
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	want := [][]WinnersDirtyIdentifier{{
		{PackageID: advisory.PackageID, RepositoryID: advisory.RepositoryID, C0: "GHSA-aaaa-bbbb-cccc", AdvisoryID: "GHSA-aaaa-bbbb-cccc"},
		capturedIdentifier(twinA),
	}}
	if !reflect.DeepEqual(*batches, want) {
		t.Fatalf("captured = %+v, want %+v", *batches, want)
	}
}

// TestRetractionCapturesRetractedFindingIdentifiers pins that the retract
// statement itself appends one dirty-log row per tombstoned finding, so no Go
// change is needed to name a retracted finding's key (ruling decision 1).
func TestRetractionCapturesRetractedFindingIdentifiers(t *testing.T) {
	t.Parallel()

	query := retractSupersededSupplyChainImpactFindingsQuery
	for _, want := range []string{
		"UPDATE fact_records",
		"RETURNING",
		"INSERT INTO supply_chain_impact_winners_dirty_log",
		"'fact_retract'",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("retract statement does not contain %q:\n%s", want, query)
		}
	}
}
