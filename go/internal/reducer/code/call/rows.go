// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/java"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/javascript"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func extractSCIPCodeCallRows(
	repositoryID string,
	entityIndex shared.EntityIndex,
	seenRows map[string]struct{},
	fileData map[string]any,
) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, edge := range payloadcore.MapSlice(fileData["function_calls_scip"]) {
		callerID := shared.ResolveEntityID(entityIndex, edge["caller_file"], edge["caller_line"])
		calleeID := shared.ResolveEntityID(entityIndex, edge["callee_file"], edge["callee_line"])
		calleeFile := payloadcore.AnyToString(edge["callee_file"])
		resolutionMethod := codeprovenance.MethodSCIP
		if calleeID == "" {
			calleeID, calleeFile, resolutionMethod = shared.ResolveSymbolCallee(entityIndex, edge)
		}
		if callerID == "" || calleeID == "" {
			continue
		}

		key := repositoryID + "|" + callerID + "|" + calleeID + "|" + fmt.Sprintf("%d", shared.PayloadInt(edge["ref_line"]))
		if _, exists := seenRows[key]; exists {
			continue
		}
		seenRows[key] = struct{}{}

		row := map[string]any{
			"repo_id":          repositoryID,
			"caller_entity_id": callerID,
			"caller_entity_type": shared.EndpointEntityType(
				entityIndex,
				repositoryID,
				callerID,
			),
			"callee_entity_id": calleeID,
			"callee_entity_type": shared.EndpointEntityType(
				entityIndex,
				repositoryID,
				calleeID,
			),
			"resolution_method": resolutionMethod,
			"action":            reducercontract.IntentActionUpsert,
		}
		shared.CopyOptionalField(row, edge, "caller_symbol")
		shared.CopyOptionalField(row, edge, "callee_symbol")
		shared.CopyOptionalField(row, edge, "caller_file")
		shared.CopyOptionalField(row, edge, "callee_file")
		if calleeFile != "" {
			row["callee_file"] = calleeFile
		}
		shared.CopyOptionalField(row, edge, "ref_line")
		rows = append(rows, row)
	}
	return rows
}

func extractGenericCodeCallRows(
	repositoryID string,
	relativePath string,
	rawPath string,
	entityIndex shared.EntityIndex,
	repositoryImports map[string][]string,
	reexportIndex shared.ReexportIndex,
	seenRows map[string]struct{},
	fileData map[string]any,
) []map[string]any {
	rows := make([]map[string]any, 0)
	callerFilePath := shared.PreferredPath(rawPath, relativePath)
	for _, edge := range payloadcore.MapSlice(fileData["function_calls"]) {
		callLine := shared.PayloadInt(edge["line_number"], edge["ref_line"])
		if callLine <= 0 {
			continue
		}
		callerID := shared.ResolveContainingEntityID(entityIndex, rawPath, relativePath, callLine)
		if callerID == "" {
			callerID = javascript.FileRootCallerID(repositoryID, relativePath, fileData)
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
			callerID = javascript.TopLevelReferenceCallerID(repositoryID, callerFilePath, edge)
		}
		if callerID == "" {
			callerID = java.MetadataFileRootCallerID(repositoryID, callerFilePath, edge)
		}
		if callerID == "" {
			callerID = javascript.SameFileTopLevelCallerID(
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
		if constructorID := shared.ResolveConstructorMethodCalleeID(entityIndex, calleeFilePath, edge); constructorID != "" {
			rows = appendCodeCallRow(rows, seenRows, repositoryID, entityIndex, callerID, constructorID, callerFilePath, calleeFilePath, callLine, codeprovenance.MethodTypeInferred, edge)
		}
	}
	return rows
}

func resolveSameFileScopedCalleeEntityID(
	index shared.EntityIndex,
	rawPath string,
	relativePath string,
	call map[string]any,
	line int,
) string {
	if line <= 0 {
		return ""
	}
	language := shared.CallLanguage(call, rawPath, relativePath)
	callNames := shared.ExactCandidateNames(call, language)
	if !shared.PrefersImportedQualifiedTarget(call, language) {
		callNames = append(callNames, shared.BroadCandidateNames(call, language)...)
	}
	for _, pathKey := range shared.PathKeys(rawPath, relativePath) {
		caller := shared.FunctionSpan{}
		for _, span := range index.SpansByPath(pathKey) {
			if line >= span.StartLine && line <= span.EndLine &&
				(caller.EntityID == "" || shared.SpanWidth(span) < shared.SpanWidth(caller)) {
				caller = span
			}
		}
		if caller.EntityID == "" {
			continue
		}

		match := ""
		for _, span := range index.SpansByPath(pathKey) {
			if span.EntityID == caller.EntityID ||
				span.StartLine < caller.StartLine ||
				span.EndLine > caller.EndLine ||
				!shared.SpanMatchesAnyName(span, callNames) {
				continue
			}
			if match != "" {
				return ""
			}
			match = span.EntityID
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
	entityIndex shared.EntityIndex,
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
		"caller_entity_type": shared.EndpointEntityType(entityIndex, repositoryID, callerID),
		"callee_entity_id":   calleeID,
		"callee_entity_type": shared.EndpointEntityType(entityIndex, repositoryID, calleeID),
		"caller_file":        callerFilePath,
		"callee_file":        calleeFilePath,
		"ref_line":           callLine,
		"action":             reducercontract.IntentActionUpsert,
	}
	if resolutionMethod != "" {
		row["resolution_method"] = resolutionMethod
	}
	// "lang" rides along so a corpus-wide pass (recordCodeCallSelfLoopWritten)
	// can attribute a written self-loop (caller_entity_id == callee_entity_id
	// — a genuinely recursive function, not a defect to filter: see #5332)
	// to the source language without re-deriving it from file paths.
	shared.CopyOptionalField(row, edge, "lang")
	shared.CopyOptionalField(row, edge, "full_name")
	shared.CopyOptionalField(row, edge, "call_kind")
	if relationshipType != "" {
		row["relationship_type"] = relationshipType
	}
	return append(rows, row)
}
