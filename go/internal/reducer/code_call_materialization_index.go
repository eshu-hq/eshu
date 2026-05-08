package reducer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type codeEntityIndex struct {
	entitiesByPathLine  map[string]string
	spansByPath         map[string][]codeFunctionSpan
	uniqueNameByPath    map[string]map[string]string
	uniqueNameByRepo    map[string]map[string]string
	uniqueNameByRepoDir map[string]map[string]map[string]string
	constructorByPath   map[string]map[string]string
	entityFileByID      map[string]string
}

type codeFunctionSpan struct {
	startLine int
	endLine   int
	entityID  string
}

func buildCodeEntityIndex(envelopes []facts.Envelope) codeEntityIndex {
	index := codeEntityIndex{
		entitiesByPathLine:  make(map[string]string),
		spansByPath:         make(map[string][]codeFunctionSpan),
		uniqueNameByPath:    make(map[string]map[string]string),
		uniqueNameByRepo:    make(map[string]map[string]string),
		uniqueNameByRepoDir: make(map[string]map[string]map[string]string),
		constructorByPath:   make(map[string]map[string]string),
		entityFileByID:      make(map[string]string),
	}
	nameCandidates := make(map[string]map[string]map[string]struct{})
	repoNameCandidates := make(map[string]map[string]map[string]struct{})
	repoDirNameCandidates := make(map[string]map[string]map[string]map[string]struct{})

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
				if anyToString(item["name"]) == "constructor" {
					classContext := strings.TrimSpace(anyToString(item["class_context"]))
					if classContext != "" {
						if _, ok := index.constructorByPath[pathKey]; !ok {
							index.constructorByPath[pathKey] = make(map[string]string)
						}
						index.constructorByPath[pathKey][classContext] = entityID
					}
				}
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
						addCodeCallRepoDirNameCandidate(repoDirNameCandidates, repositoryID, preferredPath, candidateName, entityID)
					}
				}
			}
		}
		for _, bucket := range []string{"classes", "structs", "interfaces"} {
			for _, item := range mapSlice(fileData[bucket]) {
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
							addCodeCallRepoDirNameCandidate(repoDirNameCandidates, repositoryID, preferredPath, candidateName, entityID)
						}
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
	for repositoryID, dirs := range repoDirNameCandidates {
		index.uniqueNameByRepoDir[repositoryID] = make(map[string]map[string]string, len(dirs))
		for dir, names := range dirs {
			index.uniqueNameByRepoDir[repositoryID][dir] = make(map[string]string, len(names))
			for name, entityIDs := range names {
				if len(entityIDs) != 1 {
					continue
				}
				for entityID := range entityIDs {
					index.uniqueNameByRepoDir[repositoryID][dir][name] = entityID
				}
			}
		}
	}
	return index
}

func addCodeCallRepoDirNameCandidate(
	candidates map[string]map[string]map[string]map[string]struct{},
	repositoryID string,
	filePath string,
	name string,
	entityID string,
) {
	dir := codeCallDirectoryKey(filePath)
	if repositoryID == "" || dir == "" || name == "" || entityID == "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][dir]; !ok {
		candidates[repositoryID][dir] = make(map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][dir][name]; !ok {
		candidates[repositoryID][dir][name] = make(map[string]struct{})
	}
	candidates[repositoryID][dir][name][entityID] = struct{}{}
}

func codeCallDirectoryKey(filePath string) string {
	normalized := normalizeCodeCallPath(filePath)
	if normalized == "" || normalized == "." || !strings.Contains(normalized, "/") {
		return ""
	}
	return normalizeCodeCallPath(filepath.Dir(normalized))
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
	reexportIndex codeCallReexportIndex,
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
			callerID = resolveFileRootCodeCallCallerID(repositoryID, relativePath, fileData)
		}
		if callerID == "" {
			continue
		}
		calleeID, calleeFilePath := resolveGenericCallee(
			entityIndex,
			repositoryID,
			repositoryImports,
			reexportIndex,
			rawPath,
			relativePath,
			fileData,
			edge,
		)
		if calleeID == "" {
			continue
		}

		rows = appendCodeCallRow(rows, seenRows, repositoryID, callerID, calleeID, callerFilePath, calleeFilePath, callLine, edge)
		if constructorID := resolveConstructorMethodCalleeID(entityIndex, calleeFilePath, edge); constructorID != "" {
			rows = appendCodeCallRow(rows, seenRows, repositoryID, callerID, constructorID, callerFilePath, calleeFilePath, callLine, edge)
		}
	}
	return rows
}

func appendCodeCallRow(
	rows []map[string]any,
	seenRows map[string]struct{},
	repositoryID string,
	callerID string,
	calleeID string,
	callerFilePath string,
	calleeFilePath string,
	callLine int,
	edge map[string]any,
) []map[string]any {
	relationshipType := codeCallRelationshipType(edge)
	key := codeCallRowKey(repositoryID, callerID, calleeID, relationshipType, callLine)
	if _, exists := seenRows[key]; exists {
		return rows
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
	if relationshipType != "" {
		row["relationship_type"] = relationshipType
	}
	return append(rows, row)
}

func resolveConstructorMethodCalleeID(index codeEntityIndex, calleeFilePath string, edge map[string]any) string {
	if anyToString(edge["call_kind"]) != "constructor_call" {
		return ""
	}
	className := strings.TrimSpace(anyToString(edge["name"]))
	if className == "" {
		className = strings.TrimSpace(anyToString(edge["full_name"]))
	}
	if className == "" {
		return ""
	}
	for _, pathKey := range codeCallPathKeys(calleeFilePath, "") {
		if entityID := index.constructorByPath[pathKey][className]; entityID != "" {
			return entityID
		}
	}
	return ""
}

// resolveFileRootCodeCallCallerID returns the file path identity for top-level
// calls in JavaScript package-root files. This keeps executable module bodies
// visible to dead-code reachability without treating every library module's
// top-level expressions as roots.
func resolveFileRootCodeCallCallerID(repositoryID string, relativePath string, fileData map[string]any) string {
	language := anyToString(fileData["language"])
	if language == "" {
		language = anyToString(fileData["lang"])
	}
	switch strings.ToLower(language) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	for _, rootKind := range toStringSlice(fileData["dead_code_file_root_kinds"]) {
		switch rootKind {
		case "javascript.node_package_entrypoint", "javascript.node_package_bin", "javascript.node_package_export":
			if repositoryID == "" || relativePath == "" {
				return ""
			}
			return repositoryID + ":" + normalizeCodeCallPath(relativePath)
		}
	}
	return ""
}

// codeCallRelationshipType maps parser call-like metadata to the canonical
// relationship that truthfully describes the edge.
func codeCallRelationshipType(edge map[string]any) string {
	switch anyToString(edge["call_kind"]) {
	case "go.composite_literal_type_reference":
		return "REFERENCES"
	default:
		return ""
	}
}

// codeCallRowKey deduplicates type references by entity pair because repeated
// literal sites do not carry distinct reachability truth.
func codeCallRowKey(repositoryID string, callerID string, calleeID string, relationshipType string, line int) string {
	if relationshipType == "REFERENCES" {
		return repositoryID + "|" + callerID + "|" + calleeID + "|" + relationshipType
	}
	return repositoryID + "|" + callerID + "|" + calleeID + "|" + fmt.Sprintf("%d", line)
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
