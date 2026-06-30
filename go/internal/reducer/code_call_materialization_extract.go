// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ExtractAllCodeRelationshipRows builds both code-call and metaclass edge rows
// from a single entity index pass. This eliminates the duplicate
// buildCodeEntityIndex call that occurs when ExtractCodeCallRows and
// ExtractPythonMetaclassRows are called separately.
func ExtractAllCodeRelationshipRows(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
) {
	ccRepoIDs, ccRows, mcRepoIDs, mcRows, _ := extractAllCodeRelationshipRowsWithIndex(envelopes)
	return ccRepoIDs, ccRows, mcRepoIDs, mcRows
}

// extractAllCodeRelationshipRowsWithIndex builds code-call and metaclass edge
// rows and also returns the shared codeEntityIndex it built, so callers that
// need the index for an additional resolution pass (for example HANDLES_ROUTE
// handler resolution, #2721) reuse one index build instead of paying for a
// second pass over the same envelopes.
func extractAllCodeRelationshipRowsWithIndex(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
	entityIndex codeEntityIndex,
) {
	if len(envelopes) == 0 {
		return nil, nil, nil, nil, codeEntityIndex{}
	}

	repositoryIDs := collectCodeCallRepositoryIDs(envelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil, nil, nil, codeEntityIndex{}
	}

	entityIndex = buildCodeEntityIndex(envelopes)
	repositoryImports := collectCodeCallRepositoryImports(envelopes)
	reexportIndex := buildCodeCallReexportIndex(envelopes)

	ccRepoIDs, ccRows := extractCodeCallRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)
	mcRepoIDs, mcRows := extractPythonMetaclassRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports)
	return ccRepoIDs, ccRows, mcRepoIDs, mcRows, entityIndex
}

// ExtractCodeCallRows builds canonical caller/callee edge rows from repository
// and file facts.
func ExtractCodeCallRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	repositoryIDs := collectCodeCallRepositoryIDs(envelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil
	}

	entityIndex := buildCodeEntityIndex(envelopes)
	repositoryImports := collectCodeCallRepositoryImports(envelopes)
	reexportIndex := buildCodeCallReexportIndex(envelopes)
	return extractCodeCallRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)
}

func extractCodeCallRowsWithIndex(
	envelopes []facts.Envelope,
	repositoryIDs []string,
	entityIndex codeEntityIndex,
	repositoryImports map[string]map[string][]string,
	reexportIndex codeCallReexportIndex,
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

		rows = append(rows, extractSCIPCodeCallRows(repositoryID, entityIndex, seenRows, fileData)...)
		rows = append(
			rows,
			extractGenericCodeCallRows(
				repositoryID,
				relativePath,
				anyToString(fileData["path"]),
				entityIndex,
				repositoryImports[repositoryID],
				reexportIndex,
				seenRows,
				fileData,
			)...,
		)
	}

	sort.Slice(rows, func(i, j int) bool {
		left := anyToString(rows[i]["caller_entity_id"]) + "->" + anyToString(rows[i]["callee_entity_id"])
		right := anyToString(rows[j]["caller_entity_id"]) + "->" + anyToString(rows[j]["callee_entity_id"])
		if left == right {
			return anyToString(rows[i]["repo_id"]) < anyToString(rows[j]["repo_id"])
		}
		return left < right
	})

	return repositoryIDs, rows
}

func collectCodeCallRepositoryIDs(envelopes []facts.Envelope) []string {
	repositorySet := make(map[string]struct{})
	for _, env := range envelopes {
		switch env.FactKind {
		case "repository", "file":
			repositoryID := payloadStr(env.Payload, "repo_id")
			if repositoryID == "" {
				repositoryID = payloadStr(env.Payload, "graph_id")
			}
			if repositoryID != "" {
				repositorySet[repositoryID] = struct{}{}
			}
		}
	}

	repositoryIDs := make([]string, 0, len(repositorySet))
	for repositoryID := range repositorySet {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}
