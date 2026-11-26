// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
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

// The wrapper embeds the transaction interface, which hides the supports maintainer
// the writer's write step requires; forward it to the wrapped fake.
func (t capturingImpactTx) UpsertImpactSupportRows(ctx context.Context, rows []factwrite.VersionedRow) (SupplyChainImpactSupportsWrite, error) {
	return t.SupplyChainImpactTx.(SupplyChainImpactSupportsMaintainer).UpsertImpactSupportRows(ctx, rows)
}

func (t capturingImpactTx) RecordRetractedImpactSupports(ctx context.Context, rows int64) {
	t.SupplyChainImpactTx.(SupplyChainImpactSupportsMaintainer).RecordRetractedImpactSupports(ctx, rows)
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
// for source fact_retract: the writer reports the retract statement's marked
// count (distinct dirty-log keys touched) through the capturer in the same
// transaction, while FactsRetracted keeps the tombstoned rows. A pass that
// retracts nothing reports nothing.
func TestRetractionCountsItsCaptureRowsAtCaptureTime(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	write := changedTestWrite(finding)
	writer, beginner, _, recorded := captureWriterRecording()
	beginner.state.retractCounts = map[string][2]int64{retractSupersededSupplyChainImpactFindingsQuery: {3, 2}}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.FactsRetracted != 3 || recorded["fact_retract"] != 2 {
		t.Fatalf("FactsRetracted=%d recorded=%v, want tombstoned 3 with fact_retract = 2 marked keys", result.FactsRetracted, recorded)
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

// TestWriteStagesOneDirtyRowWhenOnePassAddsAndRetiresTheSameKey pins the
// #11411 emission dedup: one writer pass stages each changed key once. When
// the pass adds rows of a key under new fact ids and retires stored
// same-generation rows of that key, the retraction's own staging insert names
// the key, so the upsert capture must not name it again: the same triple
// twice in one batch yields one dirty row, not one per emission statement.
func TestWriteStagesOneDirtyRowWhenOnePassAddsAndRetiresTheSameKey(t *testing.T) {
	t.Parallel()

	newTwins := func() (retired, addedA, addedB SupplyChainImpactFinding) {
		retired = changedTestFinding("CVE-2026-0400")
		addedA = changedTestFinding("CVE-2026-0400")
		addedB = changedTestFinding("CVE-2026-0400")
		addedA.ObservedVersion = "2.0.0"
		addedB.ObservedVersion = "3.0.0"
		return retired, addedA, addedB
	}

	t.Run("retired key leaves the upsert capture", func(t *testing.T) {
		t.Parallel()
		retired, addedA, addedB := newTwins()
		write := changedTestWrite(addedA, addedB)
		writer, beginner, batches := captureWriter()
		beginner.state.stored = []fakeStoredFinding{storedLike(write, retired)}
		// Production tombstones the retired row and stages its key, so the
		// canned counts report one tombstoned row and one marked key.
		beginner.state.retractCounts = map[string][2]int64{retractSupersededSupplyChainImpactFindingsQuery: {1, 1}}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		if len(*batches) != 0 {
			t.Fatalf("captured = %+v, want no upsert capture: the retraction stages the retired key", *batches)
		}
	})

	t.Run("row a fresher pass owns keeps the capture", func(t *testing.T) {
		t.Parallel()
		retired, addedA, addedB := newTwins()
		write := changedTestWrite(addedA, addedB)
		writer, beginner, batches := captureWriter()
		owned := storedLike(write, retired)
		owned.fencingToken = 99
		beginner.state.stored = []fakeStoredFinding{owned}
		// The fencing guard keeps the fresher row, so production retracts
		// nothing and stages nothing.
		beginner.state.retractCounts = map[string][2]int64{retractSupersededSupplyChainImpactFindingsQuery: {0, 0}}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		if len(*batches) != 1 || len((*batches)[0]) != 1 {
			t.Fatalf("captured = %+v, want one batch with the added key: no row is retired", *batches)
		}
	})

	t.Run("partial evidence pass keeps the capture", func(t *testing.T) {
		t.Parallel()
		retired, addedA, addedB := newTwins()
		write := changedTestWrite(addedA, addedB)
		write.PartialEvidence = true
		writer, beginner, batches := captureWriter()
		beginner.state.stored = []fakeStoredFinding{storedLike(write, retired)}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		if len(*batches) != 1 || len((*batches)[0]) != 1 {
			t.Fatalf("captured = %+v, want one batch with the added key: a partial pass retracts nothing", *batches)
		}
	})

	t.Run("scoped pass covering the key leaves the capture", func(t *testing.T) {
		t.Parallel()
		retired, addedA, addedB := newTwins()
		write := changedTestWrite(addedA, addedB)
		write.ScopedCVEIDs = []string{"CVE-2026-0400"}
		writer, beginner, batches := captureWriter()
		beginner.state.stored = []fakeStoredFinding{storedLike(write, retired)}
		beginner.state.retractCounts = map[string][2]int64{retractSupersededSupplyChainImpactFindingsScopedQuery: {1, 1}}
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		if len(*batches) != 0 {
			t.Fatalf("captured = %+v, want no upsert capture: the scoped retraction stages the retired key", *batches)
		}
	})
}

// TestRetractionCapturesRetractedFindingIdentifiers pins that the retract
// statement itself stages one row per deleted support's key (#10837 direction C,
// moved to supports by #10880 PR-C2 i), so no Go change is needed to name a
// retracted finding's key: the DISTINCT collapses twins to one staged mark, and
// the final SELECT reports the CTE-based deleted and marked counts. There is no ORDER BY (no locks to
// take in order) and no ON CONFLICT (no unique index on the hot path): the
// tick collapse restores the version math.
func TestRetractionCapturesRetractedFindingIdentifiers(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"replace-set": retractSupersededSupplyChainImpactFindingsQuery,
		"scoped":      retractSupersededSupplyChainImpactFindingsScopedQuery,
	} {
		for _, want := range []string{
			"DELETE FROM supply_chain_impact_finding_supports",
			"RETURNING",
			"SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id",
			"INSERT INTO supply_chain_impact_winners_dirty_staging",
			"'fact_retract'",
			"(SELECT count(*) FROM retracted) AS deleted",
			"(SELECT count(*) FROM upserted) AS marked",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("%s retract statement does not contain %q:\n%s", name, want, query)
			}
		}
		for _, banned := range []string{
			"supply_chain_impact_winners_dirty_log",
			"ON CONFLICT",
			"dirty_version",
			"fact_records",
		} {
			if strings.Contains(query, banned) {
				t.Errorf("%s retract statement contains %q, the hot path it must avoid:\n%s", name, banned, query)
			}
		}
	}
}

// TestScopedRetractionFiltersOnTheCVEIDColumn pins the scoped variant of the
// supports retraction (#10880 PR-C2 i): it deletes only inside the stamped CVE set
// ($4) and filters on the support's cve_id column, not on a payload expression, so
// the planner can use the column's index; the replace-set variant has no such
// filter and takes no fourth argument.
func TestScopedRetractionFiltersOnTheCVEIDColumn(t *testing.T) {
	t.Parallel()

	if !strings.Contains(retractSupersededSupplyChainImpactFindingsScopedQuery, "AND cve_id = ANY($4::text[])") {
		t.Fatalf("the scoped retraction does not filter on the cve_id column:\n%s", retractSupersededSupplyChainImpactFindingsScopedQuery)
	}
	if strings.Contains(retractSupersededSupplyChainImpactFindingsScopedQuery, "payload->>'cve_id'") {
		t.Fatal("the scoped retraction still filters on the payload expression")
	}
	if strings.Contains(retractSupersededSupplyChainImpactFindingsQuery, "$4") {
		t.Fatal("the replace-set retraction names $4: it takes no CVE set")
	}
}
