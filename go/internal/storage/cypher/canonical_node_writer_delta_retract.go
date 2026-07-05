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

func (w *CanonicalNodeWriter) buildDeltaRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	changedFilePaths := dedupeStringValues(canonicalFilePaths(mat.Files))
	deletedFilePaths := dedupeStringValues(mat.DeltaDeletedFilePaths)
	deletedDirectoryPaths := dedupeStringValues(mat.DeltaDeletedDirectoryPaths)
	touchedFilePaths := dedupeStringValues(append(append([]string(nil), mat.DeltaFilePaths...), changedFilePaths...))
	touchedFilePaths = appendMissingStrings(touchedFilePaths, deletedFilePaths)

	stmts := make([]Statement, 0, 5)
	if len(deletedFilePaths) > 0 {
		stmts = append(stmts, buildDeltaDeletedFileRetractStatements(mat.RepoID, deletedFilePaths)...)
	}
	if len(deletedDirectoryPaths) > 0 {
		stmts = append(stmts, buildDeltaDeletedDirectoryRetractStatements(mat.RepoID, deletedDirectoryPaths)...)
	}
	if len(deletedFilePaths) > 0 || len(deletedDirectoryPaths) > 0 {
		stmts = append(stmts, buildDeltaEmptyDirectoryRetractStatements(
			mat.RepoID,
			mat.RepoPath,
			deletedFilePaths,
			deletedDirectoryPaths,
		)...)
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
	if directoryParentEdgeRefreshRows := currentDirectoryParentEdgeRefreshRows(mat.Directories); len(directoryParentEdgeRefreshRows) > 0 {
		stmts = append(stmts, buildDirectoryParentEdgeRefreshStatements(
			canonicalNodeRefreshCurrentDirectoryParentEdgesCypher,
			mat.RepoID,
			directoryParentEdgeRefreshRows,
			canonicalNodeRefreshFilePathBatchSize,
		)...)
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

func currentDirectoryParentEdgeRefreshRows(directories []projector.DirectoryRow) []map[string]any {
	rows := make([]map[string]any, 0, len(directories))
	seen := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		if directory.Depth <= 0 {
			continue
		}
		if _, ok := seen[directory.Path]; ok {
			continue
		}
		seen[directory.Path] = struct{}{}
		rows = append(rows, map[string]any{
			"path":        directory.Path,
			"parent_path": directory.ParentPath,
		})
	}
	return rows
}

func buildDirectoryParentEdgeRefreshStatements(
	cypher string,
	repoID string,
	rows []map[string]any,
	batchSize int,
) []Statement {
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(rows)
	}
	stmts := make([]Statement, 0, (len(rows)+batchSize-1)/batchSize)
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				"rows":    append([]map[string]any(nil), rows[start:end]...),
				"repo_id": repoID,
			},
		})
	}
	return stmts
}

func buildDeltaDeletedDirectoryRetractStatements(repoID string, directoryPaths []string) []Statement {
	if len(directoryPaths) == 0 {
		return nil
	}
	stmts := make([]Statement, 0, (len(directoryPaths)+canonicalNodeRefreshFilePathBatchSize-1)/canonicalNodeRefreshFilePathBatchSize)
	for start := 0; start < len(directoryPaths); start += canonicalNodeRefreshFilePathBatchSize {
		end := start + canonicalNodeRefreshFilePathBatchSize
		if end > len(directoryPaths) {
			end = len(directoryPaths)
		}
		batch := append([]string(nil), directoryPaths[start:end]...)
		stmts = append(stmts,
			Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    canonicalNodeRetractDeltaDeletedDirectoryEdgesCypher,
				Parameters: map[string]any{
					"repo_id":         repoID,
					"directory_paths": batch,
				},
			},
			Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    canonicalNodeRetractDeltaDeletedDirectoriesCypher,
				Parameters: map[string]any{
					"repo_id":         repoID,
					"directory_paths": batch,
				},
			},
		)
	}
	return stmts
}

func buildDeltaEmptyDirectoryRetractStatements(
	repoID string,
	repoPath string,
	filePaths []string,
	directoryPaths []string,
) []Statement {
	pathsByDepth := deletedFileDirectoryPathsByDepth(repoPath, filePaths, directoryPaths)
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

func deletedFileDirectoryPathsByDepth(repoPath string, filePaths []string, directoryPaths []string) map[int][]string {
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
	for _, directoryPath := range directoryPaths {
		dirPath := path.Clean(directoryPath)
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
