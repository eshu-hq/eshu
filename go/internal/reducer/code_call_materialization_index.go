package reducer

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type codeEntityIndex struct {
	entitiesByPathLine map[string]string
	spansByPath        map[string][]codeFunctionSpan
	uniqueNameByPath   map[string]map[string]string
	uniqueNameByRepo   map[string]map[string]string
	entityFileByID     map[string]string
}

type codeFunctionSpan struct {
	startLine int
	endLine   int
	entityID  string
}

func buildCodeEntityIndex(envelopes []facts.Envelope) codeEntityIndex {
	index := codeEntityIndex{
		entitiesByPathLine: make(map[string]string),
		spansByPath:        make(map[string][]codeFunctionSpan),
		uniqueNameByPath:   make(map[string]map[string]string),
		uniqueNameByRepo:   make(map[string]map[string]string),
		entityFileByID:     make(map[string]string),
	}
	nameCandidates := make(map[string]map[string]map[string]struct{})
	repoNameCandidates := make(map[string]map[string]map[string]struct{})

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}

		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}

		relativePath := payloadStr(env.Payload, "relative_path")
		rawPath := anyToString(fileData["path"])
		repositoryID := payloadStr(env.Payload, "repo_id")
		preferredPath := codeCallPreferredPath(rawPath, relativePath)
		for _, item := range mapSlice(fileData["functions"]) {
			entityID := anyToString(item["uid"])
			startLine := codeCallInt(item["line_number"], item["start_line"])
			endLine := codeCallInt(item["end_line"])
			if entityID == "" || startLine <= 0 {
				continue
			}
			if endLine < startLine {
				endLine = startLine
			}
			if preferredPath != "" {
				index.entityFileByID[entityID] = preferredPath
			}
			for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
				index.entitiesByPathLine[codeCallPathLineKey(pathKey, startLine)] = entityID
				index.spansByPath[pathKey] = append(index.spansByPath[pathKey], codeFunctionSpan{
					startLine: startLine,
					endLine:   endLine,
					entityID:  entityID,
				})
				for _, candidateName := range codeCallFunctionCandidateNames(item) {
					if _, ok := nameCandidates[pathKey]; !ok {
						nameCandidates[pathKey] = make(map[string]map[string]struct{})
					}
					if _, ok := nameCandidates[pathKey][candidateName]; !ok {
						nameCandidates[pathKey][candidateName] = make(map[string]struct{})
					}
					nameCandidates[pathKey][candidateName][entityID] = struct{}{}
					if repositoryID != "" {
						if _, ok := repoNameCandidates[repositoryID]; !ok {
							repoNameCandidates[repositoryID] = make(map[string]map[string]struct{})
						}
						if _, ok := repoNameCandidates[repositoryID][candidateName]; !ok {
							repoNameCandidates[repositoryID][candidateName] = make(map[string]struct{})
						}
						repoNameCandidates[repositoryID][candidateName][entityID] = struct{}{}
					}
				}
			}
		}
		for _, item := range mapSlice(fileData["classes"]) {
			entityID := anyToString(item["uid"])
			if entityID == "" {
				continue
			}
			if preferredPath != "" {
				index.entityFileByID[entityID] = preferredPath
			}
			for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
				for _, candidateName := range codeCallTypeCandidateNames(item) {
					if _, ok := nameCandidates[pathKey]; !ok {
						nameCandidates[pathKey] = make(map[string]map[string]struct{})
					}
					if _, ok := nameCandidates[pathKey][candidateName]; !ok {
						nameCandidates[pathKey][candidateName] = make(map[string]struct{})
					}
					nameCandidates[pathKey][candidateName][entityID] = struct{}{}
					if repositoryID != "" {
						if _, ok := repoNameCandidates[repositoryID]; !ok {
							repoNameCandidates[repositoryID] = make(map[string]map[string]struct{})
						}
						if _, ok := repoNameCandidates[repositoryID][candidateName]; !ok {
							repoNameCandidates[repositoryID][candidateName] = make(map[string]struct{})
						}
						repoNameCandidates[repositoryID][candidateName][entityID] = struct{}{}
					}
				}
			}
		}
	}

	for pathKey, spans := range index.spansByPath {
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].startLine == spans[j].startLine {
				return spans[i].endLine < spans[j].endLine
			}
			return spans[i].startLine < spans[j].startLine
		})
		index.spansByPath[pathKey] = spans
	}
	for pathKey, names := range nameCandidates {
		index.uniqueNameByPath[pathKey] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				index.uniqueNameByPath[pathKey][name] = entityID
			}
		}
	}
	for repositoryID, names := range repoNameCandidates {
		index.uniqueNameByRepo[repositoryID] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				index.uniqueNameByRepo[repositoryID][name] = entityID
			}
		}
	}
	return index
}

func resolveCodeEntityID(index codeEntityIndex, pathValue any, lineValue any) string {
	line := codeCallInt(lineValue)
	if line <= 0 {
		return ""
	}

	for _, pathKey := range codeCallPathKeys(anyToString(pathValue), "") {
		if entityID := index.entitiesByPathLine[codeCallPathLineKey(pathKey, line)]; entityID != "" {
			return entityID
		}
	}
	return ""
}

func extractSCIPCodeCallRows(
	repositoryID string,
	entityIndex codeEntityIndex,
	seenRows map[string]struct{},
	fileData map[string]any,
) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, edge := range mapSlice(fileData["function_calls_scip"]) {
		callerID := resolveCodeEntityID(entityIndex, edge["caller_file"], edge["caller_line"])
		calleeID := resolveCodeEntityID(entityIndex, edge["callee_file"], edge["callee_line"])
		if callerID == "" || calleeID == "" {
			continue
		}

		key := repositoryID + "|" + callerID + "|" + calleeID + "|" + fmt.Sprintf("%d", codeCallInt(edge["ref_line"]))
		if _, exists := seenRows[key]; exists {
			continue
		}
		seenRows[key] = struct{}{}

		row := map[string]any{
			"repo_id":          repositoryID,
			"caller_entity_id": callerID,
			"callee_entity_id": calleeID,
			"action":           IntentActionUpsert,
		}
		copyOptionalCodeCallField(row, edge, "caller_symbol")
		copyOptionalCodeCallField(row, edge, "callee_symbol")
		copyOptionalCodeCallField(row, edge, "caller_file")
		copyOptionalCodeCallField(row, edge, "callee_file")
		copyOptionalCodeCallField(row, edge, "ref_line")
		rows = append(rows, row)
	}
	return rows
}

func extractGenericCodeCallRows(
	repositoryID string,
	relativePath string,
	rawPath string,
	entityIndex codeEntityIndex,
	repositoryImports map[string][]string,
	seenRows map[string]struct{},
	fileData map[string]any,
) []map[string]any {
	rows := make([]map[string]any, 0)
	callerFilePath := codeCallPreferredPath(rawPath, relativePath)
	for _, edge := range mapSlice(fileData["function_calls"]) {
		callLine := codeCallInt(edge["line_number"], edge["ref_line"])
		if callLine <= 0 {
			continue
		}
		callerID := resolveContainingCodeEntityID(entityIndex, rawPath, relativePath, callLine)
		if callerID == "" {
			continue
		}
		calleeID, calleeFilePath := resolveGenericCallee(
			entityIndex,
			repositoryID,
			repositoryImports,
			rawPath,
			relativePath,
			fileData,
			edge,
		)
		if calleeID == "" {
			continue
		}

		key := repositoryID + "|" + callerID + "|" + calleeID + "|" + fmt.Sprintf("%d", callLine)
		if _, exists := seenRows[key]; exists {
			continue
		}
		seenRows[key] = struct{}{}

		row := map[string]any{
			"repo_id":          repositoryID,
			"caller_entity_id": callerID,
			"callee_entity_id": calleeID,
			"caller_file":      callerFilePath,
			"callee_file":      calleeFilePath,
			"ref_line":         callLine,
			"action":           IntentActionUpsert,
		}
		copyOptionalCodeCallField(row, edge, "full_name")
		copyOptionalCodeCallField(row, edge, "call_kind")
		rows = append(rows, row)
	}
	return rows
}

func resolveContainingCodeEntityID(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	line int,
) string {
	var (
		bestEntityID string
		bestWidth    int
	)
	for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
		for _, span := range index.spansByPath[pathKey] {
			if line < span.startLine || line > span.endLine {
				continue
			}
			width := span.endLine - span.startLine
			if bestEntityID == "" || width < bestWidth {
				bestEntityID = span.entityID
				bestWidth = width
			}
		}
		if bestEntityID != "" {
			return bestEntityID
		}
	}
	return ""
}
