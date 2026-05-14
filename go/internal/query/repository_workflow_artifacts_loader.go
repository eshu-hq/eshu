package query

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func buildRepositoryWorkflowArtifacts(files []FileContent) map[string]any {
	artifacts := make([]map[string]any, 0)
	for _, file := range files {
		if !isGitHubActionsWorkflowFile(file) {
			continue
		}

		row := map[string]any{
			"relative_path": file.RelativePath,
			"artifact_type": "github_actions_workflow",
			"workflow_name": workflowArtifactName(file.RelativePath),
			"signals":       []string{"workflow_file"},
		}
		enrichWorkflowArtifactRow(row, file.Content)
		artifacts = append(artifacts, row)
	}
	if len(artifacts) == 0 {
		return nil
	}
	return map[string]any{"workflow_artifacts": artifacts}
}

func loadRepositoryWorkflowArtifacts(
	ctx context.Context,
	reader ContentStore,
	repoID string,
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
			return nil, fmt.Errorf("list workflow artifact files: %w", err)
		}
	}

	hydratedCandidates, err := hydrateRepositoryCandidateFiles(ctx, reader, repoID, candidates, isGitHubActionsWorkflowFile)
	if err != nil {
		return nil, fmt.Errorf("hydrate workflow artifact files: %w", err)
	}

	contentFiles := make([]FileContent, 0, len(hydratedCandidates))
	for _, file := range hydratedCandidates {
		if !isGitHubActionsWorkflowFile(file) {
			continue
		}
		if strings.TrimSpace(file.Content) == "" {
			continue
		}
		contentFiles = append(contentFiles, file)
	}
	return buildRepositoryWorkflowArtifacts(contentFiles), nil
}

func isGitHubActionsWorkflowFile(file FileContent) bool {
	if strings.EqualFold(file.ArtifactType, "github_actions_workflow") {
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(file.RelativePath)))
	return strings.Contains(lower, ".github/workflows/") &&
		(strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml"))
}

func workflowArtifactName(relativePath string) string {
	base := filepath.Base(strings.TrimSpace(relativePath))
	if base == "" {
		return ""
	}
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
