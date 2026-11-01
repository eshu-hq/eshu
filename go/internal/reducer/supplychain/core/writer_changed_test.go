// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

func changedTestFinding(cveID string) SupplyChainImpactFinding {
	return SupplyChainImpactFinding{
		CVEID:           cveID,
		PackageID:       testImpactPackageID,
		PURL:            testImpactPURL,
		ObservedVersion: "1.2.3",
		RequestedRange:  "^1.2.0",
		MatchReason:     "npm_semver_affected_range",
		Status:          SupplyChainImpactAffectedExact,
		Confidence:      "exact",
		CVSSScore:       7.5,
		RepositoryID:    testImpactRepositoryID,
		CanonicalWrites: 1,
		EvidenceFactIDs: []string{"cve-1", "affected-1"},
	}
}

func changedTestWrite(findings ...SupplyChainImpactFinding) SupplyChainImpactWrite {
	return SupplyChainImpactWrite{
		FencingToken: 10,
		IntentID:     "intent-changed",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-changed",
		SourceSystem: "vulnerability_intelligence",
		Cause:        "vulnerability evidence observed",
		Findings:     findings,
	}
}

func changedTestWriter() (PostgresSupplyChainImpactWriter, fakeImpactBeginner) {
	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	return PostgresSupplyChainImpactWriter{DB: beginner, Now: func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) }}, beginner
}

// storedLike returns the row a previous identical pass would have left for
// the finding.
func storedLike(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) fakeStoredFinding {
	return fakeStoredFinding{
		factID:       supplyChainImpactFactID(write, finding),
		generationID: write.GenerationID,
		stableKey:    supplyChainImpactStableFactKey(write, finding),
		digest:       supplyChainImpactContentDigest(write, finding),
		cveID:        finding.CVEID,
		fencingToken: 5,
	}
}

// priorLike returns the row the scope's previous generation left for the same
// finding: a different fact id (the id embeds the generation), the same stable
// key, and the digest of that generation's own derivation.
func priorLike(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) fakeStoredFinding {
	prior := write
	prior.GenerationID = "generation-prior"
	return storedLike(prior, finding)
}

// TestWriteReportsOnlyTheCVEsWhoseRowsChanged proves the change signal the
// fan-out is bounded by: an unchanged row costs nothing, a row whose content
// differs, a new row, and a retracted row each name their CVE, and a row a
// fresher pass owns (higher fencing token) is not counted as retracted.
func TestWriteReportsOnlyTheCVEsWhoseRowsChanged(t *testing.T) {
	t.Parallel()

	unchanged, differs, fresh := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002"), changedTestFinding("CVE-2026-0003")
	write := changedTestWrite(unchanged, differs, fresh)
	writer, beginner := changedTestWriter()
	stale := storedLike(write, differs)
	stale.digest = "digest-of-an-older-derivation"
	beginner.state.stored = []fakeStoredFinding{
		storedLike(write, unchanged),
		stale,
		{factID: "finding-no-longer-derived", generationID: "generation-changed", stableKey: "k4", digest: "d", cveID: "CVE-2026-0004", fencingToken: 5},
		{factID: "finding-owned-by-a-fresher-pass", generationID: "generation-changed", stableKey: "k5", digest: "d", cveID: "CVE-2026-0005", fencingToken: 99},
	}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	want := []string{"CVE-2026-0002", "CVE-2026-0003", "CVE-2026-0004"}
	if !reflect.DeepEqual(result.ChangedCVEIDs, want) {
		t.Fatalf("ChangedCVEIDs = %v, want %v", result.ChangedCVEIDs, want)
	}
	if result.RecoveryCVEIDs != nil {
		t.Fatalf("RecoveryCVEIDs = %v, want nil without Recovery", result.RecoveryCVEIDs)
	}
}

// TestWriteOfIdenticalContentReportsNoChange is the idempotency proof: an
// identical rewrite (new intent id and cause, same findings) changes nothing.
func TestWriteOfIdenticalContentReportsNoChange(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	first := changedTestWrite(finding)
	second := changedTestWrite(finding)
	second.IntentID, second.Cause = "intent-later", "a different cause"
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{storedLike(first, finding)}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), second)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none for an identical rewrite", result.ChangedCVEIDs)
	}
}

// TestRevivedAndLegacyRowsCountAsChanged covers the two stored shapes whose
// digest cannot be trusted. A tombstoned row a pass derives again is absent from
// the read (it returns active rows only), so it looks new and counts as changed;
// a row written before the digest existed has none. Both are changed, so the
// first pass after a rollout heals copies written without a fan-out.
func TestRevivedAndLegacyRowsCountAsChanged(t *testing.T) {
	t.Parallel()

	revived, legacy := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002")
	write := changedTestWrite(revived, legacy)
	writer, beginner := changedTestWriter()
	noDigest := storedLike(write, legacy)
	noDigest.digest = ""
	beginner.state.stored = []fakeStoredFinding{noDigest}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if want := []string{"CVE-2026-0001", "CVE-2026-0002"}; !reflect.DeepEqual(result.ChangedCVEIDs, want) {
		t.Fatalf("ChangedCVEIDs = %v, want %v", result.ChangedCVEIDs, want)
	}
}

// TestPartialEvidenceRetractsNothingSoItReportsNoRetractedCVEs keeps the change
// signal honest for a pass that did not run the retraction.
func TestPartialEvidenceRetractsNothingSoItReportsNoRetractedCVEs(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	write := changedTestWrite(finding)
	write.PartialEvidence = true
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{
		storedLike(write, finding),
		{factID: "finding-not-reached", generationID: "generation-changed", stableKey: "k9", digest: "d", cveID: "CVE-2026-0009", fencingToken: 5},
	}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none: a partial pass retracts nothing", result.ChangedCVEIDs)
	}
}

// TestRecoveryWriteReportsEveryCVEOfTheScope proves the retry path: a rerun
// that changed nothing still names every CVE the scope's rows carry, the
// tombstoned ones included, so the fan-out it owes can be reached.
func TestRecoveryWriteReportsEveryCVEOfTheScope(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	write := changedTestWrite(finding)
	write.Recovery = true
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{storedLike(write, finding)}
	beginner.state.cveRows = []string{"CVE-2026-0001", "CVE-2026-0007"}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none for an identical rewrite", result.ChangedCVEIDs)
	}
	if want := []string{"CVE-2026-0001", "CVE-2026-0007"}; !reflect.DeepEqual(result.RecoveryCVEIDs, want) {
		t.Fatalf("RecoveryCVEIDs = %v, want %v", result.RecoveryCVEIDs, want)
	}
	// Without Recovery the extra read never runs.
	write.Recovery = false
	_, plain := changedTestWriter()
	plainWriter := PostgresSupplyChainImpactWriter{DB: plain, Now: writer.Now}
	if _, err := plainWriter.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatal(err)
	}
	for _, call := range plain.state.all {
		if call.Query == listSupplyChainImpactCVEIDsQuery {
			t.Fatal("the recovery CVE read ran without Recovery")
		}
	}
}

// TestChangedRowReadFailureFailsTheWrite keeps a failed read from silently
// reporting no change.
func TestChangedRowReadFailureFailsTheWrite(t *testing.T) {
	t.Parallel()

	writer, beginner := changedTestWriter()
	beginner.state.queryErr = errFakeImpactStatement
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), changedTestWrite(changedTestFinding("CVE-2026-0001"))); err == nil {
		t.Fatal("error = nil, want the changed-row read failure")
	}
	if beginner.state.commits != 0 || beginner.state.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, want a rolled-back write", beginner.state.commits, beginner.state.rollbacks)
	}
}

// TestContentDigestIgnoresPassFieldsAndSeesRealChanges pins the digest: the
// intent id and cause differ on every pass and must not count as a change, and
// the suppression state (an operator copy flipping between accepted_risk and
// advisory_retracted) and the retraction must.
func TestContentDigestIgnoresPassFieldsAndSeesRealChanges(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	base := changedTestWrite(finding)
	digest := supplyChainImpactContentDigest(base, finding)
	if digest == "" {
		t.Fatal("digest is empty")
	}
	other := base
	other.IntentID, other.Cause, other.FencingToken = "intent-other", "other cause", 99
	if got := supplyChainImpactContentDigest(other, finding); got != digest {
		t.Fatalf("digest moved with intent id, cause, or fencing token: %q != %q", got, digest)
	}
	retracted := finding
	retracted.Suppression = SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateAdvisoryRetracted, Reason: "advisory retracted by its source: nvd rejected CVE-2026-0001"}
	if got := supplyChainImpactContentDigest(base, retracted); got == digest {
		t.Fatal("digest did not move when the suppression state became advisory_retracted")
	}
	accepted := finding
	accepted.Suppression = SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateAcceptedRisk, Reason: "operator accepted the risk"}
	if supplyChainImpactContentDigest(base, accepted) == supplyChainImpactContentDigest(base, retracted) {
		t.Fatal("digest does not tell accepted_risk from advisory_retracted")
	}
	bumped := finding
	bumped.CVSSScore = 9.9
	if got := supplyChainImpactContentDigest(base, bumped); got == digest {
		t.Fatal("digest did not move with the score")
	}
}

// TestChangedCVEIDsNeverTrustsAnEmptyDigest pins the guard behind the stored
// digest comparison: a digest that failed to compute is empty on both sides, and
// two empty digests are not evidence that nothing changed.
func TestChangedCVEIDsNeverTrustsAnEmptyDigest(t *testing.T) {
	t.Parallel()

	rows := []changedRow{{factID: "fact-1", stableKey: "k1", digest: "", cveID: "CVE-2026-0001"}, {factID: "fact-2", stableKey: "k2", digest: "d2", cveID: "CVE-2026-0002"}}
	stored := []storedFinding{
		{factID: "fact-1", generationID: "g", stableKey: "k1", digest: "", cveID: "CVE-2026-0001", fencingToken: 1},
		{factID: "fact-2", generationID: "g", stableKey: "k2", digest: "d2", cveID: "CVE-2026-0002", fencingToken: 1},
	}
	got := changedCVEIDs(rows, stored, "g", 10, true)
	if want := []string{"CVE-2026-0001"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changedCVEIDs = %v, want %v: an empty digest is changed, a matching digest is not", got, want)
	}
}

// TestNewGenerationWithIdenticalContentReportsNoChange is the storm guard. A
// finding's fact id embeds its generation, so the first pass of every new
// generation finds none of its own rows. Judged against nothing, every row looks
// new and every holder of every CVE would be reopened on every rescan. The
// baseline for that first pass is the scope's previous generation, matched by
// stable key, so a rescan that derived the same findings (new fact ids, new
// evidence fact ids, a new generation id) reports no change.
func TestNewGenerationWithIdenticalContentReportsNoChange(t *testing.T) {
	t.Parallel()

	a, b := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002")
	prior := changedTestWrite(a, b)
	prior.GenerationID = "generation-prior"
	prior.Findings[0].EvidenceFactIDs = []string{"cve-gen1", "affected-gen1"}
	prior.Findings[1].EvidenceFactIDs = []string{"cve-gen1-b"}
	next := changedTestWrite(a, b)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{storedLike(prior, prior.Findings[0]), storedLike(prior, prior.Findings[1])}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), next)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none: the rescan derived the same findings", result.ChangedCVEIDs)
	}
}

// TestNewGenerationReportsChangedNewAndRemovedFindings is the other half: a
// first pass of a new generation still reports what differs from the previous
// generation, including a finding it no longer derives.
func TestNewGenerationReportsChangedNewAndRemovedFindings(t *testing.T) {
	t.Parallel()

	same, differs, added := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002"), changedTestFinding("CVE-2026-0003")
	gone := changedTestFinding("CVE-2026-0004")
	next := changedTestWrite(same, differs, added)
	priorDiffers := differs
	priorDiffers.CVSSScore = 4.0
	prior := changedTestWrite(same, priorDiffers, gone)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{
		priorLike(prior, same), priorLike(prior, priorDiffers), priorLike(prior, gone),
	}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), next)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if want := []string{"CVE-2026-0002", "CVE-2026-0003", "CVE-2026-0004"}; !reflect.DeepEqual(result.ChangedCVEIDs, want) {
		t.Fatalf("ChangedCVEIDs = %v, want %v (changed, new, removed)", result.ChangedCVEIDs, want)
	}
}

// TestRerunOfACurrentGenerationIgnoresThePriorGeneration keeps a rerun from
// re-reporting what the generation's first pass already reported: once the
// generation has rows of its own they are the baseline, and the previous
// generation is not consulted.
func TestRerunOfACurrentGenerationIgnoresThePriorGeneration(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	gone := changedTestFinding("CVE-2026-0004")
	write := changedTestWrite(finding)
	prior := changedTestWrite(finding, gone)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{storedLike(write, finding), priorLike(prior, finding), priorLike(prior, gone)}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none for an identical rerun", result.ChangedCVEIDs)
	}
}

// TestPartialNewGenerationReportsNoRemovals keeps the partial-evidence rule on
// the new-generation path: a bounded load that did not reach a finding is not
// evidence the finding is gone.
func TestPartialNewGenerationReportsNoRemovals(t *testing.T) {
	t.Parallel()

	kept, gone := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0004")
	write := changedTestWrite(kept)
	write.PartialEvidence = true
	prior := changedTestWrite(kept, gone)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{priorLike(prior, kept), priorLike(prior, gone)}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none: a partial pass proves no removal", result.ChangedCVEIDs)
	}
}

// TestContentDigestIgnoresGenerationAndSourceFactIds pins the two fields that
// differ on every rescan without the finding changing.
func TestContentDigestIgnoresGenerationAndSourceFactIds(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	base := changedTestWrite(finding)
	digest := supplyChainImpactContentDigest(base, finding)
	other := base
	other.GenerationID = "generation-other"
	rescanned := finding
	rescanned.EvidenceFactIDs = []string{"cve-new-id", "affected-new-id"}
	if got := supplyChainImpactContentDigest(other, rescanned); got != digest {
		t.Fatalf("digest moved with the generation id or the evidence fact ids: %q != %q", got, digest)
	}
}

// fakeOwedRecorder stands in for the store's recorder: it executes a statement on
// the write transaction, so the test can see where in the transaction it ran.
type fakeOwedRecorder struct {
	err   error
	calls int
}

func (r *fakeOwedRecorder) RecordFanOutOwed(ctx context.Context, exec factwrite.Execer) error {
	r.calls++
	if _, err := exec.ExecContext(ctx, "mark-owed"); err != nil {
		return err
	}
	return r.err
}

// TestOwedMarkerIsRecordedInsideTheWriteTransaction pins the atomicity the
// retry logic depends on: the marker statement runs after the upsert and before
// the commit, only for a commit that changed something, and a failure rolls the
// whole write back.
func TestOwedMarkerIsRecordedInsideTheWriteTransaction(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	write := changedTestWrite(finding)
	recorder := &fakeOwedRecorder{}
	write.Owed = recorder
	writer, beginner := changedTestWriter()
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 {
		t.Fatalf("recorder calls = %d, want 1 for a changed commit", recorder.calls)
	}
	upsert, mark, commits := -1, -1, beginner.state.commits
	for i, call := range beginner.state.all {
		switch call.Query {
		case "mark-owed":
			mark = i
		case factwrite.BatchInsertVersionedQuery:
			upsert = i
		}
	}
	if upsert < 0 || mark <= upsert || commits != 1 {
		t.Fatalf("upsert at %d, marker at %d, commits %d: the marker must follow the upsert inside the one transaction", upsert, mark, commits)
	}

	// An unchanged rewrite records nothing.
	again := changedTestWrite(finding)
	again.Owed = &fakeOwedRecorder{}
	writer2, beginner2 := changedTestWriter()
	beginner2.state.stored = []fakeStoredFinding{storedLike(again, finding)}
	if _, err := writer2.WriteSupplyChainImpactFindings(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if again.Owed.(*fakeOwedRecorder).calls != 0 {
		t.Fatal("the marker was recorded for a commit that changed nothing")
	}

	// A failing recorder (a lost lease) rolls the write back.
	failing := changedTestWrite(finding)
	failing.Owed = &fakeOwedRecorder{err: errors.New("lease lost")}
	writer3, beginner3 := changedTestWriter()
	if _, err := writer3.WriteSupplyChainImpactFindings(context.Background(), failing); err == nil {
		t.Fatal("error = nil, want the recorder failure")
	}
	if beginner3.state.commits != 0 || beginner3.state.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, want a rolled-back write", beginner3.state.commits, beginner3.state.rollbacks)
	}
}
