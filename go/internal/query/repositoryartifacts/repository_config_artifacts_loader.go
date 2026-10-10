// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type repositoryArtifactSource struct {
	RepoID      string
	RepoName    string
	Files       []querycontract.FileContent
	HasFileList bool
}

func LoadSharedRepositoryConfigArtifacts(
	ctx context.Context,
	graph querycontract.GraphQuery,
	reader querycontract.ContentStore,
	repoID string,
	repoName string,
	files []querycontract.FileContent,
) (map[string]any, error) {
	return loadSharedRepositoryConfigArtifacts(ctx, graph, reader, repoID, repoName, files, &ConfigArtifactReadStats{})
}

func loadSharedRepositoryConfigArtifacts(
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
	sources = append(sources, relatedSources...)
	stats.RelatedRepositoryCount = len(relatedSources)

	configArtifacts, err := loadRepositoryConfigArtifactsForSources(ctx, reader, sources, stats, productionConfigArtifactPasses)
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

// configArtifactPasses are the two per-repository steps of the config-artifact
// load: choose and read the files, then extract rows from them. Production
// wires hydrateConfigArtifactFiles and buildRepositoryConfigArtifacts. The
// equality tests substitute a pass that reads every candidate in full, and a
// builder that reads content a new way, and compare through this same loader.
type configArtifactPasses struct {
	hydrate configFileHydrator
	build   func(repoName string, files []querycontract.FileContent) map[string]any
}

var productionConfigArtifactPasses = configArtifactPasses{
	hydrate: hydrateConfigArtifactFiles,
	build:   buildRepositoryConfigArtifacts,
}

// configFileHydrator returns the files BuildRepositoryConfigArtifacts should
// see for one repository: candidates with non-blank content.
type configFileHydrator func(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	files []querycontract.FileContent,
	stats *ConfigArtifactReadStats,
) ([]querycontract.FileContent, error)

func loadRepositoryConfigArtifactsForSources(
	ctx context.Context,
	reader querycontract.ContentStore,
	sources []repositoryArtifactSource,
	stats *ConfigArtifactReadStats,
	passes configArtifactPasses,
) (map[string]any, error) {
	if reader == nil || len(sources) == 0 {
		return nil, nil
	}

	rows := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, source := range sources {
		if source.RepoID == "" || source.RepoName == "" {
			continue
		}

		files := source.Files
		if !source.HasFileList {
			var err error
			listStarted := time.Now()
			files, err = reader.ListRepoFiles(ctx, source.RepoID, querycontract.RepositorySemanticEntityLimit)
			stats.ListDuration += time.Since(listStarted)
			if err != nil {
				return nil, fmt.Errorf("list config artifact files for %q: %w", source.RepoID, err)
			}
		}
		stats.FilesListed += len(files)

		hydrateStarted := time.Now()
		contentFiles, err := passes.hydrate(ctx, reader, source.RepoID, files, stats)
		stats.HydrateDuration += time.Since(hydrateStarted)
		if err != nil {
			return nil, fmt.Errorf("hydrate config artifact files for %q: %w", source.RepoID, err)
		}

		parseStarted := time.Now()
		artifacts := passes.build(source.RepoName, contentFiles)
		stats.ParseDuration += time.Since(parseStarted)
		for _, row := range querycontract.MapSliceValue(artifacts, "config_paths") {
			key := strings.Join([]string{
				querycontract.StringVal(row, "path"),
				querycontract.StringVal(row, "source_repo"),
				querycontract.StringVal(row, "relative_path"),
				querycontract.StringVal(row, "evidence_kind"),
			}, "|")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, row)
		}
	}

	if len(rows) > 0 {
		sort.Slice(rows, func(i, j int) bool {
			leftPath := querycontract.StringVal(rows[i], "path")
			rightPath := querycontract.StringVal(rows[j], "path")
			if leftPath != rightPath {
				return leftPath < rightPath
			}
			leftRepo := querycontract.StringVal(rows[i], "source_repo")
			rightRepo := querycontract.StringVal(rows[j], "source_repo")
			if leftRepo != rightRepo {
				return leftRepo < rightRepo
			}
			return querycontract.StringVal(rows[i], "relative_path") < querycontract.StringVal(rows[j], "relative_path")
		})
	}

	if len(rows) == 0 {
		return nil, nil
	}

	sort.Slice(rows, func(i, j int) bool {
		leftPath := querycontract.StringVal(rows[i], "path")
		rightPath := querycontract.StringVal(rows[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		leftRepo := querycontract.StringVal(rows[i], "source_repo")
		rightRepo := querycontract.StringVal(rows[j], "source_repo")
		if leftRepo != rightRepo {
			return leftRepo < rightRepo
		}
		return querycontract.StringVal(rows[i], "relative_path") < querycontract.StringVal(rows[j], "relative_path")
	})

	return map[string]any{"config_paths": rows}, nil
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
