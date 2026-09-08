// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func BuildRepositoryControllerArtifacts(repoName string, files []querycontract.FileContent) map[string]any {
	ansibleMetadata := collectAnsibleControllerMetadata(files)
	artifacts := make([]map[string]any, 0)
	for _, file := range files {
		metadata := parser.ExtractGroovyPipelineMetadata(file.Content)
		if !isJenkinsGroovyArtifact(file, metadata) {
			continue
		}
		row := map[string]any{
			"path":            CleanRepositoryRelativePath(file.RelativePath),
			"source_repo":     repoName,
			"relative_path":   file.RelativePath,
			"controller_kind": "jenkins_pipeline",
		}

		if sharedLibraries := querycontract.StringSliceValue(metadata, "shared_libraries"); len(sharedLibraries) > 0 {
			row["shared_libraries"] = sharedLibraries
		}
		if pipelineCalls := querycontract.StringSliceValue(metadata, "pipeline_calls"); len(pipelineCalls) > 0 {
			row["pipeline_calls"] = pipelineCalls
		}
		if entryPoints := querycontract.StringSliceValue(metadata, "entry_points"); len(entryPoints) > 0 {
			row["entry_points"] = entryPoints
		}
		if shellCommands := querycontract.StringSliceValue(metadata, "shell_commands"); len(shellCommands) > 0 {
			row["shell_commands"] = shellCommands
		}
		if hints := querycontract.MapSliceValue(metadata, "ansible_playbook_hints"); len(hints) > 0 {
			row["ansible_playbook_hints"] = hints
		}
		querycontract.CopyStringSliceField(row, ansibleMetadata, "ansible_inventories")
		querycontract.CopyStringSliceField(row, ansibleMetadata, "ansible_var_files")
		querycontract.CopyStringSliceField(row, ansibleMetadata, "ansible_task_entrypoints")
		querycontract.CopyStringSliceField(row, ansibleMetadata, "ansible_role_paths")
		if useConfigd, ok := metadata["use_configd"].(bool); ok {
			row["use_configd"] = useConfigd
		}
		if hasPreDeploy, ok := metadata["has_pre_deploy"].(bool); ok {
			row["has_pre_deploy"] = hasPreDeploy
		}

		artifacts = append(artifacts, row)
	}

	if len(artifacts) == 0 {
		return nil
	}

	sort.SliceStable(artifacts, func(i, j int) bool {
		leftPath := querycontract.StringVal(artifacts[i], "path")
		rightPath := querycontract.StringVal(artifacts[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return querycontract.StringVal(artifacts[i], "relative_path") < querycontract.StringVal(artifacts[j], "relative_path")
	})

	return map[string]any{"controller_artifacts": artifacts}
}

func isPotentialControllerArtifact(file querycontract.FileContent) bool {
	base := strings.TrimSpace(file.RelativePath)
	if base == "" {
		return false
	}
	name := strings.ToLower(filepath.Base(base))
	if name == "jenkinsfile" {
		return true
	}
	if strings.HasPrefix(name, "jenkinsfile.") {
		return true
	}
	return strings.HasSuffix(name, ".groovy")
}

func isJenkinsGroovyArtifact(file querycontract.FileContent, metadata map[string]any) bool {
	base := strings.TrimSpace(file.RelativePath)
	if base == "" {
		return false
	}
	name := strings.ToLower(filepath.Base(base))
	if name == "jenkinsfile" {
		return true
	}
	if strings.HasPrefix(name, "jenkinsfile.") {
		return true
	}
	if !strings.HasSuffix(name, ".groovy") {
		return false
	}
	return groovyPipelineMetadataPresent(metadata)
}

func groovyPipelineMetadataPresent(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}
	if values := querycontract.StringSliceValue(metadata, "shared_libraries"); len(values) > 0 {
		return true
	}
	if values := querycontract.StringSliceValue(metadata, "pipeline_calls"); len(values) > 0 {
		return true
	}
	if values := querycontract.StringSliceValue(metadata, "shell_commands"); len(values) > 0 {
		return true
	}
	if values := querycontract.StringSliceValue(metadata, "entry_points"); len(values) > 0 {
		return true
	}
	if useConfigd, ok := metadata["use_configd"].(bool); ok && useConfigd {
		return true
	}
	if hasPreDeploy, ok := metadata["has_pre_deploy"].(bool); ok && hasPreDeploy {
		return true
	}
	return false
}

func collectAnsibleControllerMetadata(files []querycontract.FileContent) map[string]any {
	inventories := make(map[string]struct{})
	varFiles := make(map[string]struct{})
	taskEntrypoints := make(map[string]struct{})
	rolePaths := make(map[string]struct{})

	for _, file := range files {
		relativePath := strings.TrimSpace(file.RelativePath)
		if relativePath == "" {
			continue
		}
		lowerPath := strings.ToLower(CleanRepositoryRelativePath(relativePath))
		lowerBase := strings.ToLower(filepath.Base(relativePath))

		if ansibleInventoryEvidencePath(lowerPath, lowerBase) {
			inventories[CleanRepositoryRelativePath(relativePath)] = struct{}{}
		}
		if ansibleVarsEvidencePath(lowerPath, lowerBase) {
			varFiles[CleanRepositoryRelativePath(relativePath)] = struct{}{}
		}
		if ansibleTaskEntrypointEvidencePath(lowerPath, lowerBase) {
			taskEntrypoints[CleanRepositoryRelativePath(relativePath)] = struct{}{}
		}
		if rolePath := ansibleRoleEvidencePath(lowerPath); rolePath != "" {
			rolePaths[CleanRepositoryRelativePath(rolePath)] = struct{}{}
		}
	}

	metadata := map[string]any{}
	if values := querycontract.SortedSetKeys(inventories); len(values) > 0 {
		metadata["ansible_inventories"] = values
	}
	if values := querycontract.SortedSetKeys(varFiles); len(values) > 0 {
		metadata["ansible_var_files"] = values
	}
	if values := querycontract.SortedSetKeys(taskEntrypoints); len(values) > 0 {
		metadata["ansible_task_entrypoints"] = values
	}
	if values := querycontract.SortedSetKeys(rolePaths); len(values) > 0 {
		metadata["ansible_role_paths"] = values
	}
	return metadata
}
