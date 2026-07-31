// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"sort"
	"sync"
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
//  2. Worker B (fresh): a LARGER FencingToken (#5875 P1: this is now the
//     database-issued ordering value AWSCloudRuntimeDriftHandler.Handle would
//     have obtained from a LATER evidence read; this write-level test sets it
//     explicitly to drive the admission CAS directly, the same way the real
//     issuer would have produced it), one candidate for ARN X classified
//     image_version_drift. Run to completion (admission + insert + retire).
//  3. Worker A (stale): a SMALLER FencingToken (an earlier evidence read),
//     one candidate for the SAME ARN X classified orphaned_cloud_resource. A
//     different classification means a different fact_id, so the insert's own
//     conflict guard cannot help here -- only the insert-admission check can.
//
// Before #5848: A's insert lands unopposed (different fact_id, no conflict),
// and its retire is fenced at A's OWN older token so it cannot delete B's
// fresher row either -- leaving TWO rows for one ARN.
//
// After #5848: A's write is REJECTED by the admission check before it issues
// ANY statement, so exactly one row survives, from B's fresher evidence --
// regardless of which one's WriteAWSCloudRuntimeDriftFindings call this test
// happens to invoke first (#5875 P1 closed the shape where a host-clock-derived
// token could invert this ordering under cross-replica skew; this write-level
// test pins the admission CAS's OWN ordering contract directly against
// explicit tokens, independent of however a caller obtains them).
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

	// Step 2: worker B, fresh (larger token), image_version_drift. Runs to
	// completion first.
	freshWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-worker-b",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "fresh evidence read",
		EvidenceAsOf:  now,
		FencingToken:  200,
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

	// Step 3: worker A, a SMALLER (stale) token, lands AFTER B in call order.
	staleWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-worker-a",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "stale evidence read",
		EvidenceAsOf:  now.Add(-5 * time.Minute),
		FencingToken:  100,
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
// fencing token, and must be ADMITTED, not rejected as stale. #5875 P1 moved
// the token from a wall-clock derivation to a database-issued value, but this
// SQL-level guarantee is unchanged and still load-bearing: a caller that
// legitimately reuses one already-issued token across a retry of the
// identical write attempt must not be rejected merely for repeating it.
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
		FencingToken:  300,
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

// TestAWSCloudRuntimeDriftFencingTokenIssuerIssuesStrictlyIncreasingValuesLive
// is the #5875 P1 fix's own mechanism proof, superseding the round-1 P1's
// exact-tie test (TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive,
// now deleted): that test proved WHAT HAPPENED on an exact watermark tie
// because the OLD wall-clock token (evidenceAsOf.UnixMicro()) could produce
// one -- two independent passes stamping the identical microsecond. A
// Postgres sequence never returns the same value twice, so that exact
// scenario is now categorically impossible; a test built to characterize it
// would be asserting behavior for an unreachable input. This test instead
// proves the property that actually matters for #5875 P1: real,
// concurrent callers against the SAME shared Postgres sequence never collide
// and are always strictly ordered by issuance -- immune to any individual
// caller's clock, since the caller's clock is never consulted at all.
//
// Two parts: (1) sequential issuance is strictly increasing, proving the
// basic mechanism; (2) CONCURRENT issuance from many goroutines still yields
// a globally unique, strictly-ordered-by-issuance set (via nextval()'s own
// atomicity), the genuine multi-worker-replica shape #5875 P1 is about --
// this is the concurrency proof CLAUDE.md's evidence rules require for a
// change to a shared ordering primitive, not just a single-threaded
// correctness check.
func TestAWSCloudRuntimeDriftFencingTokenIssuerIssuesStrictlyIncreasingValuesLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)
	issuer := PostgresAWSCloudRuntimeDriftFencingTokenIssuer{DB: SQLDB{DB: sqlDB}}

	t.Run("sequential issuance is strictly increasing", func(t *testing.T) {
		const calls = 20
		var previous int64 = -1
		for i := 0; i < calls; i++ {
			token, err := issuer.NextAWSCloudRuntimeDriftFencingToken(ctx)
			if err != nil {
				t.Fatalf("call %d: NextAWSCloudRuntimeDriftFencingToken() error = %v", i, err)
			}
			if token <= previous {
				t.Fatalf("call %d: token = %d, want strictly greater than the previous token %d", i, token, previous)
			}
			previous = token
		}
	})

	t.Run("concurrent issuance never collides and stays strictly ordered by issuance", func(t *testing.T) {
		const goroutines = 10
		const perGoroutine = 20
		results := make(chan int64, goroutines*perGoroutine)
		errs := make(chan error, goroutines*perGoroutine)

		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perGoroutine; i++ {
					token, err := issuer.NextAWSCloudRuntimeDriftFencingToken(ctx)
					if err != nil {
						errs <- err
						continue
					}
					results <- token
				}
			}()
		}
		wg.Wait()
		close(results)
		close(errs)

		for err := range errs {
			t.Fatalf("concurrent NextAWSCloudRuntimeDriftFencingToken() error = %v", err)
		}

		seen := make(map[int64]struct{}, goroutines*perGoroutine)
		tokens := make([]int64, 0, goroutines*perGoroutine)
		for token := range results {
			if _, dup := seen[token]; dup {
				t.Fatalf(
					"token %d issued more than once across %d concurrent callers: the sequence must "+
						"never let two concurrent workers collide on the same fencing token",
					token, goroutines,
				)
			}
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
		if got, want := len(tokens), goroutines*perGoroutine; got != want {
			t.Fatalf("collected %d tokens, want %d (one per call)", got, want)
		}

		sort.Slice(tokens, func(i, j int) bool { return tokens[i] < tokens[j] })
		for i := 1; i < len(tokens); i++ {
			if tokens[i] <= tokens[i-1] {
				t.Fatalf(
					"sorted tokens are not strictly increasing at index %d: %d <= %d -- a Postgres "+
						"sequence must never repeat a value",
					i, tokens[i], tokens[i-1],
				)
			}
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
		FencingToken:  100,
		Candidates:    []model.Candidate{awsCloudRuntimeDriftCandidateFixture(arn, cloudruntime.FindingKindOrphanedCloudResource)},
		EvaluatedARNs: []string{arn},
	}
	if _, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, staleWrite); err != nil {
		t.Fatalf("first pass write error = %v, want nil", err)
	}
	if kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(kinds) != 1 || kinds[0] != string(cloudruntime.FindingKindOrphanedCloudResource) {
		t.Fatalf("after first pass, finding rows = %v, want exactly [orphaned_cloud_resource]", kinds)
	}

	// Second pass: a reopen replay with fresher evidence (state now active)
	// and a LARGER fencing token, reclassifying the SAME ARN.
	freshWrite := reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-reopen-replay",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "reopen replay after state activated",
		EvidenceAsOf:  freshEvidenceAsOf,
		FencingToken:  200,
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
