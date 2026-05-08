package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ExtractPythonMetaclassRows builds canonical USES_METACLASS edges from Python
// class metadata emitted by the Go parser path.
func ExtractPythonMetaclassRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	repositoryIDs := collectCodeCallRepositoryIDs(envelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil
	}

	entityIndex := buildCodeEntityIndex(envelopes)
	repositoryImports := collectCodeCallRepositoryImports(envelopes)
	return extractPythonMetaclassRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports)
}

func extractPythonMetaclassRowsWithIndex(
	envelopes []facts.Envelope,
	repositoryIDs []string,
	entityIndex codeEntityIndex,
	repositoryImports map[string]map[string][]string,
) ([]string, []map[string]any) {
	seenRows := make(map[string]struct{})
	rows := make([]map[string]any, 0)

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}

		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}

		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}

		relativePath := payloadStr(env.Payload, "relative_path")
		rawPath := anyToString(fileData["path"])
		sourceFilePath := codeCallPreferredPath(rawPath, relativePath)
		for _, item := range mapSlice(fileData["classes"]) {
			sourceEntityID := strings.TrimSpace(anyToString(item["uid"]))
			if sourceEntityID == "" {
				sourceEntityID = resolvePythonClassEntityID(indexForMetaclass(entityIndex), rawPath, relativePath, item)
			}
			if sourceEntityID == "" {
				continue
			}

			metaclassName := strings.TrimSpace(anyToString(item["metaclass"]))
			if metaclassName == "" {
				continue
			}

			targetEntityID, targetFilePath := resolvePythonMetaclassEntityID(
				entityIndex,
				repositoryID,
				repositoryImports[repositoryID],
				rawPath,
				relativePath,
				fileData,
				metaclassName,
			)
			if targetEntityID == "" || targetEntityID == sourceEntityID {
				continue
			}

			key := repositoryID + "|" + sourceEntityID + "|" + targetEntityID
			if _, exists := seenRows[key]; exists {
				continue
			}
			seenRows[key] = struct{}{}

			row := map[string]any{
				"repo_id":           repositoryID,
				"source_entity_id":  sourceEntityID,
				"target_entity_id":  targetEntityID,
				"source_file":       sourceFilePath,
				"target_file":       targetFilePath,
				"relationship_type": "USES_METACLASS",
				"reason":            "python_metaclass",
				"action":            IntentActionUpsert,
			}
			rows = append(rows, row)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		left := anyToString(rows[i]["source_entity_id"]) + "->" + anyToString(rows[i]["target_entity_id"])
		right := anyToString(rows[j]["source_entity_id"]) + "->" + anyToString(rows[j]["target_entity_id"])
		if left == right {
			return anyToString(rows[i]["repo_id"]) < anyToString(rows[j]["repo_id"])
		}
		return left < right
	})

	return repositoryIDs, rows
}

func resolvePythonClassEntityID(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	class map[string]any,
) string {
	sourceName := strings.TrimSpace(anyToString(class["name"]))
	if sourceName == "" {
		return ""
	}
	for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
		if entityID := index.uniqueNameByPath[pathKey][sourceName]; entityID != "" {
			return entityID
		}
	}
	return ""
}

func resolvePythonMetaclassEntityID(
	index codeEntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	metaclassName string,
) (string, string) {
	callLike := map[string]any{
		"name":      codeCallTrailingName(metaclassName),
		"full_name": metaclassName,
		"lang":      "python",
	}

	if entityID := resolveSameFileCalleeEntityID(index, rawPath, relativePath, callLike); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath)
	}
	for _, name := range codeCallExactCandidateNames(callLike, "python") {
		if entityID := index.uniqueNameByRepo[repositoryID][name]; entityID != "" {
			return entityID, index.entityFileByID[entityID]
		}
	}
	for _, name := range codeCallBroadCandidateNames(callLike, "python") {
		if entityID := index.uniqueNameByRepo[repositoryID][name]; entityID != "" {
			return entityID, index.entityFileByID[entityID]
		}
	}

	return resolveImportedCrossFileCallee(
		index,
		repositoryImports,
		codeCallReexportIndex{},
		repositoryID,
		rawPath,
		relativePath,
		fileData,
		callLike,
	)
}

func indexForMetaclass(index codeEntityIndex) codeEntityIndex {
	return index
}
