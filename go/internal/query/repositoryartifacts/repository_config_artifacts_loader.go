// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

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

// repositoryConfigFanoutWidth bounds the per-repository loads loadForSources
// runs at once (#7126). Eight matches ReadChunksInFlight, the bound the
// within-repo chunk reader already places on concurrent content-store reads
// (#7962): one repository load can drive up to eight chunk reads, so a
// cross-repo width of eight keeps the usual in-flight statements near the
// width, with short bursts the reader pool absorbs by queueing. It overlaps
// the per-repository list, hydrate and blank-check round trips of a
// 317-repository story without spawning one goroutine per repository.
// Contention with concurrent stories sharing the 30-connection reader pool is
// NOT_CHECKED: keep the width here until a deployed read measures it.
const repositoryConfigFanoutWidth = 8

// sourceLoadOutcome is one repository's per-repository load result: its rows,
// its read statistics, and its error, if any.
type sourceLoadOutcome struct {
	rows      []map[string]any
	readStats contentartifacts.ReadStats
	err       error
}

// loadForSources runs the per-repository config artifact function of the
// neutral package for each source, then merges every repository's rows with
// one dedup map and one total sort. A source with no id or no name is
// skipped. Each repository's allowance is the floor plus what is left of the
// request budget; a repository that takes more than the floor spends the
// difference. The statistics of every repository are summed into stats, also
// when a repository fails.
//
// The loads run in a bounded fan-out of repositoryConfigFanoutWidth workers
// and commit in source order, which keeps the exact sequential contract:
// merged rows in source order and budget accounting in source order. Every
// load first runs optimistically with the widest allowance any source could
// receive (floor plus budget). The ordered commit then adopts a repository's
// optimistic result exactly when it provably equals the sequential result:
// the load succeeded and its candidate pool fits the true sequential
// allowance, so both runs read every candidate and compute the same rows and
// the same counts. Any other repository -- one the true allowance truncates,
// or one whose optimistic load failed -- is re-run sequentially with its
// exact sequential allowance, so its outcome is the sequential outcome. A
// failed repository still contributes its statistics and ends the load with
// no rows, as in the serial walk. The fan-out never cancels the shared
// context on an error: every optimistic load runs to completion, so the first
// error in source order is the reported one, as in the serial walk.
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

	ordered := make([]repositoryArtifactSource, 0, len(sources))
	for _, source := range sources {
		if source.RepoID == "" || source.RepoName == "" {
			continue
		}
		ordered = append(ordered, source)
	}
	if len(ordered) == 0 {
		return nil, nil
	}

	port := contentStoreReader{store: reader}
	optimistic := l.floor + max(l.budget, 0)
	outcomes := make([]sourceLoadOutcome, len(ordered))
	next := make(chan int, len(ordered))
	for i := range ordered {
		next <- i
	}
	close(next)
	var wg sync.WaitGroup
	for w := 0; w < min(max(repositoryConfigFanoutWidth, 1), len(ordered)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				source := ordered[i]
				rows, readStats, err := l.load(
					ctx,
					port,
					contentartifacts.Repository{ID: source.RepoID, Name: source.RepoName},
					filesFromContract(source.Files),
					source.HasFileList,
					optimistic,
				)
				outcomes[i] = sourceLoadOutcome{rows: rows, readStats: readStats, err: err}
			}
		}()
	}
	wg.Wait()

	remaining := l.budget
	perRepository := make([][]map[string]any, 0, len(ordered))
	for i, source := range ordered {
		allowance := l.floor + remaining
		adopted := outcomes[i]
		if adopted.err != nil || adopted.readStats.CandidateCount > allowance {
			rows, readStats, err := l.load(
				ctx,
				port,
				contentartifacts.Repository{ID: source.RepoID, Name: source.RepoName},
				filesFromContract(source.Files),
				source.HasFileList,
				allowance,
			)
			adopted = sourceLoadOutcome{rows: rows, readStats: readStats, err: err}
		}
		stats.addRepository(adopted.readStats)
		remaining = max(remaining-max(adopted.readStats.CandidatesHydrated-l.floor, 0), 0)
		if adopted.err != nil {
			return nil, adopted.err
		}
		perRepository = append(perRepository, adopted.rows)
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
