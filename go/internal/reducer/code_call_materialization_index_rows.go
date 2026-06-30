// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

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
		calleeFile := anyToString(edge["callee_file"])
		resolutionMethod := codeprovenance.MethodSCIP
		if calleeID == "" {
			calleeID, calleeFile, resolutionMethod = resolveCodeSymbolCallee(entityIndex, edge)
		}
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
			"caller_entity_type": codeCallEndpointEntityType(
				entityIndex,
				repositoryID,
				callerID,
			),
			"callee_entity_id": calleeID,
			"callee_entity_type": codeCallEndpointEntityType(
				entityIndex,
				repositoryID,
				calleeID,
			),
			"resolution_method": resolutionMethod,
			"action":            IntentActionUpsert,
		}
		copyOptionalCodeCallField(row, edge, "caller_symbol")
		copyOptionalCodeCallField(row, edge, "callee_symbol")
		copyOptionalCodeCallField(row, edge, "caller_file")
		copyOptionalCodeCallField(row, edge, "callee_file")
		if calleeFile != "" {
			row["callee_file"] = calleeFile
		}
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
		calleeID, calleeFilePath, resolutionMethod := resolveGenericCallee(
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
		if callerID == "" {
			callerID = resolveJavaScriptTopLevelReferenceCallerID(repositoryID, callerFilePath, edge)
		}
		if callerID == "" {
			callerID = resolveJavaMetadataFileRootCallerID(repositoryID, callerFilePath, edge)
		}
		if callerID == "" {
			callerID = resolveSameFileTopLevelCodeCallCallerID(
				repositoryID,
				callerFilePath,
				calleeFilePath,
				edge,
			)
		}
		if callerID == "" {
			continue
		}

		rows = appendCodeCallRow(rows, seenRows, repositoryID, entityIndex, callerID, calleeID, callerFilePath, calleeFilePath, callLine, resolutionMethod, edge)
		rows = appendInstantiatesRow(rows, seenRows, repositoryID, entityIndex, callerID, calleeID, callerFilePath, calleeFilePath, callLine, edge)
		if constructorID := resolveConstructorMethodCalleeID(entityIndex, calleeFilePath, edge); constructorID != "" {
			rows = appendCodeCallRow(rows, seenRows, repositoryID, entityIndex, callerID, constructorID, callerFilePath, calleeFilePath, callLine, codeprovenance.MethodTypeInferred, edge)
		}
	}
	return rows
}

func resolveSameFileScopedCalleeEntityID(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	call map[string]any,
	line int,
) string {
	if line <= 0 {
		return ""
	}
	language := codeCallLanguage(call, rawPath, relativePath)
	callNames := codeCallExactCandidateNames(call, language)
	if !codeCallPrefersImportedQualifiedTarget(call, language) {
		callNames = append(callNames, codeCallBroadCandidateNames(call, language)...)
	}
	for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
		caller := codeFunctionSpan{}
		for _, span := range index.spansByPath[pathKey] {
			if line >= span.startLine && line <= span.endLine &&
				(caller.entityID == "" || spanWidth(span) < spanWidth(caller)) {
				caller = span
			}
		}
		if caller.entityID == "" {
			continue
		}

		match := ""
		for _, span := range index.spansByPath[pathKey] {
			if span.entityID == caller.entityID ||
				span.startLine < caller.startLine ||
				span.endLine > caller.endLine ||
				!codeCallSpanMatchesAnyName(span, callNames) {
				continue
			}
			if match != "" {
				return ""
			}
			match = span.entityID
		}
		if match != "" {
			return match
		}
	}
	return ""
}

func appendCodeCallRow(
	rows []map[string]any,
	seenRows map[string]struct{},
	repositoryID string,
	entityIndex codeEntityIndex,
	callerID string,
	calleeID string,
	callerFilePath string,
	calleeFilePath string,
	callLine int,
	resolutionMethod codeprovenance.Method,
	edge map[string]any,
) []map[string]any {
	relationshipType := codeCallRelationshipType(edge)
	key := codeCallRowKey(repositoryID, callerID, calleeID, relationshipType, callLine)
	if _, exists := seenRows[key]; exists {
		return rows
	}
	seenRows[key] = struct{}{}

	row := map[string]any{
		"repo_id":            repositoryID,
		"caller_entity_id":   callerID,
		"caller_entity_type": codeCallEndpointEntityType(entityIndex, repositoryID, callerID),
		"callee_entity_id":   calleeID,
		"callee_entity_type": codeCallEndpointEntityType(entityIndex, repositoryID, calleeID),
		"caller_file":        callerFilePath,
		"callee_file":        calleeFilePath,
		"ref_line":           callLine,
		"action":             IntentActionUpsert,
	}
	if resolutionMethod != "" {
		row["resolution_method"] = resolutionMethod
	}
	copyOptionalCodeCallField(row, edge, "full_name")
	copyOptionalCodeCallField(row, edge, "call_kind")
	if relationshipType != "" {
		row["relationship_type"] = relationshipType
	}
	return append(rows, row)
}
