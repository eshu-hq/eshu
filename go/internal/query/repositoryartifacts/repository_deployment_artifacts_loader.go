// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func LoadDeploymentArtifactOverview(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	repoID string,
	repoName string,
	files []querycontract.FileContent,
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

	configArtifacts, err := LoadSharedRepositoryConfigArtifacts(
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
	content querycontract.ContentStore,
	repoID string,
	files []querycontract.FileContent,
) ([]querycontract.FileContent, error) {
	if content == nil || repoID == "" || len(files) == 0 {
		return files, nil
	}

	return HydrateRepositoryCandidateFiles(ctx, content, repoID, files, func(file querycontract.FileContent) bool {
		return IsDockerComposeArtifact(file) || IsGitHubActionsWorkflowFile(file)
	})
}

func mergeArtifactOverview(overview map[string]any, artifacts map[string]any) map[string]any {
	if len(artifacts) == 0 {
		return overview
	}
	if overview == nil {
		overview = map[string]any{}
	}
	overview["deployment_artifacts"] = MergeDeploymentArtifactMaps(
		querycontract.MapValue(overview, "deployment_artifacts"),
		artifacts,
	)
	return overview
}
