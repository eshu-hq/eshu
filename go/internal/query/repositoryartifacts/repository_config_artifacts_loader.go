// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type repositoryArtifactSource struct {
	RepoID      string
	RepoName    string
	Files       []querycontract.FileContent
	HasFileList bool
}

// LoadSharedRepositoryConfigArtifacts loads the config-artifact rows of the
// anchor repository and of every related repository inside the caller's
// grant, under the production floor and request budget, and merges the
// controller artifacts of the anchor.
func LoadSharedRepositoryConfigArtifacts(
	ctx context.Context,
	graph querycontract.GraphQuery,
	reader querycontract.ContentStore,
	repoID string,
	repoName string,
	files []querycontract.FileContent,
) (map[string]any, error) {
	return productionConfigArtifactLoader.loadShared(ctx, graph, reader, repoID, repoName, files, &ConfigArtifactReadStats{})
}

// loadShared walks the anchor repository first, then the related
// repositories the access filter kept, sorted by RepoID so the budget is
// spent in an order that does not depend on the graph backend (#7951).
func (l configArtifactLoader) loadShared(
	ctx context.Context,
	graph querycontract.GraphQuery,
	reader querycontract.ContentStore,
	repoID string,
	repoName string,
	files []querycontract.FileContent,
	stats *ConfigArtifactReadStats,
) (map[string]any, error) {
	if graph == nil || reader == nil || repoID == "" {
		return nil, nil
	}

	sources := []repositoryArtifactSource{{
		RepoID:      repoID,
		RepoName:    repoName,
		Files:       files,
		HasFileList: true,
	}}

	relatedSources, err := QueryRelatedRepositoryArtifactSources(ctx, graph, repoID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(relatedSources, func(a, b repositoryArtifactSource) int { return cmp.Compare(a.RepoID, b.RepoID) })
	sources = append(sources, relatedSources...)
	stats.RelatedRepositoryCount = len(relatedSources)

	configArtifacts, err := l.loadForSources(ctx, reader, sources, stats)
	if err != nil {
		return nil, err
	}
	controllerArtifacts, err := loadRepositoryControllerArtifacts(ctx, reader, repoID, repoName, files)
	if err != nil {
		return nil, err
	}
	if len(controllerArtifacts) > 0 {
		configArtifacts = MergeDeploymentArtifactMaps(configArtifacts, controllerArtifacts)
	}
	return configArtifacts, nil
}

func LoadRepositoryControllerArtifacts(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	repoName string,
	files []querycontract.FileContent,
) (map[string]any, error) {
	if reader == nil || repoID == "" {
		return nil, nil
	}

	candidates := files
	if candidates == nil {
		var err error
		candidates, err = reader.ListRepoFiles(ctx, repoID, querycontract.RepositorySemanticEntityLimit)
		if err != nil {
			return nil, fmt.Errorf("list controller artifact files: %w", err)
		}
	}

	hydratedCandidates, err := HydrateRepositoryCandidateFiles(ctx, reader, repoID, candidates, isPotentialControllerArtifact)
	if err != nil {
		return nil, fmt.Errorf("hydrate controller artifact files: %w", err)
	}

	contentFiles := append([]querycontract.FileContent(nil), hydratedCandidates...)

	return BuildRepositoryControllerArtifacts(repoName, contentFiles), nil
}

// QueryRelatedRepositoryArtifactSources walks every DEPENDS_ON/USES_MODULE/...
// relationship touching repoID and returns each related repository as a
// candidate config/controller artifact source. This traversal has no anchor
// label predicate, so an unfiltered related repository can belong to a
// different tenant than the caller's grant.
//
// #5167 W3 P0 (third round): LoadSharedRepositoryConfigArtifacts fetches and
// merges each returned source's files (source_repo names, config paths) into
// deployment_evidence/deployment_artifacts/infrastructure_overview whenever
// loadServiceDeploymentEvidence falls through to the
// LoadDeploymentArtifactOverview fallback -- which happens exactly when the
// grant-bound EvidenceArtifact evidence set (filterDeploymentEvidenceRowsForAccess)
// comes back empty. Binding the caller's grant here, before any related
// source's files are ever fetched, is the single point that closes that
// fallback leak for every LoadDeploymentArtifactOverview caller (config,
// controller; the workflow/runtime/cloudformation loaders never leave the
// anchor repo, so they need no filter of their own).
func QueryRelatedRepositoryArtifactSources(
	ctx context.Context,
	graph querycontract.GraphQuery,
	repoID string,
) ([]repositoryArtifactSource, error) {
	rows, err := graph.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(related:Repository)
		RETURN related.id AS repo_id, related.name AS repo_name
		UNION
		MATCH (related:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(r:Repository {id: $repo_id})
		RETURN related.id AS repo_id, related.name AS repo_name
	`, map[string]any{"repo_id": repoID})
	if err != nil {
		return nil, fmt.Errorf("query related repository artifact sources: %w", err)
	}

	seen := map[string]struct{}{}
	sources := make([]repositoryArtifactSource, 0, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(querycontract.StringVal(row, "repo_id"))
		name := strings.TrimSpace(querycontract.StringVal(row, "repo_name"))
		if id == "" || name == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		sources = append(sources, repositoryArtifactSource{
			RepoID:   id,
			RepoName: name,
		})
	}
	return FilterRepositoryArtifactSourcesForAccess(sources, querycontract.RepositoryAccessFilterFromContext(ctx)), nil
}

// FilterRepositoryArtifactSourcesForAccess drops related-repository artifact
// sources outside the caller's grant (#5167 W3 P0, third round). repoID is
// always non-empty here (QueryRelatedRepositoryArtifactSources already
// requires both repo_id and repo_name to be non-empty before building a
// source), so the deny-by-default empty-repoID case the other #5167 W3
// filters apply never arises for this row shape.
func FilterRepositoryArtifactSourcesForAccess(
	sources []repositoryArtifactSource,
	access querycontract.RepositoryAccessFilter,
) []repositoryArtifactSource {
	if !access.Scoped() {
		return sources
	}
	filtered := make([]repositoryArtifactSource, 0, len(sources))
	for _, source := range sources {
		if access.AllowsRepositoryID(source.RepoID) {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

// loadRepositoryConfigArtifactsForSources runs the production loader over
// sources in the given order.
func loadRepositoryConfigArtifactsForSources(
	ctx context.Context,
	reader querycontract.ContentStore,
	sources []repositoryArtifactSource,
	stats *ConfigArtifactReadStats,
) (map[string]any, error) {
	return productionConfigArtifactLoader.loadForSources(ctx, reader, sources, stats)
}

// loadForSources runs the per-repository config artifact function of the
// neutral package for each source in order, then merges every repository's
// rows with one dedup map and one total sort. A source with no id or no name
// is skipped. Each repository's allowance is the floor plus what is left of
// the request budget; a repository that takes more than the floor spends the
// difference. The statistics of every repository are summed into stats, also
// when a repository fails.
func (l configArtifactLoader) loadForSources(
	ctx context.Context,
	reader querycontract.ContentStore,
	sources []repositoryArtifactSource,
	stats *ConfigArtifactReadStats,
) (map[string]any, error) {
	stats.HydrationFloor = l.floor
	stats.HydrationBudget = l.budget
	if reader == nil || len(sources) == 0 {
		return nil, nil
	}

	port := contentStoreReader{store: reader}
	remaining := l.budget
	perRepository := make([][]map[string]any, 0, len(sources))
	for _, source := range sources {
		if source.RepoID == "" || source.RepoName == "" {
			continue
		}
		rows, readStats, err := l.load(
			ctx,
			port,
			contentartifacts.Repository{ID: source.RepoID, Name: source.RepoName},
			filesFromContract(source.Files),
			source.HasFileList,
			l.floor+remaining,
		)
		stats.addRepository(readStats)
		remaining = max(remaining-max(readStats.CandidatesHydrated-l.floor, 0), 0)
		if err != nil {
			return nil, err
		}
		perRepository = append(perRepository, rows)
	}

	merged := contentartifacts.MergeConfigPaths(perRepository...)
	if len(merged) == 0 {
		return nil, nil
	}
	return map[string]any{contentartifacts.ConfigPathsKey: merged}, nil
}

// loadRepositoryControllerArtifacts keeps the in-package spelling after the
// #6060 export; root tests name LoadRepositoryControllerArtifacts.
func loadRepositoryControllerArtifacts(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	repoName string,
	files []querycontract.FileContent,
) (map[string]any, error) {
	return LoadRepositoryControllerArtifacts(ctx, reader, repoID, repoName, files)
}
