// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Per-repo-wait narrowed-path differential tests (#10877).
//
// These replay the per_repo_wait_accuracy_baseline_test seeds through the
// NARROWED path (per-item read-set waits keyed on the producing scopes) and
// require exact fingerprint equality with the global-path clean runs wherever
// the design says the outcomes agree. RED-first: the restricted loader
// interfaces and the narrowing fields they reference do not exist yet.
//
// Shape note: the store computes the blocking scope set inside the same
// statement snapshot as the restricted verdict and the rows it admits, so a
// holder that activates mid-read cannot slip between a separate verdict and
// the rows. The stub scripts the blocking set per call; the deferral error
// must carry exactly the set the verdict ran under, which is what the parked
// wait keys on.

// stubRestrictedCorpusFencedLoader serves the narrowed fused read: rows, the
// verdict-time blocking set, and the verdict, scripted per test.
type stubRestrictedCorpusFencedLoader struct {
	repoResolved []relationships.ResolvedRelationship
	blocking     []string
	complete     bool
	fingerprint  string
	gotRepoIDs   [][]string
	calls        int
}

func (s *stubRestrictedCorpusFencedLoader) GetResolvedRelationships(
	_ context.Context,
	_ string,
) ([]relationships.ResolvedRelationship, error) {
	return nil, nil
}

func (s *stubRestrictedCorpusFencedLoader) GetResolvedRelationshipsForRepos(
	_ context.Context,
	_ []string,
) ([]relationships.ResolvedRelationship, error) {
	return nil, nil
}

func (s *stubRestrictedCorpusFencedLoader) GetResolvedRelationshipsForReposWithRestrictedFence(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, error) {
	s.calls++
	s.gotRepoIDs = append(s.gotRepoIDs, append([]string(nil), repoIDs...))
	return s.repoResolved, append([]string(nil), s.blocking...), s.complete, nil
}

func (s *stubRestrictedCorpusFencedLoader) GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, reducercontract.ResolvedReadFingerprint, error) {
	s.calls++
	s.gotRepoIDs = append(s.gotRepoIDs, append([]string(nil), repoIDs...))
	return s.repoResolved, append([]string(nil), s.blocking...), s.complete, reducercontract.ResolvedReadFingerprint{
		RepoIDs: append([]string(nil), repoIDs...),
		Value:   s.fingerprint,
	}, nil
}

// loadNarrowWorkloadProjectionScopeInputsForTest builds the production scope
// loader over a scripted restricted stub: the same LoadWorkloadProjectionScopeInputs
// the narrow path runs, with the restricted read engaged by the stub's
// interface.
func loadNarrowWorkloadProjectionScopeInputsForTest(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
) (WorkloadProjectionInputs, error) {
	l := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:         loader,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		IncompleteScopesLookup: func(context.Context) ([]string, error) { return nil, nil },
	}
	return l.LoadWorkloadProjectionScopeInputs(ctx, dockerfileLoaderIntent())
}

// runPerRepoWaitNarrowWorkloadSeed runs the Dockerfile workload seed through
// the narrowed path and captures the same outcome the global baseline pins.
func runPerRepoWaitNarrowWorkloadSeed(
	ctx context.Context,
	loader *stubRestrictedCorpusFencedLoader,
) (perRepoWaitWorkloadOutcome, error) {
	inputs, err := loadNarrowWorkloadProjectionScopeInputsForTest(ctx, loader)
	outcome := perRepoWaitWorkloadOutcome{fencedCalls: loader.calls}
	var deferral workloadMaterializationResolutionNotReadyError
	switch {
	case err == nil:
		outcome.scopeAdmitted = len(inputs.ScopeCandidates)
		outcome.narrowed = len(inputs.Candidates)
		for _, candidate := range inputs.ScopeCandidates {
			outcome.deploymentIDs = append(outcome.deploymentIDs, candidate.DeploymentRepoIDs...)
		}
		if len(inputs.ScopeCandidates) > 0 {
			outcome.fencedRepoIDs = workloadCandidateRepoIDs(inputs.ScopeCandidates)
		}
	case errors.As(err, &deferral):
		outcome.deferred = deferral.FailureClass()
		outcome.holdingScopes = append([]string(nil), deferral.holdingScopeIDs...)
	default:
		return outcome, err
	}
	return outcome, nil
}

func TestPerRepoWaitNarrowWorkloadShouldMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deferredLoader := &stubRestrictedCorpusFencedLoader{blocking: []string{"scope-deploy"}}
	deferred, err := runPerRepoWaitNarrowWorkloadSeed(ctx, deferredLoader)
	if err != nil {
		t.Fatalf("narrow first attempt error = %v, want a resolution-not-ready deferral", err)
	}
	if deferred.deferred != WorkloadMaterializationResolutionNotReadyFailureClass {
		t.Fatalf("narrow first attempt fingerprint = %q, want deferred=%q",
			deferred.fingerprint(), WorkloadMaterializationResolutionNotReadyFailureClass)
	}
	if len(deferredLoader.gotRepoIDs) != 1 || !slices.Equal(deferredLoader.gotRepoIDs[0], []string{"repo-service"}) {
		t.Fatalf("narrow read repo ids = %v, want [[repo-service]]", deferredLoader.gotRepoIDs)
	}
	if !slices.Equal(deferred.holdingScopes, []string{"scope-deploy"}) {
		t.Fatalf("narrow holding scopes = %v, want [scope-deploy]", deferred.holdingScopes)
	}

	wokenLoader := &stubRestrictedCorpusFencedLoader{
		repoResolved: []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()},
		complete:     true,
		fingerprint:  "fp-woken",
	}
	woken, err := runPerRepoWaitNarrowWorkloadSeed(ctx, wokenLoader)
	if err != nil {
		t.Fatalf("narrow retry error = %v, want nil once the foreign scope activates", err)
	}
	clean, _, err := runPerRepoWaitWorkloadSeed(ctx,
		[]relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}, true, nil)
	if err != nil {
		t.Fatalf("global clean run error = %v, want nil", err)
	}
	if woken.fingerprint() != clean.fingerprint() {
		t.Fatalf("narrow retry fingerprint = %q, global clean = %q: the narrowed retry must converge to the clean global run",
			woken.fingerprint(), clean.fingerprint())
	}
}

func TestPerRepoWaitNarrowWorkloadUnrelatedHolderProceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// The holder set is non-empty globally, but the restricted verdict proves
	// no producer for the seed read set: the narrowed run must succeed with
	// the clean-run fingerprint instead of deferring.
	loader := &stubRestrictedCorpusFencedLoader{
		repoResolved: []relationships.ResolvedRelationship{perRepoWaitUnrelatedRow()},
		complete:     true,
		fingerprint:  "fp-clean-unrelated",
	}
	outcome, err := runPerRepoWaitNarrowWorkloadSeed(ctx, loader)
	if err != nil {
		t.Fatalf("narrow seed error = %v, want nil: no blocking scope waits on this read set", err)
	}
	clean, _, err := runPerRepoWaitWorkloadSeed(ctx,
		[]relationships.ResolvedRelationship{perRepoWaitUnrelatedRow()}, true, nil)
	if err != nil {
		t.Fatalf("global clean run error = %v, want nil", err)
	}
	if outcome.fingerprint() != clean.fingerprint() {
		t.Fatalf("narrow fingerprint = %q, global clean = %q", outcome.fingerprint(), clean.fingerprint())
	}
}

func TestPerRepoWaitNarrowWorkloadAbsenceRecordsFingerprint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	absentLoader := &stubRestrictedCorpusFencedLoader{complete: true, fingerprint: "fp-absent"}
	absent, err := runPerRepoWaitNarrowWorkloadSeed(ctx, absentLoader)
	if err != nil {
		t.Fatalf("narrow absence run error = %v, want nil", err)
	}
	if absent.scopeAdmitted != 0 {
		t.Fatalf("narrow absence scope admitted = %d, want 0", absent.scopeAdmitted)
	}

	arrivedLoader := &stubRestrictedCorpusFencedLoader{
		repoResolved: []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()},
		complete:     true,
		fingerprint:  "fp-arrived",
	}
	arrived, err := runPerRepoWaitNarrowWorkloadSeed(ctx, arrivedLoader)
	if err != nil {
		t.Fatalf("narrow late-arrival error = %v, want nil", err)
	}
	if !slices.Contains(arrived.deploymentIDs, "repo-deploy") {
		t.Fatalf("narrow late-arrival deployment ids = %v, want [repo-deploy]", arrived.deploymentIDs)
	}
	clean, _, err := runPerRepoWaitWorkloadSeed(ctx,
		[]relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}, true, nil)
	if err != nil {
		t.Fatalf("global clean run error = %v, want nil", err)
	}
	if arrived.fingerprint() != clean.fingerprint() {
		t.Fatalf("narrow late-arrival fingerprint = %q, global clean = %q", arrived.fingerprint(), clean.fingerprint())
	}
}

// runPerRepoWaitNarrowDeployableUnitSeed runs the Dockerfile DU seed through
// the narrowed path and captures the same outcome the global baseline pins.
func runPerRepoWaitNarrowDeployableUnitSeed(
	ctx context.Context,
	loader *stubRestrictedCorpusFencedLoader,
) (perRepoWaitDeployableUnitOutcome, error) {
	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         loader,
		PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:             writer,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		IncompleteScopesLookup: func(context.Context) ([]string, error) { return nil, nil },
	}
	result, err := handler.Handle(ctx, deployableUnitIntent("repo:repo-edge-api"))
	outcome := perRepoWaitDeployableUnitOutcome{
		retractCalls: len(writer.retractCalls),
		writeCalls:   len(writer.writeCalls),
	}
	var deferral deployableUnitCorrelationResolutionNotReadyError
	switch {
	case err == nil:
		outcome.status = string(result.Status)
		outcome.evidenceSummary = result.EvidenceSummary
		outcome.canonicalWrites = result.CanonicalWrites
	case errors.As(err, &deferral):
		outcome.deferred = deferral.FailureClass()
		outcome.holdingScopes = append([]string(nil), deferral.holdingScopeIDs...)
	default:
		return outcome, err
	}
	return outcome, nil
}

func TestPerRepoWaitNarrowDeployableUnitShouldMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deferred, err := runPerRepoWaitNarrowDeployableUnitSeed(ctx,
		&stubRestrictedCorpusFencedLoader{blocking: []string{"scope-deploy"}})
	if err != nil {
		t.Fatalf("narrow first attempt error = %v, want a resolution-not-ready deferral", err)
	}
	if deferred.deferred != DeployableUnitCorrelationResolutionNotReadyFailureClass {
		t.Fatalf("narrow first attempt fingerprint = %q, want deferred=%q",
			deferred.fingerprint(), DeployableUnitCorrelationResolutionNotReadyFailureClass)
	}
	if !slices.Equal(deferred.holdingScopes, []string{"scope-deploy"}) {
		t.Fatalf("narrow holding scopes = %v, want [scope-deploy]", deferred.holdingScopes)
	}
	if deferred.retractCalls != 0 || deferred.writeCalls != 0 {
		t.Fatalf("narrow deferral edge calls = retract:%d write:%d, want 0/0",
			deferred.retractCalls, deferred.writeCalls)
	}

	foreign := []relationships.ResolvedRelationship{perRepoWaitDeployableUnitForeignRow()}
	woken, err := runPerRepoWaitNarrowDeployableUnitSeed(ctx,
		&stubRestrictedCorpusFencedLoader{repoResolved: foreign, complete: true, fingerprint: "fp-du-woken"})
	if err != nil {
		t.Fatalf("narrow retry error = %v, want nil once the foreign scope activates", err)
	}
	clean, err := runPerRepoWaitDeployableUnitSeed(ctx, foreign, true, nil)
	if err != nil {
		t.Fatalf("global clean run error = %v, want nil", err)
	}
	if woken.fingerprint() != clean.fingerprint() {
		t.Fatalf("narrow retry fingerprint = %q, global clean = %q", woken.fingerprint(), clean.fingerprint())
	}
	if woken.canonicalWrites != 1 || woken.writeCalls != 1 || woken.retractCalls != 1 {
		t.Fatalf("narrow retry writes = canonical:%d retract:%d write:%d, want 1/1/1",
			woken.canonicalWrites, woken.retractCalls, woken.writeCalls)
	}
}

func TestPerRepoWaitNarrowDeployableUnitUnrelatedHolderProceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	outcome, err := runPerRepoWaitNarrowDeployableUnitSeed(ctx,
		&stubRestrictedCorpusFencedLoader{
			repoResolved: []relationships.ResolvedRelationship{perRepoWaitUnrelatedRow()},
			complete:     true,
			fingerprint:  "fp-du-clean",
		})
	if err != nil {
		t.Fatalf("narrow seed error = %v, want nil: no blocking scope waits on this read set", err)
	}
	clean, err := runPerRepoWaitDeployableUnitSeed(ctx,
		[]relationships.ResolvedRelationship{perRepoWaitUnrelatedRow()}, true, nil)
	if err != nil {
		t.Fatalf("global clean run error = %v, want nil", err)
	}
	if outcome.fingerprint() != clean.fingerprint() {
		t.Fatalf("narrow fingerprint = %q, global clean = %q", outcome.fingerprint(), clean.fingerprint())
	}
}

func TestPerRepoWaitNarrowDeferralCarriesWaitKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	loader := &stubRestrictedCorpusFencedLoader{blocking: []string{"scope-deploy", "scope-other"}}
	// The outcome runner pins deferrals into the outcome (like the global
	// baseline runner); the wait key lives on the raw deferral, so call
	// the seam directly here.
	_, err := loadNarrowWorkloadProjectionScopeInputsForTest(ctx, loader)
	if err == nil {
		t.Fatal("narrow seed error = nil, want a resolution-not-ready deferral")
	}
	var narrowing interface {
		CorpusWaitRepoIDs() []string
		CorpusWaitBlockingScopeIDs() []string
	}
	if !errors.As(err, &narrowing) {
		t.Fatalf("deferral error = %v (%T), want narrowing-carrying deferral", err, err)
	}
	if got := narrowing.CorpusWaitRepoIDs(); !slices.Equal(got, []string{"repo-service"}) {
		t.Fatalf("deferral repo ids = %v, want [repo-service]", got)
	}
	if got := narrowing.CorpusWaitBlockingScopeIDs(); !slices.Equal(got, []string{"scope-deploy", "scope-other"}) {
		t.Fatalf("deferral blocking scopes = %v, want [scope-deploy scope-other]", got)
	}
}

// TestPerRepoWaitNarrowRestrictedReadIsFingerprinted proves the production
// wiring takes the fingerprinted restricted branch when the loader offers
// it: the read is narrowed, fingerprinted, and carries the verdict-time
// sets the reopen gate and the wait key need. A method-set drift that
// satisfies only test stubs would still compile but take the
// non-fingerprinted branch here, silently leaving fingerprint recording off
// in production (#10877). Both the workload and the deployable-unit entries
// are covered because the latter delegates to the former.
func TestPerRepoWaitNarrowRestrictedReadIsFingerprinted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	loader := &stubRestrictedCorpusFencedLoader{
		repoResolved: []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()},
		blocking:     []string{"scope-deploy"},
		complete:     true,
		fingerprint:  "fp-narrow-claimed",
	}
	candidates := []WorkloadCandidate{{RepoID: "repo-service", DeploymentRepoIDs: []string{"repo-deploy"}}}
	intent := reducercontract.Intent{IntentID: "narrow-fingerprint-proof", ScopeID: "scope-test", GenerationID: "gen-1"}

	assertFingerprinted := func(name string, read corpusFencedResolvedRead, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s error = %v, want nil", name, err)
		}
		if !read.narrowed {
			t.Fatalf("%s narrowed = false, want the restricted branch taken", name)
		}
		if !read.fingerprinted {
			t.Fatalf("%s fingerprinted = false, want the fingerprinted restricted read recorded", name)
		}
		if read.fingerprint.Value != "fp-narrow-claimed" {
			t.Fatalf("%s fingerprint value = %q, want fp-narrow-claimed", name, read.fingerprint.Value)
		}
		if !slices.Equal(read.fingerprint.RepoIDs, []string{"repo-service"}) {
			t.Fatalf("%s fingerprint repos = %v, want [repo-service]", name, read.fingerprint.RepoIDs)
		}
		if !slices.Equal(read.repoIDs, []string{"repo-service"}) {
			t.Fatalf("%s wait-key repos = %v, want [repo-service]", name, read.repoIDs)
		}
		if !slices.Equal(read.blockingScopeIDs, []string{"scope-deploy"}) {
			t.Fatalf("%s wait-key blocking scopes = %v, want [scope-deploy]", name, read.blockingScopeIDs)
		}
	}

	read, err := readRestrictedCorpusFencedRelationships(ctx, loader, nil, intent, candidates)
	assertFingerprinted("workload restricted read", read, err)

	duRead, err := readDeployableUnitRestrictedInput(ctx, loader, nil, intent, candidates)
	assertFingerprinted("deployable-unit restricted read", duRead, err)

	if loader.calls != 2 {
		t.Fatalf("restricted loader calls = %d, want 2 (one per entry)", loader.calls)
	}
}
