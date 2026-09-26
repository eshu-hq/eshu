// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// loadResolvedRelationshipsForIntent reads the generation-pinned own-scope
// resolved set when the loader supports it, falling back to the scope read.
// It lives with the readiness gate because callers must establish
// ownResolutionGenerationReady before using its output; otherwise an inactive
// generation can look complete.
func loadResolvedRelationshipsForIntent(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent Intent,
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

// WorkloadMaterializationResolutionNotReadyFailureClass classifies a deferral
// of workload projection inputs whose own relationship generation has not
// activated yet.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the load is
// waiting on upstream resolution, not failing on its own merits. Without this
// gate the loader merges the generation-pinned own-scope read with an empty
// by-repos read while the generation is retired-or-pending and workload
// materialization succeeds on that partial input, which nothing reopens
// (#6184).
const WorkloadMaterializationResolutionNotReadyFailureClass = "workload_materialization_resolution_not_ready"

// workloadMaterializationResolutionNotReadyError defers the input load until
// the scope's own relationship generation activates.
type workloadMaterializationResolutionNotReadyError struct {
	scopeID      string
	generationID string
	// cause carries the fence lookup failure when the deferral is outage
	// driven rather than backpressure driven; nil for a genuinely
	// incomplete corpus fence (#6730: a swallowed lookup error reads
	// exactly like healthy backpressure at 3 AM).
	cause error
	// holdingScopeIDs names the scopes holding the corpus fence, best
	// effort; empty when the holder lookup is unwired or failed.
	holdingScopeIDs []string
}

func (e workloadMaterializationResolutionNotReadyError) Error() string {
	msg := fmt.Sprintf(
		"cross-repo resolution not active for scope %s generation %s; deferring workload projection inputs rather than materializing against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
	if e.cause != nil {
		msg += fmt.Sprintf("; corpus fence lookup failed: %v", e.cause)
	}
	if len(e.holdingScopeIDs) > 0 {
		msg += fmt.Sprintf("; holding scopes: %s", strings.Join(e.holdingScopeIDs, ","))
	}
	return msg
}

// Unwrap exposes the fence lookup failure to errors.Is/As without changing
// the retryable failure class: the cause travels for logs, the class travels
// for the queue.
func (e workloadMaterializationResolutionNotReadyError) Unwrap() error {
	return e.cause
}

func (workloadMaterializationResolutionNotReadyError) Retryable() bool { return true }

func (workloadMaterializationResolutionNotReadyError) FailureClass() string {
	return WorkloadMaterializationResolutionNotReadyFailureClass
}

// CorrelatedWorkloadProjectionInputLoader reuses deployable-unit correlation
// semantics as the authoritative gate for workload materialization inputs.
type CorrelatedWorkloadProjectionInputLoader struct {
	FactLoader     FactLoader
	ResolvedLoader ResolvedRelationshipLoader
	ScopeResolver  DeploymentRepoScopeResolver
	// ResolutionActiveLookup backs the resolution-readiness gate: the load
	// defers while the scope's own relationship generation is inactive. Nil
	// keeps the gate open for test wiring. This is the shared
	// relationship-generation fence also backing the repo-dependency lane,
	// so main.go wires the same lookup value here.
	ResolutionActiveLookup maintenance.RelationshipGenerationActiveLookup
	// ResolutionsCompleteLookup backs the corpus-wide resolution-readiness
	// gate: the load defers while any active scope's current relationship
	// generation is inactive, because the by-repos resolved read merges
	// foreign scopes the own-generation check cannot see (#6184). Nil keeps
	// the gate open for test wiring; main.go wires the Postgres lookup here.
	ResolutionsCompleteLookup maintenance.RelationshipGenerationsCompleteLookup
	// IncompleteScopesLookup best-effort names the scopes holding the fence
	// on a deferral, so the error is actionable instead of opaque (#6730).
	// Nil-safe: a nil lookup or a lookup error simply omits the holder list.
	IncompleteScopesLookup maintenance.RelationshipGenerationsIncompleteScopesLookup
}

// LoadWorkloadProjectionInputs loads workload candidates, enriches them with
// resolved deployment sources, and returns only admitted deployable units.
func (l CorrelatedWorkloadProjectionInputLoader) LoadWorkloadProjectionInputs(
	ctx context.Context,
	intent Intent,
) ([]WorkloadCandidate, map[string][]string, error) {
	if l.FactLoader == nil {
		return nil, nil, fmt.Errorf("correlated workload projection fact loader is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		l.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindRepository, factKindFile},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("load facts for correlated workload projection: %w", err)
	}

	candidates, deploymentEnvironments := ExtractWorkloadCandidates(envelopes)
	// Fail closed before the resolved-relationship read, mirroring the
	// deployable-unit correlation gate: an inactive own generation means the
	// pinned own-scope load and the by-repos load are both partial, and
	// workload materialization succeeding on that partial input is never
	// reopened (#6184). The own-scope check is deliberately own-scope only
	// (foreign scopes are undiscoverable before the read itself); the
	// corpus-wide fence below covers the foreign side.
	if !ownResolutionGenerationReady(l.ResolutionActiveLookup, intent, candidates) {
		return nil, nil, workloadMaterializationResolutionNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}
	// The own-scope check cannot see foreign scopes, whose retired-or-pending
	// generations would feed the by-repos read below a partial set. Defer on
	// the corpus-wide fence with the same non-counting retry class; a fence
	// lookup failure travels on the deferral so an outage never reads as
	// healthy backpressure (#6730).
	fenceReady, fenceErr := corpusResolutionsComplete(ctx, l.ResolutionsCompleteLookup, candidates)
	if fenceErr != nil {
		return nil, nil, workloadMaterializationResolutionNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
			cause:        fenceErr,
		}
	}
	if !fenceReady {
		return nil, nil, workloadMaterializationResolutionNotReadyError{
			scopeID:         intent.ScopeID,
			generationID:    intent.GenerationID,
			holdingScopeIDs: maintenance.IncompleteScopeIDs(ctx, l.IncompleteScopesLookup),
		}
	}
	if l.ResolvedLoader != nil {
		resolved, err := loadWorkloadResolvedRelationships(ctx, l.ResolvedLoader, intent, candidates)
		if err != nil {
			return nil, nil, fmt.Errorf("load resolved relationships for correlated workload projection: %w", err)
		}
		candidates = applyResolvedDeploymentSources(candidates, resolved)
		logDeploymentSourceGuardStats(ctx, string(intent.Domain), intent.ScopeID, intent.GenerationID, resolved)
		candidates = applyResolvedProvisioningSources(candidates, resolved)
		// Re-evaluate the corpus fence after the foreign read (#6730 Codex
		// P1): the pre-read fence and this read are separate queries, so a
		// scope that advances its active generation mid-pass would otherwise
		// leave this pass succeeding on a mixed-corpus input that is never
		// reopened. Success now requires the fence to hold across the whole
		// pass. Residual window: a full advance-and-complete cycle inside one
		// pass still needs snapshot isolation across fence and read — a store
		// interface change the owner has not approved — so that narrower
		// shape stays documented here rather than silently accepted.
		fenceStillReady, fenceRecheckErr := corpusResolutionsComplete(ctx, l.ResolutionsCompleteLookup, candidates)
		if fenceRecheckErr != nil {
			return nil, nil, workloadMaterializationResolutionNotReadyError{
				scopeID:      intent.ScopeID,
				generationID: intent.GenerationID,
				cause:        fenceRecheckErr,
			}
		}
		if !fenceStillReady {
			return nil, nil, workloadMaterializationResolutionNotReadyError{
				scopeID:         intent.ScopeID,
				generationID:    intent.GenerationID,
				holdingScopeIDs: maintenance.IncompleteScopeIDs(ctx, l.IncompleteScopesLookup),
			}
		}
	}

	if l.ScopeResolver != nil {
		deploymentEnvironments = l.enrichDeploymentRepoEnvironments(
			ctx, candidates, deploymentEnvironments,
		)
	}

	if len(intent.EntityKeys) > 0 {
		entityKeys, err := deployableUnitCorrelationEntityKeys(intent)
		if err != nil {
			return nil, nil, err
		}
		candidates = filterDeployableUnitCandidates(candidates, entityKeys)
	}

	admitted, err := admittedCorrelatedWorkloadCandidates(intent, candidates)
	if err != nil {
		return nil, nil, err
	}
	return admitted, deploymentEnvironments, nil
}

func loadWorkloadResolvedRelationships(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent Intent,
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
	intent Intent,
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

// resolutionGenerationReady reports whether the relationship generation has
// activated. Lookup errors fail closed; nil preserves isolated test wiring.
func resolutionGenerationReady(
	lookup maintenance.RelationshipGenerationActiveLookup,
	generationID string,
) bool {
	if lookup == nil {
		return true
	}
	active, err := lookup(generationID)
	return err == nil && active
}

// ownResolutionGenerationReady requires the intent's own relationship
// generation before a resolved-relationship read. An inactive own generation
// means both feeds of that read are partial, and a success on that partial
// input is never reopened (#6184), so the intent defers instead. Foreign
// scopes are not knowable until that read, so gating on them would be
// circular. Empty candidate sets are vacuous and do not wait on work they do
// not consume.
func ownResolutionGenerationReady(
	lookup maintenance.RelationshipGenerationActiveLookup,
	intent Intent,
	candidates []WorkloadCandidate,
) bool {
	if len(candidates) == 0 {
		return true
	}
	return resolutionGenerationReady(lookup, intent.GenerationID)
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
	lookup maintenance.RelationshipGenerationsCompleteLookup,
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
