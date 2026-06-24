// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func (w *CanonicalNodeWriter) buildRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration {
		return nil
	}
	if !hasRepositoryScopedRetract(mat) {
		return nil
	}
	if mat.DeltaProjection {
		return w.buildDeltaRetractStatements(mat)
	}

	retractParams := map[string]any{
		"repo_id":       mat.RepoID,
		"generation_id": mat.GenerationID,
	}

	filePaths := make([]string, 0, len(mat.Files))
	for _, f := range mat.Files {
		filePaths = append(filePaths, f.Path)
	}
	filePaths = dedupeStringValues(filePaths)
	directoryPaths := make([]string, 0, len(mat.Directories))
	for _, directory := range mat.Directories {
		directoryPaths = append(directoryPaths, directory.Path)
	}
	directoryPaths = dedupeStringValues(directoryPaths)

	stmts := make([]Statement, 0, 3)
	fileRetractCypher := canonicalNodeRetractFilesCypher
	fileRetractParams := retractParams
	if len(filePaths) > 0 {
		fileRetractCypher = canonicalNodeRetractRemovedFilesCypher
		fileRetractParams = map[string]any{
			"repo_id":       mat.RepoID,
			"generation_id": mat.GenerationID,
			"file_paths":    filePaths,
		}
	}
	stmts = append(stmts, Statement{
		Operation:  OperationCanonicalRetract,
		Cypher:     fileRetractCypher,
		Parameters: fileRetractParams,
	})

	if len(filePaths) > 0 {
		for _, cypher := range []string{
			canonicalNodeRefreshCurrentFileImportEdgesCypher,
			canonicalNodeRefreshCurrentDirectoryFileEdgesCypher,
		} {
			stmts = append(stmts, buildStringSliceRetractStatements(
				cypher,
				"file_paths",
				filePaths,
				canonicalNodeRefreshFilePathBatchSize,
			)...)
		}
	}
	stmts = append(stmts, buildEntityContainmentRefreshStatements(mat.Entities, mat.ClassMembers, mat.NestedFuncs)...)

	stmts = append(stmts, Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    canonicalNodeRetractDirectoriesCypher,
		Parameters: map[string]any{
			"repo_id":         mat.RepoID,
			"generation_id":   mat.GenerationID,
			"directory_paths": directoryPaths,
		},
	})

	// Parameter retraction uses file_paths
	if len(filePaths) > 0 {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRetractParametersCypher,
			Parameters: map[string]any{
				"file_paths":    filePaths,
				"generation_id": mat.GenerationID,
			},
		})
	}

	return stmts
}

func (w *CanonicalNodeWriter) buildEntityRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration {
		return nil
	}
	if !hasRepositoryScopedRetract(mat) {
		return nil
	}
	if mat.DeltaProjection {
		return buildDeltaEntityRetractStatements(mat)
	}
	labels := canonicalNodeRetractEntityLabels()
	stmts := make([]Statement, 0, len(labels))
	for _, label := range labels {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    fmt.Sprintf(canonicalNodeRetractEntityTemplate, label),
			Parameters: map[string]any{
				"repo_id":       mat.RepoID,
				"generation_id": mat.GenerationID,
			},
		})
	}
	return stmts
}

func (w *CanonicalNodeWriter) buildDeltaRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	changedFilePaths := dedupeStringValues(canonicalFilePaths(mat.Files))
	deletedFilePaths := dedupeStringValues(mat.DeltaDeletedFilePaths)
	touchedFilePaths := dedupeStringValues(append(append([]string(nil), mat.DeltaFilePaths...), changedFilePaths...))
	touchedFilePaths = appendMissingStrings(touchedFilePaths, deletedFilePaths)

	stmts := make([]Statement, 0, 5)
	if len(deletedFilePaths) > 0 {
		stmts = append(stmts, buildDeltaDeletedFileRetractStatements(mat.RepoID, deletedFilePaths)...)
		stmts = append(stmts, buildDeltaEmptyDirectoryRetractStatements(mat.RepoID, mat.RepoPath, deletedFilePaths)...)
	}
	if len(changedFilePaths) > 0 {
		for _, cypher := range []string{
			canonicalNodeRefreshCurrentFileImportEdgesCypher,
			canonicalNodeRefreshCurrentDirectoryFileEdgesCypher,
		} {
			stmts = append(stmts, buildStringSliceRetractStatements(
				cypher,
				"file_paths",
				changedFilePaths,
				canonicalNodeRefreshFilePathBatchSize,
			)...)
		}
	}
	stmts = append(stmts, buildEntityContainmentRefreshStatements(mat.Entities, mat.ClassMembers, mat.NestedFuncs)...)
	if len(touchedFilePaths) > 0 {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRetractParametersCypher,
			Parameters: map[string]any{
				"file_paths":    touchedFilePaths,
				"generation_id": mat.GenerationID,
			},
		})
	}
	return stmts
}

func buildDeltaDeletedFileRetractStatements(repoID string, filePaths []string) []Statement {
	if len(filePaths) == 0 {
		return nil
	}
	stmts := make([]Statement, 0, (len(filePaths)+canonicalNodeRefreshFilePathBatchSize-1)/canonicalNodeRefreshFilePathBatchSize)
	for start := 0; start < len(filePaths); start += canonicalNodeRefreshFilePathBatchSize {
		end := start + canonicalNodeRefreshFilePathBatchSize
		if end > len(filePaths) {
			end = len(filePaths)
		}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRetractDeltaDeletedFilesCypher,
			Parameters: map[string]any{
				"repo_id":    repoID,
				"file_paths": append([]string(nil), filePaths[start:end]...),
			},
		})
	}
	return stmts
}

func buildDeltaEmptyDirectoryRetractStatements(repoID string, repoPath string, filePaths []string) []Statement {
	pathsByDepth := deletedFileDirectoryPathsByDepth(repoPath, filePaths)
	if len(pathsByDepth) == 0 {
		return nil
	}
	depths := make([]int, 0, len(pathsByDepth))
	for depth := range pathsByDepth {
		depths = append(depths, depth)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(depths)))

	stmts := make([]Statement, 0, len(depths))
	for _, depth := range depths {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRetractDeltaEmptyDirectoriesCypher,
			Parameters: map[string]any{
				"repo_id":         repoID,
				"directory_paths": pathsByDepth[depth],
			},
		})
	}
	return stmts
}

func deletedFileDirectoryPathsByDepth(repoPath string, filePaths []string) map[int][]string {
	repoPath = path.Clean(repoPath)
	seen := make(map[string]struct{})
	byDepth := make(map[int][]string)
	for _, filePath := range filePaths {
		dirPath := path.Dir(path.Clean(filePath))
		for dirPath != "." && dirPath != "/" && dirPath != repoPath {
			if !pathWithinRepo(repoPath, dirPath) {
				break
			}
			if _, ok := seen[dirPath]; ok {
				dirPath = path.Dir(dirPath)
				continue
			}
			seen[dirPath] = struct{}{}
			depth := strings.Count(strings.TrimPrefix(dirPath, repoPath+"/"), "/")
			byDepth[depth] = append(byDepth[depth], dirPath)
			dirPath = path.Dir(dirPath)
		}
	}
	for depth, paths := range byDepth {
		sort.Strings(paths)
		byDepth[depth] = paths
	}
	return byDepth
}

func pathWithinRepo(repoPath string, candidatePath string) bool {
	return candidatePath == repoPath || strings.HasPrefix(candidatePath, repoPath+"/")
}

func buildDeltaEntityRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	filePaths := dedupeStringValues(mat.DeltaFilePaths)
	filePaths = appendMissingStrings(filePaths, canonicalFilePaths(mat.Files))
	filePaths = appendMissingStrings(filePaths, mat.DeltaDeletedFilePaths)
	if len(filePaths) == 0 {
		return nil
	}
	labels := canonicalNodeRetractEntityLabels()
	stmts := make([]Statement, 0, len(labels))
	for _, label := range labels {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    fmt.Sprintf(canonicalNodeRetractDeltaEntityTemplate, label),
			Parameters: map[string]any{
				"repo_id":       mat.RepoID,
				"generation_id": mat.GenerationID,
				"file_paths":    filePaths,
			},
		})
	}
	return stmts
}

func canonicalFilePaths(files []projector.FileRow) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func appendMissingStrings(values []string, more []string) []string {
	if len(more) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values)+len(more))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range more {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func hasRepositoryScopedRetract(mat projector.CanonicalMaterialization) bool {
	return strings.TrimSpace(mat.RepoID) != ""
}

// dedupeStringValues preserves first-seen order so retract chunking stays
// deterministic while positive UNWIND cleanups avoid duplicate deletes.
func dedupeStringValues(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func canonicalNodeRetractEntityLabels() []string {
	labels := make(map[string]struct{})
	for _, family := range []map[string]struct{}{
		canonicalNodeRetractCodeEntityLabels,
		canonicalNodeRetractInfraEntityLabels,
		canonicalNodeRetractTerraformEntityLabels,
		canonicalNodeRetractCloudFormationEntityLabels,
		canonicalNodeRetractSQLEntityLabels,
		canonicalNodeRetractDataEntityLabels,
		canonicalNodeRetractOCIEntityLabels,
		canonicalNodeRetractPackageRegistryEntityLabels,
	} {
		for label := range family {
			labels[label] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(labels))
	for label := range labels {
		sorted = append(sorted, label)
	}
	sort.Strings(sorted)
	return sorted
}

func buildStringSliceRetractStatements(cypher string, paramName string, values []string, batchSize int) []Statement {
	if len(values) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(values)
	}
	stmts := make([]Statement, 0, (len(values)+batchSize-1)/batchSize)
	for start := 0; start < len(values); start += batchSize {
		end := start + batchSize
		if end > len(values) {
			end = len(values)
		}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				paramName: append([]string(nil), values[start:end]...),
			},
		})
	}
	return stmts
}

func buildEntityContainmentRefreshStatements(
	entities []projector.EntityRow,
	classMembers []projector.ClassMemberRow,
	nestedFuncs []projector.NestedFunctionRow,
) []Statement {
	parentChildIDs := make(map[string]map[string]struct{})
	parentLabels := make(map[string]string)
	classIDsByFileName := make(map[string][]string)
	functionIDsByFileName := make(map[string][]string)
	functionIDsByFileNameLine := make(map[string][]string)

	for _, entity := range entities {
		if entity.EntityID == "" {
			continue
		}
		switch entity.Label {
		case "Class":
			parentChildIDs[entity.EntityID] = make(map[string]struct{})
			parentLabels[entity.EntityID] = entity.Label
			classIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)] = append(
				classIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)],
				entity.EntityID,
			)
		case "Function":
			parentChildIDs[entity.EntityID] = make(map[string]struct{})
			parentLabels[entity.EntityID] = entity.Label
			functionIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)] = append(
				functionIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)],
				entity.EntityID,
			)
			functionIDsByFileNameLine[fileNameLineKey(entity.FilePath, entity.EntityName, entity.StartLine)] = append(
				functionIDsByFileNameLine[fileNameLineKey(entity.FilePath, entity.EntityName, entity.StartLine)],
				entity.EntityID,
			)
		}
	}

	for _, classMember := range classMembers {
		childIDs := functionIDsByFileNameLine[fileNameLineKey(classMember.FilePath, classMember.FunctionName, classMember.FunctionLine)]
		if len(childIDs) == 0 {
			continue
		}
		for _, parentID := range classIDsByFileName[fileNameKey(classMember.FilePath, classMember.ClassName)] {
			for _, childID := range childIDs {
				parentChildIDs[parentID][childID] = struct{}{}
			}
		}
	}

	for _, nestedFunc := range nestedFuncs {
		childIDs := functionIDsByFileNameLine[fileNameLineKey(nestedFunc.FilePath, nestedFunc.InnerName, nestedFunc.InnerLine)]
		if len(childIDs) == 0 {
			continue
		}
		for _, parentID := range functionIDsByFileName[fileNameKey(nestedFunc.FilePath, nestedFunc.OuterName)] {
			for _, childID := range childIDs {
				parentChildIDs[parentID][childID] = struct{}{}
			}
		}
	}

	if len(parentChildIDs) == 0 {
		return nil
	}
	parentIDs := make([]string, 0, len(parentChildIDs))
	for parentID := range parentChildIDs {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)

	rowsByLabel := make(map[string][]map[string]any, 2)
	for _, parentID := range parentIDs {
		childIDs := make([]string, 0, len(parentChildIDs[parentID]))
		for childID := range parentChildIDs[parentID] {
			childIDs = append(childIDs, childID)
		}
		sort.Strings(childIDs)
		label := parentLabels[parentID]
		rowsByLabel[label] = append(rowsByLabel[label], map[string]any{
			"parent_entity_id": parentID,
			"child_entity_ids": childIDs,
		})
	}

	labels := make([]string, 0, len(rowsByLabel))
	for label := range rowsByLabel {
		if label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)

	stmts := make([]Statement, 0, len(parentIDs))
	for _, label := range labels {
		stmts = append(
			stmts,
			buildBatchedRetractStatements(
				fmt.Sprintf(canonicalNodeRefreshCurrentEntityContainmentEdgesTemplate, label),
				rowsByLabel[label],
				canonicalNodeRefreshEntityContainmentBatchSize,
			)...,
		)
	}
	return stmts
}

func fileNameKey(filePath, name string) string {
	return filePath + "\x00" + name
}

func fileNameLineKey(filePath, name string, line int) string {
	return fmt.Sprintf("%s\x00%s\x00%d", filePath, name, line)
}

// --- Phase B: Repository ---
