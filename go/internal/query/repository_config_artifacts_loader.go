package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type repositoryArtifactSource struct {
	RepoID      string
	RepoName    string
	Files       []FileContent
	HasFileList bool
}

func loadSharedRepositoryConfigArtifacts(
	ctx context.Context,
	graph GraphQuery,
	reader ContentStore,
	repoID string,
	repoName string,
	files []FileContent,
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

	relatedSources, err := queryRelatedRepositoryArtifactSources(ctx, graph, repoID)
	if err != nil {
		return nil, err
	}
	sources = append(sources, relatedSources...)

	configArtifacts, err := loadRepositoryConfigArtifactsForSources(ctx, reader, sources)
	if err != nil {
		return nil, err
	}
	controllerArtifacts, err := loadRepositoryControllerArtifacts(ctx, reader, repoID, repoName, files)
	if err != nil {
		return nil, err
	}
	if len(controllerArtifacts) > 0 {
		configArtifacts = mergeDeploymentArtifactMaps(configArtifacts, controllerArtifacts)
	}
	return configArtifacts, nil
}

func loadRepositoryControllerArtifacts(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	repoName string,
	files []FileContent,
) (map[string]any, error) {
	if reader == nil || repoID == "" {
		return nil, nil
	}

	candidates := files
	if candidates == nil {
		var err error
		candidates, err = reader.ListRepoFiles(ctx, repoID, repositorySemanticEntityLimit)
		if err != nil {
			return nil, fmt.Errorf("list controller artifact files: %w", err)
		}
	}

	hydratedCandidates, err := hydrateRepositoryCandidateFiles(ctx, reader, repoID, candidates, isPotentialControllerArtifact)
	if err != nil {
		return nil, fmt.Errorf("hydrate controller artifact files: %w", err)
	}

	contentFiles := append([]FileContent(nil), hydratedCandidates...)

	return buildRepositoryControllerArtifacts(repoName, contentFiles), nil
}

func queryRelatedRepositoryArtifactSources(
	ctx context.Context,
	graph GraphQuery,
	repoID string,
) ([]repositoryArtifactSource, error) {
	rows, err := graph.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(related:Repository)
		RETURN related.id AS repo_id, related.name AS repo_name
		UNION
		MATCH (related:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(r:Repository {id: $repo_id})
		RETURN related.id AS repo_id, related.name AS repo_name
	`, map[string]any{"repo_id": repoID})
	if err != nil {
		return nil, fmt.Errorf("query related repository artifact sources: %w", err)
	}

	seen := map[string]struct{}{}
	sources := make([]repositoryArtifactSource, 0, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(StringVal(row, "repo_id"))
		name := strings.TrimSpace(StringVal(row, "repo_name"))
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
	return sources, nil
}

func loadRepositoryConfigArtifactsForSources(
	ctx context.Context,
	reader ContentStore,
	sources []repositoryArtifactSource,
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
			files, err = reader.ListRepoFiles(ctx, source.RepoID, repositorySemanticEntityLimit)
			if err != nil {
				return nil, fmt.Errorf("list config artifact files for %q: %w", source.RepoID, err)
			}
		}

		hydratedFiles, err := hydrateRepositoryCandidateFiles(ctx, reader, source.RepoID, files, isConfigArtifactCandidate)
		if err != nil {
			return nil, fmt.Errorf("hydrate config artifact files for %q: %w", source.RepoID, err)
		}

		contentFiles := make([]FileContent, 0, len(hydratedFiles))
		for _, file := range hydratedFiles {
			if !isConfigArtifactCandidate(file) {
				continue
			}
			if strings.TrimSpace(file.Content) == "" {
				continue
			}
			contentFiles = append(contentFiles, file)
		}

		artifacts := buildRepositoryConfigArtifacts(source.RepoName, contentFiles)
		for _, row := range mapSliceValue(artifacts, "config_paths") {
			key := strings.Join([]string{
				StringVal(row, "path"),
				StringVal(row, "source_repo"),
				StringVal(row, "relative_path"),
				StringVal(row, "evidence_kind"),
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
			leftPath := StringVal(rows[i], "path")
			rightPath := StringVal(rows[j], "path")
			if leftPath != rightPath {
				return leftPath < rightPath
			}
			leftRepo := StringVal(rows[i], "source_repo")
			rightRepo := StringVal(rows[j], "source_repo")
			if leftRepo != rightRepo {
				return leftRepo < rightRepo
			}
			return StringVal(rows[i], "relative_path") < StringVal(rows[j], "relative_path")
		})
	}

	if len(rows) == 0 {
		return nil, nil
	}

	sort.Slice(rows, func(i, j int) bool {
		leftPath := StringVal(rows[i], "path")
		rightPath := StringVal(rows[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		leftRepo := StringVal(rows[i], "source_repo")
		rightRepo := StringVal(rows[j], "source_repo")
		if leftRepo != rightRepo {
			return leftRepo < rightRepo
		}
		return StringVal(rows[i], "relative_path") < StringVal(rows[j], "relative_path")
	})

	return map[string]any{"config_paths": rows}, nil
}
