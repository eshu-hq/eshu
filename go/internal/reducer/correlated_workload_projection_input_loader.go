// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/accepted"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// loadResolvedRelationshipsForIntent reads the generation-pinned own-scope
// resolved set when the loader supports it, falling back to the scope read.
// It lives with the readiness gate because callers must establish
// ownResolutionGenerationReady before using its output; otherwise an inactive
// generation can look complete.
func loadResolvedRelationshipsForIntent(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent reducercontract.Intent,
) ([]relationships.ResolvedRelationship, error) {
	if generationScoped, ok := loader.(GenerationScopedResolvedRelationshipLoader); ok {
		return generationScoped.GetResolvedRelationshipsForGeneration(
			ctx,
			intent.ScopeID,
			intent.GenerationID,
		)
	}
	return loader.GetResolvedRelationships(ctx, intent.ScopeID)
}

// CorrelatedWorkloadProjectionInputLoader reuses deployable-unit correlation
// semantics as the authoritative gate for workload materialization inputs.
type CorrelatedWorkloadProjectionInputLoader struct {
	FactLoader     factload.FactLoader
	ResolvedLoader ResolvedRelationshipLoader
	ScopeResolver  DeploymentRepoScopeResolver
	// ResolutionActiveLookup backs the resolution-readiness gate: the load
	// defers while the scope's own relationship generation is inactive. Nil
	// keeps the gate open for test wiring. This is the shared
	// relationship-generation fence also backing the repo-dependency lane,
	// so main.go wires the same lookup value here.
	ResolutionActiveLookup accepted.RelationshipGenerationActiveLookup
	// ResolutionsCompleteLookup backs the corpus-wide resolution-readiness
	// gate: the load defers while any active scope's current relationship
	// generation is inactive, because the by-repos resolved read merges
	// foreign scopes the own-generation check cannot see (#6184). Nil keeps
	// the gate open for test wiring; main.go wires the Postgres lookup here.
	// It is not consulted when ResolvedLoader implements
	// CorpusFencedResolvedRelationshipLoader: that read carries its own
	// same-snapshot verdict (#6740).
	ResolutionsCompleteLookup accepted.RelationshipGenerationsCompleteLookup
	// IncompleteScopesLookup best-effort names the scopes holding the fence
	// on a deferral, so the error is actionable instead of opaque (#6730).
	// Nil-safe: a nil lookup or a lookup error simply omits the holder list.
	IncompleteScopesLookup accepted.RelationshipGenerationsIncompleteScopesLookup
	// ResolutionProducerLookup reads the own generation's deployment_mapping
	// producer when the own gate is closed (#7597), so a dead-lettered
	// producer counts instead of deferring forever. Nil keeps the plain
	// deferral for test wiring; see crossrepo.EvaluateOwnGate.
	ResolutionProducerLookup crossrepo.ProducerLookup
	// Instruments and Logger make the candidate-precondition probe outcome
	// observable (#9986): a counter per call and a rate-limited error log.
	// Both are optional; a nil Logger falls back to slog.Default().
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
	// probeErrorLog rate-limits the probe error log per cause class; nil
	// uses the process-wide limiter. Tests inject their own.
	probeErrorLog *probeErrorLogLimiter
}

// LoadWorkloadProjectionInputs loads workload candidates, enriches them with
// resolved deployment sources, and returns only admitted deployable units
// selected by the intent's entity keys.
func (l CorrelatedWorkloadProjectionInputLoader) LoadWorkloadProjectionInputs(
	ctx context.Context,
	intent reducercontract.Intent,
) ([]WorkloadCandidate, map[string][]string, error) {
	inputs, err := l.LoadWorkloadProjectionScopeInputs(ctx, intent)
	if err != nil {
		return nil, nil, err
	}
	return inputs.Candidates, inputs.DeploymentEnvironments, nil
}

// LoadWorkloadProjectionScopeInputs is LoadWorkloadProjectionInputs plus the
// scope generation's complete admitted set. It admits every scope candidate
// once and then narrows the admitted set to the intent's entity keys. The
// narrowing matches on repository identity only and admission neither reads
// nor changes it, so the narrowed set equals the one filtering before
// admission produced (#7285 retract-scope ruling).
func (l CorrelatedWorkloadProjectionInputLoader) LoadWorkloadProjectionScopeInputs(
	ctx context.Context,
	intent reducercontract.Intent,
) (WorkloadProjectionInputs, error) {
	if l.FactLoader == nil {
		return WorkloadProjectionInputs{}, fmt.Errorf("correlated workload projection fact loader is required")
	}
	// #9697: cheap readiness probes before the bulk load below; a deferred
	// intent must not pay for facts it discards.
	if err := l.deferUnlessReadyForBulkLoad(ctx, intent); err != nil {
		return WorkloadProjectionInputs{}, err
	}

	envelopes, err := factload.LoadFactsForKinds(
		ctx,
		l.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factload.FactKindRepository, factload.FactKindFile},
	)
	if err != nil {
		return WorkloadProjectionInputs{}, fmt.Errorf("load facts for correlated workload projection: %w", err)
	}

	candidates, deploymentEnvironments := ExtractWorkloadCandidates(envelopes)
	// Fail closed before the resolved-relationship read, mirroring the
	// deployable-unit correlation gate: an inactive own generation means both
	// resolved loads are partial, and succeeding on partial input is never
	// reopened (#6184). The own-scope check cannot see foreign scopes; the
	// corpus-wide fence below covers them. A closed gate reads the own
	// deployment_mapping producer (#7597): dead-lettered counts, never defers.
	gate := crossrepo.EvaluateOwnGate(ctx, l.ResolutionActiveLookup, l.ResolutionProducerLookup,
		intent, len(candidates) > 0, time.Now().UTC())
	if gate.Err != nil {
		return WorkloadProjectionInputs{}, gate.Err
	}
	if !gate.Ready {
		return WorkloadProjectionInputs{}, workloadMaterializationResolutionNotReadyError{
			scopeID: intent.ScopeID, generationID: intent.GenerationID, producerDetail: gate.ProducerDetail,
		}
	}
	// The own-scope check cannot see foreign scopes, whose retired-or-pending
	// generations would feed the by-repos read a partial set. Defer on the
	// corpus-wide fence with the same non-counting retry class; a fence lookup
	// failure travels on the deferral so an outage never reads as healthy
	// backpressure (#6730). readCorpusFencedResolvedRelationships takes the
	// fence verdict from the by-repos read's own statement snapshot when the
	// store offers it (#6740), so no advance-and-complete cycle can slip
	// between the verdict and the rows it admits.
	read, err := readCorpusFencedResolvedRelationships(ctx, l.ResolvedLoader, l.ResolutionsCompleteLookup, intent, candidates)
	if err != nil {
		return WorkloadProjectionInputs{}, fmt.Errorf("load resolved relationships for correlated workload projection: %w", err)
	}
	if read.fenceErr != nil {
		return WorkloadProjectionInputs{}, workloadMaterializationResolutionNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
			cause:        read.fenceErr,
		}
	}
	if !read.complete {
		return WorkloadProjectionInputs{}, workloadMaterializationResolutionNotReadyError{
			scopeID:         intent.ScopeID,
			generationID:    intent.GenerationID,
			holdingScopeIDs: accepted.IncompleteScopeIDs(ctx, l.IncompleteScopesLookup),
		}
	}
	if l.ResolvedLoader != nil {
		candidates = applyResolvedDeploymentSources(candidates, read.resolved)
		logDeploymentSourceGuardStats(ctx, string(intent.Domain), intent.ScopeID, intent.GenerationID, read.resolved)
		candidates = applyResolvedProvisioningSources(candidates, read.resolved)
	}

	if l.ScopeResolver != nil {
		deploymentEnvironments = l.enrichDeploymentRepoEnvironments(
			ctx, candidates, deploymentEnvironments,
		)
	}

	// Admit the whole scope first: the stale-edge retract keeps what the
	// scope generation supports, never what this intent's keys select.
	admitted, err := admittedCorrelatedWorkloadCandidates(intent, candidates)
	if err != nil {
		return WorkloadProjectionInputs{}, err
	}
	written := admitted
	selection := CandidateSelectionReport{Admitted: len(admitted), Selected: len(admitted), Reason: SelectionNoKeys}
	if len(intent.EntityKeys) > 0 {
		entityKeys, err := deployableUnitCorrelationEntityKeys(intent)
		if err != nil {
			return WorkloadProjectionInputs{}, err
		}
		written, selection = filterDeployableUnitCandidates(admitted, entityKeys)
	}
	return WorkloadProjectionInputs{
		Candidates:             written,
		ScopeCandidates:        admitted,
		DeploymentEnvironments: deploymentEnvironments,
		Selection:              selection,
	}, nil
}

// CorpusFencedResolvedRelationshipLoader returns the active resolved
// relationships touching one or more repositories together with the
// corpus-completeness fence verdict, both evaluated in ONE statement snapshot
// (#6740). complete is false while any active scope's current relationship
// generation is retired-or-pending (the shape
// AreActiveScopeRelationshipGenerationsComplete reports); the rows are then
// empty and must not be consumed. Because verdict and rows share a snapshot,
// a scope that retires, re-resolves, and re-activates its generation while
// the read runs is either wholly before or wholly after it, never mixed. An
// empty repoIDs slice still evaluates the fence. Loaders preferred through
// readCorpusFencedResolvedRelationships; stores without it keep the
// two-statement fence and post-read recheck.
type CorpusFencedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForReposWithCorpusFence(
		ctx context.Context,
		repoIDs []string,
	) ([]relationships.ResolvedRelationship, bool, error)
}

// corpusFencedResolvedRead is the merged own-scope plus by-repos resolved set
// and the corpus fence verdict it was admitted under. fenceErr carries a
// separate-statement fence lookup failure (a deferral cause, not a read
// failure); it is always nil on the fused path, whose query error is a read
// error. fingerprint and fingerprinted are set only by
// readDeployableUnitResolvedInput (#8283).
type corpusFencedResolvedRead struct {
	resolved      []relationships.ResolvedRelationship
	complete      bool
	fenceErr      error
	fingerprint   reducercontract.ResolvedReadFingerprint
	fingerprinted bool
}

// readCorpusFencedResolvedRelationships loads the resolved relationships a
// workload or deployable-unit pass consumes, gated on the corpus fence. With
// a CorpusFencedResolvedRelationshipLoader and a non-empty candidate set, the
// fence verdict comes from the by-repos read's own statement (#6740) and the
// separate lookup is never consulted. Otherwise the fence is checked before
// the read and re-checked after it (#6730); that fallback still has the
// residual window a full advance-and-complete cycle between the two checks
// slips through, which is why the production store implements the fused
// method (TestRelationshipStoreSatisfiesCorpusFencedResolvedRelationshipLoader
// in go/internal/storage/postgres asserts it on the store type). On the fused
// path an incomplete verdict returns before the own-scope read, and a fused
// query error is a read error, not a fenceErr deferral. A nil loader reads
// nothing and only evaluates the fence. The own-generation check is the
// caller's job and must run first.
func readCorpusFencedResolvedRelationships(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	completeLookup accepted.RelationshipGenerationsCompleteLookup,
	intent reducercontract.Intent,
	candidates []WorkloadCandidate,
) (corpusFencedResolvedRead, error) {
	if fenced, ok := loader.(CorpusFencedResolvedRelationshipLoader); ok && len(candidates) > 0 {
		repoResolved, complete, err := fenced.GetResolvedRelationshipsForReposWithCorpusFence(
			ctx, workloadCandidateRepoIDs(candidates),
		)
		if err != nil {
			return corpusFencedResolvedRead{}, err
		}
		return finishFusedResolvedRead(ctx, loader, intent, repoResolved, complete)
	}
	complete, err := corpusResolutionsComplete(ctx, completeLookup, candidates)
	if err != nil || !complete || loader == nil {
		return corpusFencedResolvedRead{complete: complete && err == nil, fenceErr: err}, nil
	}
	resolved, err := loadWorkloadResolvedRelationships(ctx, loader, intent, candidates)
	if err != nil {
		return corpusFencedResolvedRead{}, err
	}
	// Re-evaluate after the foreign read (#6730 Codex P1): success requires
	// the fence to hold across the whole pass on this two-statement path.
	complete, err = corpusResolutionsComplete(ctx, completeLookup, candidates)
	if err != nil || !complete {
		return corpusFencedResolvedRead{fenceErr: err}, nil
	}
	return corpusFencedResolvedRead{resolved: resolved, complete: true}, nil
}

// finishFusedResolvedRead completes a fused corpus-fenced read: an incomplete
// verdict returns before the own-scope read, otherwise the own-scope rows are
// merged with the by-repos rows the verdict admitted. The own-scope read is
// pinned to the intent's generation, so reading it after the verdict loses
// nothing and a deferral never pays for it (#6740 review F1).
func finishFusedResolvedRead(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent reducercontract.Intent,
	repoResolved []relationships.ResolvedRelationship,
	complete bool,
) (corpusFencedResolvedRead, error) {
	if !complete {
		return corpusFencedResolvedRead{}, nil
	}
	resolved, err := loadResolvedRelationshipsForIntent(ctx, loader, intent)
	if err != nil {
		return corpusFencedResolvedRead{}, err
	}
	return corpusFencedResolvedRead{
		resolved: mergeResolvedRelationships(resolved, repoResolved),
		complete: true,
	}, nil
}

func loadWorkloadResolvedRelationships(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent reducercontract.Intent,
	candidates []WorkloadCandidate,
) ([]relationships.ResolvedRelationship, error) {
	resolved, err := loadResolvedRelationshipsForIntent(ctx, loader, intent)
	if err != nil {
		return nil, err
	}

	repoScoped, ok := loader.(RepositoryScopedResolvedRelationshipLoader)
	if !ok {
		return resolved, nil
	}
	repoIDs := workloadCandidateRepoIDs(candidates)
	if len(repoIDs) == 0 {
		return resolved, nil
	}
	repoResolved, err := repoScoped.GetResolvedRelationshipsForRepos(ctx, repoIDs)
	if err != nil {
		return nil, err
	}
	return mergeResolvedRelationships(resolved, repoResolved), nil
}

func workloadCandidateRepoIDs(candidates []WorkloadCandidate) []string {
	repoIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		repoIDs = appendUniqueString(repoIDs, candidate.RepoID)
	}
	return uniqueSortedStrings(repoIDs)
}

func mergeResolvedRelationships(
	base []relationships.ResolvedRelationship,
	extra []relationships.ResolvedRelationship,
) []relationships.ResolvedRelationship {
	if len(extra) == 0 {
		return base
	}
	merged := make([]relationships.ResolvedRelationship, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base)+len(extra))
	for _, relationship := range append(base, extra...) {
		key := resolvedRelationshipMergeKey(relationship)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, relationship)
	}
	return merged
}

func resolvedRelationshipMergeKey(relationship relationships.ResolvedRelationship) string {
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s",
		relationship.SourceRepoID,
		relationship.TargetRepoID,
		relationship.SourceEntityID,
		relationship.TargetEntityID,
		relationship.RelationshipType,
	)
}

func admittedCorrelatedWorkloadCandidates(
	intent reducercontract.Intent,
	candidates []WorkloadCandidate,
) ([]WorkloadCandidate, error) {
	admitted := make([]WorkloadCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))

	for _, candidate := range candidates {
		evaluation, err := engine.Evaluate(
			deployableUnitRulePack(candidate),
			deployableUnitModelCandidates(intent, candidate),
		)
		if err != nil {
			return nil, fmt.Errorf("evaluate correlated workload candidate %q: %w", candidate.RepoName, err)
		}
		for _, result := range evaluation.Results {
			if result.Candidate.State != correlationmodel.CandidateStateAdmitted {
				continue
			}
			correlatedCandidate := candidate
			correlatedCandidate.WorkloadName = correlatedWorkloadName(candidate, result.Candidate)
			correlatedCandidate.Confidence = result.Candidate.Confidence

			candidateKey := fmt.Sprintf("%s:%s", correlatedCandidate.RepoID, correlatedCandidate.WorkloadName)
			if _, ok := seen[candidateKey]; ok {
				continue
			}
			seen[candidateKey] = struct{}{}
			admitted = append(admitted, correlatedCandidate)
		}
	}

	return admitted, nil
}

func correlatedWorkloadName(
	candidate WorkloadCandidate,
	evaluatedCandidate correlationmodel.Candidate,
) string {
	prefix := candidate.RepoID + ":"
	if strings.HasPrefix(evaluatedCandidate.CorrelationKey, prefix) {
		if unitKey := strings.TrimSpace(strings.TrimPrefix(evaluatedCandidate.CorrelationKey, prefix)); unitKey != "" {
			return unitKey
		}
	}
	return candidateWorkloadName(candidate)
}

// corpusResolutionsComplete requires every active scope's current relationship
// generation before a resolved-relationship read. The own-generation check
// cannot see foreign scopes, and the by-repos read filters on status =
// 'active', so without this fence a retired-or-pending foreign generation
// feeds derivation a partial set that varies run to run, and success on that
// partial input is never reopened (#6184). Empty candidate sets are vacuous
// and do not wait on work they do not consume. A nil lookup keeps the gate
// open for test wiring; a lookup error fails safe as incomplete.
func corpusResolutionsComplete(
	ctx context.Context,
	lookup accepted.RelationshipGenerationsCompleteLookup,
	candidates []WorkloadCandidate,
) (bool, error) {
	if len(candidates) == 0 {
		return true, nil
	}
	if lookup == nil {
		return true, nil
	}
	complete, err := lookup(ctx)
	if err != nil {
		return false, err
	}
	return complete, nil
}
