// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ExtractMetaclassRows builds canonical USES_METACLASS edges from Python
// class metadata emitted by the Go parser path.
func ExtractMetaclassRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	repositoryIDs := shared.CollectRepositoryIDs(envelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil
	}

	entityIndex := shared.BuildEntityIndex(envelopes)
	repositoryImports := shared.CollectRepositoryImports(envelopes)
	return ExtractMetaclassRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports)
}

// ExtractMetaclassRowsWithIndex builds USES_METACLASS edge rows using an
// already-built [shared.EntityIndex], so the code/call dispatcher can share
// one index build across the code-call and metaclass extraction passes.
func ExtractMetaclassRowsWithIndex(
	envelopes []facts.Envelope,
	repositoryIDs []string,
	entityIndex shared.EntityIndex,
	repositoryImports map[string]map[string][]string,
) ([]string, []map[string]any) {
	seenRows := make(map[string]struct{})
	rows := make([]map[string]any, 0)

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}

		repositoryID := payloadcore.PayloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}

		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}

		relativePath := payloadcore.PayloadStr(env.Payload, "relative_path")
		rawPath := payloadcore.AnyToString(fileData["path"])
		sourceFilePath := shared.PreferredPath(rawPath, relativePath)
		for _, item := range payloadcore.MapSlice(fileData["classes"]) {
			sourceEntityID := strings.TrimSpace(payloadcore.AnyToString(item["uid"]))
			if sourceEntityID == "" {
				sourceEntityID = resolvePythonClassEntityID(entityIndex, rawPath, relativePath, item)
			}
			if sourceEntityID == "" {
				continue
			}

			metaclassName := strings.TrimSpace(payloadcore.AnyToString(item["metaclass"]))
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
				"resolution_method": codeprovenance.MethodDeclared,
				"action":            reducercontract.IntentActionUpsert,
			}
			rows = append(rows, row)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		left := payloadcore.AnyToString(rows[i]["source_entity_id"]) + "->" + payloadcore.AnyToString(rows[i]["target_entity_id"])
		right := payloadcore.AnyToString(rows[j]["source_entity_id"]) + "->" + payloadcore.AnyToString(rows[j]["target_entity_id"])
		if left == right {
			return payloadcore.AnyToString(rows[i]["repo_id"]) < payloadcore.AnyToString(rows[j]["repo_id"])
		}
		return left < right
	})

	return repositoryIDs, rows
}

func resolvePythonClassEntityID(
	index shared.EntityIndex,
	rawPath string,
	relativePath string,
	class map[string]any,
) string {
	sourceName := strings.TrimSpace(payloadcore.AnyToString(class["name"]))
	if sourceName == "" {
		return ""
	}
	for _, pathKey := range shared.PathKeys(rawPath, relativePath) {
		if entityID := index.UniqueNameByPath(pathKey, sourceName); entityID != "" {
			return entityID
		}
	}
	return ""
}

func resolvePythonMetaclassEntityID(
	index shared.EntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	metaclassName string,
) (string, string) {
	callLike := map[string]any{
		"name":      shared.TrailingName(metaclassName),
		"full_name": metaclassName,
		"lang":      "python",
	}

	if entityID := shared.ResolveSameFileCalleeEntityID(index, rawPath, relativePath, callLike); entityID != "" {
		return entityID, shared.PreferredPath(rawPath, relativePath)
	}
	for _, name := range shared.ExactCandidateNames(callLike, "python") {
		if entityID := index.UniqueNameByRepo(repositoryID, name); entityID != "" {
			return entityID, index.EntityFileByID(entityID)
		}
	}
	for _, name := range shared.BroadCandidateNames(callLike, "python") {
		if entityID := index.UniqueNameByRepo(repositoryID, name); entityID != "" {
			return entityID, index.EntityFileByID(entityID)
		}
	}

	return shared.ResolveImportedCrossFileCallee(
		index,
		repositoryImports,
		shared.ReexportIndex{},
		repositoryID,
		rawPath,
		relativePath,
		fileData,
		callLike,
	)
}
