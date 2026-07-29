// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive
// is the mandatory #5848 regression: the exact interleaving the issue
// describes, reproduced deterministically (sequential, no timing dependency --
// the hazard does not require true concurrent overlap, only that the stale
// pass's evidence read predates the fresh pass's commit for the SAME (scope,
// generation)).
//
//  1. Seed one scope and one active generation.
//  2. Worker B (fresh): EvidenceAsOf = now, one candidate for ARN X classified
//     image_version_drift. Run to completion (admission + insert + retire).
//  3. Worker A (stale): EvidenceAsOf = now - 5m, one candidate for the SAME ARN
//     X classified orphaned_cloud_resource. A different classification means a
//     different fact_id, so the insert's own conflict guard cannot help here --
//     only the insert-admission check can.
//
// Before #5848: A's insert lands unopposed (different fact_id, no conflict),
// and its retire is fenced at A's OWN older token so it cannot delete B's
// fresher row either -- leaving TWO rows for one ARN.
//
// After #5848: A's write is REJECTED by the admission check before it issues
// ANY statement, so exactly one row survives, from B's fresher evidence.
func TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5848-interleave-%d", time.Now().UnixNano())
	scopeID := "aws:" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:" + suffix

	now := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)

	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}},
	}

	freshEvidenceAsOf := now
	staleEvidenceAsOf := now.Add(-5 * time.Minute)

	// Step 2: worker B, fresh evidence, image_version_drift. Runs to
	// completion first.
	freshWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-worker-b",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "fresh evidence read",
		EvidenceAsOf:  freshEvidenceAsOf,
		Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, cloudruntime.FindingKindImageVersionDrift)},
		EvaluatedARNs: []string{arn},
	}
	freshResult, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, freshWrite)
	if err != nil {
		t.Fatalf("worker B (fresh) write error = %v, want nil", err)
	}
	if freshResult.CanonicalWrites != 1 {
		t.Fatalf("worker B (fresh) CanonicalWrites = %d, want 1", freshResult.CanonicalWrites)
	}

	// Step 3: worker A, stale evidence read BEFORE B's write, lands AFTER it.
	staleWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-worker-a",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "stale evidence read",
		EvidenceAsOf:  staleEvidenceAsOf,
		Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, cloudruntime.FindingKindOrphanedCloudResource)},
		EvaluatedARNs: []string{arn},
	}
	_, err = writer.WriteAWSCloudRuntimeDriftFindings(ctx, staleWrite)
	if err == nil {
		t.Fatal("worker A (stale) write error = nil, want superseded rejection")
	}
	if !mustRetryable(t, err) {
		t.Fatalf("worker A (stale) write error = %v, want Retryable() == true", err)
	}
	if class := mustFailureClass(t, err); class != reducer.AWSCloudRuntimeDriftWriteSupersededFailureClass {
		t.Fatalf("worker A (stale) write FailureClass() = %q, want %q", class, reducer.AWSCloudRuntimeDriftWriteSupersededFailureClass)
	}

	// THE ASSERTION: exactly one row for the ARN, from the fresher evidence
	// read -- via a direct fact_records read AND via
	// AWSCloudRuntimeDriftFindingStore.ListActiveFindings.
	kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID)
	if len(kinds) != 1 {
		t.Fatalf(
			"fact_records finding_kind rows for one ARN = %v (count %d), want exactly 1: "+
				"a superseded pass must be rejected before it writes anything, not merely prevented from retiring",
			kinds, len(kinds),
		)
	}
	if kinds[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("surviving finding_kind = %q, want %q (the fresher evidence read)", kinds[0], cloudruntime.FindingKindImageVersionDrift)
	}

	store := NewAWSCloudRuntimeDriftFindingStore(SQLDB{DB: sqlDB})
	findings, err := store.ListActiveFindings(ctx, AWSCloudRuntimeDriftFindingFilter{ScopeID: scopeID})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ListActiveFindings() returned %d findings, want 1: %+v", len(findings), findings)
	}
	if findings[0].FindingKind != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("ListActiveFindings() finding_kind = %q, want %q", findings[0].FindingKind, cloudruntime.FindingKindImageVersionDrift)
	}
	if findings[0].ARN != arn {
		t.Fatalf("ListActiveFindings() arn = %q, want %q", findings[0].ARN, arn)
	}
}

// TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive pins the
// `<=` boundary (mirrors TestReducerFactBatchInsertAppliesEqualTokenRetryLive
// for #5847): a retry or redelivery of the SAME pass carries the SAME
// evidence-read watermark, and must be ADMITTED, not rejected as stale.
func TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5848-equal-token-%d", time.Now().UnixNano())
	scopeID := "aws:" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:" + suffix

	now := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)

	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}},
	}

	write := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-retry",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "retry of the same pass",
		EvidenceAsOf:  now,
		Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, cloudruntime.FindingKindOrphanedCloudResource)},
		EvaluatedARNs: []string{arn},
	}
	if _, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, write); err != nil {
		t.Fatalf("first write error = %v, want nil", err)
	}
	// Second call, identical watermark: must be admitted (idempotent retry),
	// not rejected.
	if _, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, write); err != nil {
		t.Fatalf("equal-token retry write error = %v, want nil (a `<` guard would reject its own retry)", err)
	}

	kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID)
	if len(kinds) != 1 {
		t.Fatalf("finding rows = %v, want exactly 1 (retry must upsert the same row)", kinds)
	}
}

// TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive is
// the P1 regression: fencingToken is a wall-clock LABEL
// (evidenceAsOf.UnixMicro()), not a read snapshot, so two genuinely
// independent passes (a live pass racing a maintenance-reopen replay, or a
// lease-theft duplicate claim) can tie at microsecond resolution while having
// read DIFFERENT evidence. On that tie, `stored <= EXCLUDED` is satisfied by
// equality (required for the equal-token retry case
// TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive pins), so
// BOTH transactions are admitted, and whichever commits SECOND wins: its
// retire deletes the first transaction's just-written row before inserting
// its own. This is last-committer-wins, not fresher-wins — the token alone
// cannot express which pass read evidence more recently once they tie.
//
// Proven by driving the SAME tied watermark through two DIFFERENT candidate
// sets (different classifications, so different fact_ids -- the
// reclassification shape, not the retry shape) in BOTH call orders. In
// EITHER order the pass called SECOND is the one left standing, which is
// exactly the property "last commit wins" predicts and "fresher wins" does
// not (neither candidate is chronologically fresher than the other; they
// share one watermark by construction).
func TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}},
	}

	runTiedPair := func(t *testing.T, firstKind, secondKind cloudruntime.FindingKind) string {
		t.Helper()

		now := time.Now().UTC()
		suffix := fmt.Sprintf("5848-p1-tie-%d", time.Now().UnixNano())
		scopeID := "aws:" + suffix
		generationID := "gen-" + suffix
		arn := "arn:aws:lambda:us-east-1:123456789012:function:" + suffix
		seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
		seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)

		// One tied watermark for both passes: they read DIFFERENT evidence
		// (different classifications) but stamp the IDENTICAL microsecond
		// token, exactly the shape a live pass racing a reopen replay -- or a
		// duplicate claim after lease theft -- can produce.
		tiedEvidenceAsOf := now

		firstWrite := reducer.AWSCloudRuntimeDriftWrite{
			IntentID:      "intent-tie-first",
			ScopeID:       scopeID,
			GenerationID:  generationID,
			SourceSystem:  "aws",
			Cause:         "tied pass, called first",
			EvidenceAsOf:  tiedEvidenceAsOf,
			Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, firstKind)},
			EvaluatedARNs: []string{arn},
		}
		if _, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, firstWrite); err != nil {
			t.Fatalf("first (called-first) write error = %v, want nil: an exact tie must still ADMIT, not reject", err)
		}

		secondWrite := reducer.AWSCloudRuntimeDriftWrite{
			IntentID:      "intent-tie-second",
			ScopeID:       scopeID,
			GenerationID:  generationID,
			SourceSystem:  "aws",
			Cause:         "tied pass, called second",
			EvidenceAsOf:  tiedEvidenceAsOf,
			Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, secondKind)},
			EvaluatedARNs: []string{arn},
		}
		if _, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, secondWrite); err != nil {
			t.Fatalf("second (called-second) write error = %v, want nil: an exact tie must still ADMIT, not reject", err)
		}

		kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID)
		if len(kinds) != 1 {
			t.Fatalf(
				"finding rows = %v, want exactly 1: an exact-tie admission must still leave exactly one "+
					"surviving row (the second pass's retire must clean up the first pass's row), not two "+
					"contradictory findings",
				kinds,
			)
		}
		return kinds[0]
	}

	t.Run("orphaned called first, image_version_drift called second", func(t *testing.T) {
		got := runTiedPair(t, cloudruntime.FindingKindOrphanedCloudResource, cloudruntime.FindingKindImageVersionDrift)
		if want := string(cloudruntime.FindingKindImageVersionDrift); got != want {
			t.Fatalf(
				"surviving finding_kind = %q, want %q: the pass called SECOND must win on an exact tie "+
					"(last-committer-wins), regardless of which classification it carries",
				got, want,
			)
		}
	})

	t.Run("image_version_drift called first, orphaned called second", func(t *testing.T) {
		got := runTiedPair(t, cloudruntime.FindingKindImageVersionDrift, cloudruntime.FindingKindOrphanedCloudResource)
		if want := string(cloudruntime.FindingKindOrphanedCloudResource); got != want {
			t.Fatalf(
				"surviving finding_kind = %q, want %q: reversing call order reverses the winner, which is "+
					"exactly what last-committer-wins predicts and fresher-wins (a property of the EVIDENCE, "+
					"not the call order) would not -- the two candidates are not chronologically distinguishable "+
					"by construction, sharing one tied watermark",
				got, want,
			)
		}
	})
}

// TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive
// proves the generation-authoritative retire specifically (#5848 item 3),
// the more common real-world shape: an OLDER pass ran first (nothing existed
// yet, so its stale classification was unconditionally admitted), and a LATER
// pass with fresher evidence reclassifies the same ARN -- a bootstrap
// maintenance reopen replaying a corrected read is exactly this shape. The
// reclassification mints a different fact_id, so without the retire the
// stale row from the first pass would sit alongside the new one forever.
func TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5848-retire-%d", time.Now().UnixNano())
	scopeID := "aws:" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:" + suffix

	now := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)

	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}},
	}

	olderEvidenceAsOf := now.Add(-10 * time.Minute)
	freshEvidenceAsOf := now

	// First pass: nothing exists yet for this scope/generation, so an older
	// watermark is still unconditionally admitted (a first pass, not a
	// collision) -- this is the pre-#5837-fix shape: state was not active yet,
	// so the domain durably wrote orphaned_cloud_resource as its best verdict.
	staleFactID := awsCloudRuntimeDriftFactID2(scopeID, generationID, arn, cloudruntime.FindingKindOrphanedCloudResource)
	staleWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-first-pass",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "state not active yet",
		EvidenceAsOf:  olderEvidenceAsOf,
		Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, cloudruntime.FindingKindOrphanedCloudResource)},
		EvaluatedARNs: []string{arn},
	}
	if _, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, staleWrite); err != nil {
		t.Fatalf("first pass write error = %v, want nil", err)
	}
	if kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(kinds) != 1 || kinds[0] != string(cloudruntime.FindingKindOrphanedCloudResource) {
		t.Fatalf("after first pass, finding rows = %v, want exactly [orphaned_cloud_resource]", kinds)
	}

	// Second pass: a reopen replay with fresher evidence (state now active),
	// reclassifying the SAME ARN.
	freshWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-reopen-replay",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "reopen replay after state activated",
		EvidenceAsOf:  freshEvidenceAsOf,
		Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, cloudruntime.FindingKindImageVersionDrift)},
		EvaluatedARNs: []string{arn},
	}
	freshResult, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, freshWrite)
	if err != nil {
		t.Fatalf("reopen replay write error = %v, want nil", err)
	}
	if freshResult.Retired != 1 {
		t.Fatalf("reopen replay Retired = %d, want 1 (the stale orphaned_cloud_resource row)", freshResult.Retired)
	}

	kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID)
	if len(kinds) != 1 {
		t.Fatalf(
			"finding rows after reclassification = %v (count %d), want exactly 1: "+
				"the stale orphaned_cloud_resource row must be retired, not left alongside the reclassified one",
			kinds, len(kinds),
		)
	}
	if kinds[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("surviving finding_kind = %q, want %q", kinds[0], cloudruntime.FindingKindImageVersionDrift)
	}

	// The stale fact_id itself must be gone, not merely outnumbered.
	var staleStillExists bool
	if err := sqlDB.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM fact_records WHERE fact_id = $1)`, staleFactID,
	).Scan(&staleStillExists); err != nil {
		t.Fatalf("check stale fact_id existence: %v", err)
	}
	if staleStillExists {
		t.Fatalf("stale fact_id %s still exists after retire", staleFactID)
	}
}

// awsCloudRuntimeDriftFactID2 recomputes the SAME fact_id the production
// writer derives (aws_cloud_runtime_drift_writer.go's awsCloudRuntimeDriftFactID
// / awsCloudRuntimeDriftIdentity, both unexported in package reducer) so this
// live test's cross-package "gone after retire" assertion checks the actual
// row the first pass wrote, not an arbitrary id that would trivially pass by
// never having existed. scopeID/generationID must match the write's own
// (trimmed) values; candidateID mirrors awsCloudRuntimeDriftCandidateFixture,
// the single fixture builder both this helper and the write agree on.
func awsCloudRuntimeDriftFactID2(scopeID, generationID, arn string, kind cloudruntime.FindingKind) string {
	identity := map[string]any{
		"scope_id":       scopeID,
		"generation_id":  generationID,
		"candidate_id":   "aws_cloud_runtime_drift:" + arn + ":" + string(kind),
		"arn":            arn,
		"finding_kind":   string(kind),
		"candidate_kind": rules.AWSCloudRuntimeDriftPackName,
	}
	return AWSCloudRuntimeDriftFindingFactKind + ":" + facts.StableID(AWSCloudRuntimeDriftFindingFactKind, identity)
}

// awsCloudRuntimeDriftCandidateFixture builds a minimal one-ARN admitted
// candidate carrying only the finding_kind evidence atom the writer's payload
// builder reads (awsCloudRuntimeFindingKind). Sufficient for the admission/
// retire proofs, which do not assert on the full evidence payload shape.
func awsCloudRuntimeDriftCandidateFixture(arn string, kind cloudruntime.FindingKind) model.Candidate {
	return model.Candidate{
		ID:             "aws_cloud_runtime_drift:" + arn + ":" + string(kind),
		Kind:           rules.AWSCloudRuntimeDriftPackName,
		CorrelationKey: arn,
		Confidence:     1,
		State:          model.CandidateStateAdmitted,
		Evidence: []model.EvidenceAtom{
			{
				EvidenceType: cloudruntime.EvidenceTypeFindingKind,
				Key:          "finding_kind",
				Value:        string(kind),
			},
		},
	}
}

// mustRetryable reports err.Retryable() through the reducer.RetryableError
// interface, failing the test if err does not implement it.
func mustRetryable(t *testing.T, err error) bool {
	t.Helper()
	var retryable reducer.RetryableError
	if !errors.As(err, &retryable) {
		t.Fatalf("error %v does not implement RetryableError", err)
	}
	return retryable.Retryable()
}

// mustFailureClass reads err's self-reported failure class through a local
// structural interface (the concrete error type is unexported in package
// reducer; errors.As only needs the METHOD to be exported, which
// FailureClass() is).
func mustFailureClass(t *testing.T, err error) string {
	t.Helper()
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("error %v does not self-classify a failure class", err)
	}
	return classified.FailureClass()
}
