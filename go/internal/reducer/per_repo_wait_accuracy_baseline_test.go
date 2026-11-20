// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Per-repo-wait accuracy baseline (#10877).
//
// These fixtures pin what the CURRENT global corpus fence does for the three
// accuracy classes the per-repo-wait design must preserve. They run the same
// seed through the deployable-unit and workload lanes under a scripted fence
// and record the outcome fingerprint. A future lane implementing per-scope
// waits replays the same seeds through the narrowed path and must produce
// identical fingerprints (exact rows, not counts) wherever the design says
// the outcomes agree.
//
// The baseline truths pinned here:
//   - should-match: a foreign deployment row pending at first attempt defers;
//     once its scope activates, the retry converges to the clean-run output.
//   - should-not-match: a fence held ONLY by an unrelated scope still defers
//     today. The global fence cannot tell producers from bystanders; that
//     over-wait is the perf gap per-repo waits may close, never a correctness
//     bug to "fix" by weakening the gate.
//   - unclear/absence: with a complete fence and no foreign row, the workload
//     succeeds with zero admitted candidates (this seed's admission needs the
//     foreign deployment linkage; proven by probe, not assumed); a late
//     foreign row must converge a later run to the clean-run output. Workload
//     materialization has no fingerprint/reopen (DU-only, #8283), so a
//     succeeded absence run is never restarted today: the fixture pins both
//     endpoint states for the reopen lane, it does not assert convergence
//     happens on its own.
//   - race (#6184/#6740): the fused read defers on a partial set while the
//     legacy two-statement path succeeds on it. That divergence is why the
//     production store must offer the fused read; narrowing must keep the
//     verdict and the rows in one snapshot.
//
// Fixtures and harness only: no production code changes in this lane.

// perRepoWaitForeignDeployRow is the foreign scope's deployment linkage for
// the workload seed candidate (repo-service): repo-deploy DEPLOYS repo-service
// with ArgoCD application evidence, deploy-repo-originated direction.
func perRepoWaitForeignDeployRow() relationships.ResolvedRelationship {
	return relationships.ResolvedRelationship{
		SourceRepoID:     "repo-deploy",
		TargetRepoID:     "repo-service",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.94,
		EvidenceCount:    1,
		ResolutionSource: relationships.ResolutionSourceInferred,
		Details: map[string]any{
			"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)},
		},
	}
}

// perRepoWaitUnrelatedRow touches neither seed candidate repo. A scope whose
// only rows look like this is a proven non-producer for the seed read set,
// the only holder a per-repo wait may exclude.
func perRepoWaitUnrelatedRow() relationships.ResolvedRelationship {
	return relationships.ResolvedRelationship{
		SourceRepoID:     "repo-other-deploy",
		TargetRepoID:     "repo-other",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.9,
		EvidenceCount:    1,
		ResolutionSource: relationships.ResolutionSourceInferred,
		Details: map[string]any{
			"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)},
		},
	}
}

// perRepoWaitDeployableUnitForeignRow is the same linkage for the DU seed
// candidate (repo-edge-api), mirroring the quiescence test's resolved row.
func perRepoWaitDeployableUnitForeignRow() relationships.ResolvedRelationship {
	return relationships.ResolvedRelationship{
		SourceRepoID:     "repo-deployments",
		TargetRepoID:     "repo-edge-api",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.94,
		Details: map[string]any{
			"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)},
		},
	}
}

// perRepoWaitWorkloadOutcome is the comparable outcome of one workload-loader
// seed run: the exact surface a per-repo-wait replay must reproduce.
// scopeAdmitted is the whole-scope admitted set (what a future retract reads);
// narrowed is the intent entity-key selection written for this intent.
type perRepoWaitWorkloadOutcome struct {
	deferred      string
	holdingScopes []string
	scopeAdmitted int
	narrowed      int
	deploymentIDs []string
	fencedRepoIDs []string
	fencedCalls   int
}

// fingerprint renders the outcome as one comparable string for the future
// differential replay (global run vs narrowed run, same seed).
func (o perRepoWaitWorkloadOutcome) fingerprint() string {
	return fmt.Sprintf(
		"deferred=%s holding=[%s] scope=%d narrowed=%d deployment=[%s] repos=[%s] fencedCalls=%d",
		o.deferred,
		strings.Join(slices.Sorted(slices.Values(o.holdingScopes)), ","),
		o.scopeAdmitted,
		o.narrowed,
		strings.Join(slices.Sorted(slices.Values(o.deploymentIDs)), ","),
		strings.Join(o.fencedRepoIDs, ","),
		o.fencedCalls,
	)
}

// runPerRepoWaitWorkloadSeed runs the Dockerfile workload seed once under the
// scripted fence and captures the outcome. complete=false models a foreign
// scope retired-or-pending; holding names the scopes the fence blames.
func runPerRepoWaitWorkloadSeed(
	ctx context.Context,
	repoResolved []relationships.ResolvedRelationship,
	complete bool,
	holding []string,
) (perRepoWaitWorkloadOutcome, []WorkloadCandidate, error) {
	resolvedLoader := &stubCorpusFencedResolvedLoader{
		repoResolved: repoResolved,
		complete:     complete,
	}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:         resolvedLoader,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		IncompleteScopesLookup: func(context.Context) ([]string, error) {
			return holding, nil
		},
	}
	inputs, err := loader.LoadWorkloadProjectionScopeInputs(ctx, dockerfileLoaderIntent())
	outcome := perRepoWaitWorkloadOutcome{
		fencedCalls: resolvedLoader.fencedCalls,
	}
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
		return outcome, nil, err
	}
	return outcome, inputs.Candidates, nil
}

func TestPerRepoWaitBaselineWorkloadShouldMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deferred, _, err := runPerRepoWaitWorkloadSeed(ctx, nil, false, []string{"scope-deploy"})
	if err != nil {
		t.Fatalf("first attempt error = %v, want a resolution-not-ready deferral", err)
	}
	if deferred.deferred != WorkloadMaterializationResolutionNotReadyFailureClass {
		t.Fatalf("first attempt fingerprint = %q, want deferred=%q",
			deferred.fingerprint(), WorkloadMaterializationResolutionNotReadyFailureClass)
	}
	if !slices.Equal(deferred.holdingScopes, []string{"scope-deploy"}) {
		t.Fatalf("first attempt holding scopes = %v, want [scope-deploy]", deferred.holdingScopes)
	}

	woken, _, err := runPerRepoWaitWorkloadSeed(ctx, []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}, true, nil)
	if err != nil {
		t.Fatalf("retry error = %v, want nil once the foreign scope activates", err)
	}
	clean, _, err := runPerRepoWaitWorkloadSeed(ctx, []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}, true, nil)
	if err != nil {
		t.Fatalf("clean run error = %v, want nil", err)
	}
	if woken.fingerprint() != clean.fingerprint() {
		t.Fatalf("retry fingerprint = %q, clean fingerprint = %q: the woken retry must converge to the clean global run",
			woken.fingerprint(), clean.fingerprint())
	}
	if !slices.Contains(woken.deploymentIDs, "repo-deploy") {
		t.Fatalf("retry deployment ids = %v, want [repo-deploy]: the foreign row must enrich the candidate", woken.deploymentIDs)
	}
	if !slices.Equal(woken.fencedRepoIDs, []string{"repo-service"}) {
		t.Fatalf("fenced read repos = %v, want [repo-service]", woken.fencedRepoIDs)
	}
}

func TestPerRepoWaitBaselineWorkloadUnrelatedHolderStillDefers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// The fence is held ONLY by scope-unrelated, whose rows never touch
	// repo-service. The global fence still defers: it cannot distinguish a
	// producer from a bystander. This over-wait is the documented perf gap a
	// per-producer-scope wait may close; the narrowed replay of this seed
	// must succeed with the clean-run fingerprint instead of deferring.
	outcome, _, err := runPerRepoWaitWorkloadSeed(
		ctx,
		[]relationships.ResolvedRelationship{perRepoWaitUnrelatedRow()},
		false,
		[]string{"scope-unrelated"},
	)
	if err != nil {
		t.Fatalf("seed error = %v, want a resolution-not-ready deferral", err)
	}
	if outcome.deferred != WorkloadMaterializationResolutionNotReadyFailureClass {
		t.Fatalf("outcome fingerprint = %q, want a deferral: the global fence waits on unrelated holders too",
			outcome.fingerprint())
	}
}

func TestPerRepoWaitBaselineWorkloadAbsenceThenLateArrival(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	absent, _, err := runPerRepoWaitWorkloadSeed(ctx, nil, true, nil)
	if err != nil {
		t.Fatalf("absence run error = %v, want nil: a complete fence with no foreign row succeeds", err)
	}
	if absent.fencedCalls != 1 {
		t.Fatalf("absence run fenced calls = %d, want 1: the read still runs under a complete fence", absent.fencedCalls)
	}
	if absent.scopeAdmitted != 0 {
		t.Fatalf("absence run scope admitted = %d, want 0: this seed's admission needs the foreign deployment linkage",
			absent.scopeAdmitted)
	}
	if len(absent.deploymentIDs) != 0 {
		t.Fatalf("absence run deployment ids = %v, want none", absent.deploymentIDs)
	}

	arrived, _, err := runPerRepoWaitWorkloadSeed(ctx, []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}, true, nil)
	if err != nil {
		t.Fatalf("late-arrival run error = %v, want nil", err)
	}
	clean, _, err := runPerRepoWaitWorkloadSeed(ctx, []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}, true, nil)
	if err != nil {
		t.Fatalf("clean run error = %v, want nil", err)
	}
	if arrived.fingerprint() != clean.fingerprint() {
		t.Fatalf("late-arrival fingerprint = %q, clean = %q: the converged run must equal the clean global run",
			arrived.fingerprint(), clean.fingerprint())
	}
	if !slices.Contains(arrived.deploymentIDs, "repo-deploy") {
		t.Fatalf("late-arrival deployment ids = %v, want [repo-deploy]", arrived.deploymentIDs)
	}
	// Pinned non-convergence: workload materialization records no reopen
	// fingerprint, so nothing restarts the succeeded absence run when the
	// late row lands. The narrowed design must add fingerprint parity
	// before workload leaves the global fence.
}

// perRepoWaitDeployableUnitOutcome is the comparable outcome of one DU seed
// run: the exact surface a per-repo-wait replay must reproduce.
type perRepoWaitDeployableUnitOutcome struct {
	deferred        string
	holdingScopes   []string
	status          string
	evidenceSummary string
	canonicalWrites int
	retractCalls    int
	writeCalls      int
}

func (o perRepoWaitDeployableUnitOutcome) fingerprint() string {
	return fmt.Sprintf(
		"deferred=%s holding=[%s] status=%s summary=%q canonical=%d retract=%d write=%d",
		o.deferred,
		strings.Join(slices.Sorted(slices.Values(o.holdingScopes)), ","),
		o.status,
		o.evidenceSummary,
		o.canonicalWrites,
		o.retractCalls,
		o.writeCalls,
	)
}

// runPerRepoWaitDeployableUnitSeed runs the Dockerfile DU seed once under the
// scripted fence and captures the outcome.
func runPerRepoWaitDeployableUnitSeed(
	ctx context.Context,
	repoResolved []relationships.ResolvedRelationship,
	complete bool,
	holding []string,
) (perRepoWaitDeployableUnitOutcome, error) {
	resolvedLoader := &stubCorpusFencedResolvedLoader{
		repoResolved: repoResolved,
		complete:     complete,
	}
	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         resolvedLoader,
		PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:             writer,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		IncompleteScopesLookup: func(context.Context) ([]string, error) {
			return holding, nil
		},
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

func TestPerRepoWaitBaselineDeployableUnitShouldMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deferred, err := runPerRepoWaitDeployableUnitSeed(ctx, nil, false, []string{"scope-deploy"})
	if err != nil {
		t.Fatalf("first attempt error = %v, want a resolution-not-ready deferral", err)
	}
	if deferred.deferred != DeployableUnitCorrelationResolutionNotReadyFailureClass {
		t.Fatalf("first attempt fingerprint = %q, want deferred=%q",
			deferred.fingerprint(), DeployableUnitCorrelationResolutionNotReadyFailureClass)
	}
	if deferred.retractCalls != 0 || deferred.writeCalls != 0 {
		t.Fatalf("deferral edge calls = retract:%d write:%d, want 0/0: a deferral must write nothing",
			deferred.retractCalls, deferred.writeCalls)
	}

	foreign := []relationships.ResolvedRelationship{perRepoWaitDeployableUnitForeignRow()}
	woken, err := runPerRepoWaitDeployableUnitSeed(ctx, foreign, true, nil)
	if err != nil {
		t.Fatalf("retry error = %v, want nil once the foreign scope activates", err)
	}
	clean, err := runPerRepoWaitDeployableUnitSeed(ctx, foreign, true, nil)
	if err != nil {
		t.Fatalf("clean run error = %v, want nil", err)
	}
	if woken.fingerprint() != clean.fingerprint() {
		t.Fatalf("retry fingerprint = %q, clean fingerprint = %q: the woken retry must converge to the clean global run",
			woken.fingerprint(), clean.fingerprint())
	}
	if woken.canonicalWrites != 1 || woken.writeCalls != 1 || woken.retractCalls != 1 {
		t.Fatalf("retry writes = canonical:%d retract:%d write:%d, want 1/1/1",
			woken.canonicalWrites, woken.retractCalls, woken.writeCalls)
	}
}

func TestPerRepoWaitBaselineDeployableUnitAbsenceSucceedsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Complete fence, no foreign row: the DU seed still evaluates its
	// candidate, rejects it for low confidence without deployment evidence,
	// and succeeds with zero edge writes rather than deferring. Absence of
	// evidence is a closed answer once the fence is complete; only an OPEN
	// fence defers.
	outcome, err := runPerRepoWaitDeployableUnitSeed(ctx, nil, true, nil)
	if err != nil {
		t.Fatalf("absence run error = %v, want nil", err)
	}
	if !strings.Contains(outcome.evidenceSummary, "admitted=0") {
		t.Fatalf("absence run summary = %q, want zero admitted", outcome.evidenceSummary)
	}
	if outcome.canonicalWrites != 0 || outcome.writeCalls != 0 {
		t.Fatalf("absence run writes = canonical:%d write:%d, want 0/0",
			outcome.canonicalWrites, outcome.writeCalls)
	}
}

func TestPerRepoWaitBaselineFusedVsTwoStatementRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Legacy two-statement path: a full retire/re-resolve/reactivate cycle of
	// the foreign scope between the pre-read check and the post-read recheck
	// lets both checks pass while the rows come from a mixed generation set
	// (#6740 residual). The always-complete lookup models both checks
	// passing; the admitting foreign row models the rows the cycle leaves
	// behind. The TRUE fence is open throughout: the test knows it, the
	// separate-statement checks cannot see it.
	admitting := []relationships.ResolvedRelationship{perRepoWaitForeignDeployRow()}
	legacyCalls := 0
	legacy := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:                &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:            &stubResolvedRelationshipLoader{repoResolved: admitting},
		ResolutionActiveLookup:    stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		ResolutionsCompleteLookup: alwaysCompleteLookup(&legacyCalls),
	}
	legacyInputs, legacyErr := legacy.LoadWorkloadProjectionScopeInputs(ctx, dockerfileLoaderIntent())

	// Fused path: the same rows under the same open fence defer, because
	// the verdict shares the rows' statement snapshot.
	fused, _, fusedErr := runPerRepoWaitWorkloadSeed(ctx, admitting, false, []string{"scope-foreign"})

	if legacyErr != nil {
		t.Fatalf("legacy two-statement run error = %v, want nil: the residual window admits the unbound rows", legacyErr)
	}
	if len(legacyInputs.ScopeCandidates) == 0 {
		t.Fatal("legacy two-statement run scope admitted 0 candidates, want >0: the unbound rows admit")
	}
	if legacyCalls != 2 {
		t.Fatalf("legacy fence lookup calls = %d, want 2 (pre-read check plus post-read recheck)", legacyCalls)
	}
	if fusedErr != nil {
		t.Fatalf("fused seed harness error = %v, want a captured deferral outcome", fusedErr)
	}
	if fused.deferred == "" {
		t.Fatalf("fused run fingerprint = %q, want a deferral on the same rows the legacy path admits", fused.fingerprint())
	}
}
