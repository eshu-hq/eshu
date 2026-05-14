package query

import (
	"context"
)

func loadDeploymentArtifactOverview(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	repoID string,
	repoName string,
	files []FileContent,
	infrastructure []map[string]any,
	overview map[string]any,
) (map[string]any, error) {
	merged := overview
	var firstErr error
	artifactFiles := files

	if len(files) > 0 {
		hydratedFiles, err := hydrateRepositoryArtifactFiles(ctx, content, repoID, files)
		if err != nil && firstErr == nil {
			firstErr = err
		} else {
			artifactFiles = hydratedFiles
		}
	}

	configArtifacts, err := loadSharedRepositoryConfigArtifacts(
		ctx,
		graph,
		content,
		repoID,
		repoName,
		artifactFiles,
	)
	if err != nil && firstErr == nil {
		firstErr = err
	} else {
		merged = mergeArtifactOverview(merged, configArtifacts)
	}

	cloudFormationArtifacts := buildRepositoryCloudFormationRuntimeArtifacts(infrastructure)
	merged = mergeArtifactOverview(merged, cloudFormationArtifacts)

	runtimeArtifacts, err := loadRepositoryRuntimeArtifacts(ctx, content, repoID, artifactFiles)
	if err != nil && firstErr == nil {
		firstErr = err
	} else {
		merged = mergeArtifactOverview(merged, runtimeArtifacts)
	}

	workflowArtifacts, err := loadRepositoryWorkflowArtifacts(ctx, content, repoID, artifactFiles)
	if err != nil && firstErr == nil {
		firstErr = err
	} else {
		merged = mergeArtifactOverview(merged, workflowArtifacts)
	}

	return merged, firstErr
}

func hydrateRepositoryArtifactFiles(
	ctx context.Context,
	content ContentStore,
	repoID string,
	files []FileContent,
) ([]FileContent, error) {
	if content == nil || repoID == "" || len(files) == 0 {
		return files, nil
	}

	return hydrateRepositoryCandidateFiles(ctx, content, repoID, files, func(file FileContent) bool {
		return isDockerComposeArtifact(file) || isGitHubActionsWorkflowFile(file)
	})
}

func mergeArtifactOverview(overview map[string]any, artifacts map[string]any) map[string]any {
	if len(artifacts) == 0 {
		return overview
	}
	if overview == nil {
		overview = map[string]any{}
	}
	overview["deployment_artifacts"] = mergeDeploymentArtifactMaps(
		mapValue(overview, "deployment_artifacts"),
		artifacts,
	)
	return overview
}
