package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// CorrelatedWorkloadProjectionInputLoader reuses deployable-unit correlation
// semantics as the authoritative gate for workload materialization inputs.
type CorrelatedWorkloadProjectionInputLoader struct {
	FactLoader     FactLoader
	ResolvedLoader ResolvedRelationshipLoader
	ScopeResolver  DeploymentRepoScopeResolver
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
	if l.ResolvedLoader != nil {
		resolved, err := loadWorkloadResolvedRelationships(ctx, l.ResolvedLoader, intent, candidates)
		if err != nil {
			return nil, nil, fmt.Errorf("load resolved relationships for correlated workload projection: %w", err)
		}
		candidates = applyResolvedDeploymentSources(candidates, resolved)
		candidates = applyResolvedProvisioningSources(candidates, resolved)
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
	return fmt.Sprintf("%s|%s|%s|%s|%s",
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

// enrichDeploymentRepoEnvironments loads facts from deployment repos that are
// not the source repo, extracts overlay environments, and merges them into the
// deploymentEnvironments map. This enables cross-repo environment resolution
// when the source repo is deployed via a separate helm-charts/argocd repo.
func (l CorrelatedWorkloadProjectionInputLoader) enrichDeploymentRepoEnvironments(
	ctx context.Context,
	candidates []WorkloadCandidate,
	deploymentEnvironments map[string][]string,
) map[string][]string {
	// Collect unique deployment repo IDs that differ from the source repo
	// and don't already have environments.
	needed := make(map[string]struct{})
	sourceRepos := make(map[string]struct{})
	for _, c := range candidates {
		sourceRepos[c.RepoID] = struct{}{}
	}
	for _, c := range candidates {
		deploymentRepoIDs := candidateDeploymentRepoIDs(c)
		if len(deploymentRepoIDs) == 0 {
			for _, repoID := range c.ProvisioningRepoIDs {
				markEnvironmentRepoNeeded(repoID, sourceRepos, deploymentEnvironments, needed)
			}
			continue
		}
		for _, repoID := range deploymentRepoIDs {
			markEnvironmentRepoNeeded(repoID, sourceRepos, deploymentEnvironments, needed)
		}
		for _, repoID := range c.ProvisioningRepoIDs {
			markEnvironmentRepoNeeded(repoID, sourceRepos, deploymentEnvironments, needed)
		}
	}
	if len(needed) == 0 {
		return deploymentEnvironments
	}

	repoIDs := make([]string, 0, len(needed))
	for id := range needed {
		repoIDs = append(repoIDs, id)
	}

	identities, err := l.ScopeResolver.ResolveRepoActiveGenerations(ctx, repoIDs)
	if err != nil {
		slog.Warn("resolve deployment repo active generations",
			"error", err, "repo_ids", repoIDs)
		return deploymentEnvironments
	}

	for repoID, identity := range identities {
		envelopes, err := loadFactsForKinds(
			ctx,
			l.FactLoader,
			identity.ScopeID,
			identity.GenerationID,
			[]string{factKindFile},
		)
		if err != nil {
			slog.Warn("load deployment repo facts for environment extraction",
				"error", err, "repo_id", repoID,
				"scope_id", identity.ScopeID, "generation_id", identity.GenerationID)
			continue
		}
		envs := ExtractOverlayEnvironmentsFromEnvelopes(envelopes)
		for envRepoID, environments := range envs {
			if _, exists := deploymentEnvironments[envRepoID]; !exists {
				deploymentEnvironments[envRepoID] = environments
			}
		}
	}

	return deploymentEnvironments
}

func markEnvironmentRepoNeeded(
	repoID string,
	sourceRepos map[string]struct{},
	deploymentEnvironments map[string][]string,
	needed map[string]struct{},
) {
	if repoID == "" {
		return
	}
	if _, isSameRepo := sourceRepos[repoID]; isSameRepo {
		return
	}
	if _, hasEnvs := deploymentEnvironments[repoID]; hasEnvs {
		return
	}
	needed[repoID] = struct{}{}
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
