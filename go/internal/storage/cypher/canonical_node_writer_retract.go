package cypher

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func (w *CanonicalNodeWriter) buildRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration {
		return nil
	}

	retractParams := map[string]any{
		"repo_id":       mat.RepoID,
		"generation_id": mat.GenerationID,
	}

	filePaths := make([]string, len(mat.Files))
	for i, f := range mat.Files {
		filePaths[i] = f.Path
	}
	entityIDsByFamily := map[string][]string{
		canonicalNodeRetractCodeEntitiesCypher:           canonicalEntityIDsForLabels(mat.Entities, canonicalNodeRetractCodeEntityLabels),
		canonicalNodeRetractInfraEntitiesCypher:          canonicalEntityIDsForLabels(mat.Entities, canonicalNodeRetractInfraEntityLabels),
		canonicalNodeRetractTerraformEntitiesCypher:      canonicalEntityIDsForLabels(mat.Entities, canonicalNodeRetractTerraformEntityLabels),
		canonicalNodeRetractCloudFormationEntitiesCypher: canonicalEntityIDsForLabels(mat.Entities, canonicalNodeRetractCloudFormationEntityLabels),
		canonicalNodeRetractSQLEntitiesCypher:            canonicalEntityIDsForLabels(mat.Entities, canonicalNodeRetractSQLEntityLabels),
		canonicalNodeRetractDataEntitiesCypher:           canonicalEntityIDsForLabels(mat.Entities, canonicalNodeRetractDataEntityLabels),
	}
	directoryPaths := make([]string, len(mat.Directories))
	for i, directory := range mat.Directories {
		directoryPaths[i] = directory.Path
	}

	retractions := []string{
		canonicalNodeRetractCodeEntitiesCypher,
		canonicalNodeRetractInfraEntitiesCypher,
		canonicalNodeRetractTerraformEntitiesCypher,
		canonicalNodeRetractCloudFormationEntitiesCypher,
		canonicalNodeRetractSQLEntitiesCypher,
		canonicalNodeRetractDataEntitiesCypher,
		canonicalNodeRetractDirectoriesCypher,
	}

	stmts := make([]Statement, 0, len(retractions)+2)
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
		stmts = append(stmts, buildFileEntityRefreshStatements(mat.Files, mat.Entities)...)
	}
	stmts = append(stmts, buildEntityContainmentRefreshStatements(mat.Entities, mat.ClassMembers, mat.NestedFuncs)...)

	for _, cypher := range retractions {
		params := map[string]any{
			"repo_id":       mat.RepoID,
			"generation_id": mat.GenerationID,
			"entity_ids":    entityIDsByFamily[cypher],
		}
		if cypher == canonicalNodeRetractDirectoriesCypher {
			params = map[string]any{
				"repo_id":         mat.RepoID,
				"generation_id":   mat.GenerationID,
				"directory_paths": directoryPaths,
			}
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalRetract,
			Cypher:     cypher,
			Parameters: params,
		})
	}

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

func canonicalEntityIDsForLabels(entities []projector.EntityRow, labels map[string]struct{}) []string {
	entityIDs := make([]string, 0, len(entities))
	for _, entity := range entities {
		if _, ok := labels[entity.Label]; ok {
			entityIDs = append(entityIDs, entity.EntityID)
		}
	}
	return entityIDs
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

func buildFileEntityRefreshStatements(files []projector.FileRow, entities []projector.EntityRow) []Statement {
	if len(files) == 0 {
		return nil
	}
	entityIDsByFile := make(map[string][]string, len(files))
	seenByFile := make(map[string]map[string]struct{}, len(files))
	for _, entity := range entities {
		if entity.FilePath == "" || entity.EntityID == "" {
			continue
		}
		seen := seenByFile[entity.FilePath]
		if seen == nil {
			seen = make(map[string]struct{})
			seenByFile[entity.FilePath] = seen
		}
		if _, ok := seen[entity.EntityID]; ok {
			continue
		}
		seen[entity.EntityID] = struct{}{}
		entityIDsByFile[entity.FilePath] = append(entityIDsByFile[entity.FilePath], entity.EntityID)
	}

	stmts := make([]Statement, 0, len(files))
	seenFiles := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		if _, ok := seenFiles[file.Path]; ok {
			continue
		}
		seenFiles[file.Path] = struct{}{}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRefreshCurrentFileEntityEdgesCypher,
			Parameters: map[string]any{
				"file_path":  file.Path,
				"entity_ids": append([]string(nil), entityIDsByFile[file.Path]...),
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
			classIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)] = append(
				classIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)],
				entity.EntityID,
			)
		case "Function":
			parentChildIDs[entity.EntityID] = make(map[string]struct{})
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

	rows := make([]map[string]any, 0, len(parentIDs))
	for _, parentID := range parentIDs {
		childIDs := make([]string, 0, len(parentChildIDs[parentID]))
		for childID := range parentChildIDs[parentID] {
			childIDs = append(childIDs, childID)
		}
		sort.Strings(childIDs)
		rows = append(rows, map[string]any{
			"parent_entity_id": parentID,
			"child_entity_ids": childIDs,
		})
	}
	return buildBatchedRetractStatements(canonicalNodeRefreshCurrentEntityContainmentEdgesCypher, rows, canonicalNodeRefreshEntityContainmentBatchSize)
}

func fileNameKey(filePath, name string) string {
	return filePath + "\x00" + name
}

func fileNameLineKey(filePath, name string, line int) string {
	return fmt.Sprintf("%s\x00%s\x00%d", filePath, name, line)
}

// --- Phase B: Repository ---
