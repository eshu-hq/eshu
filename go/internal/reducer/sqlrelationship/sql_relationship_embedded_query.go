// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sqlrelationship

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type sqlEmbeddedQuerySource struct {
	functionEntityID string
	repoID           string
	relativePath     string
	sourcePath       string
	tableName        string
}

func appendEmbeddedSQLQueryRows(
	rows []map[string]any,
	seenEdges map[string]struct{},
	entityByName map[string][]sqlRelationshipEntity,
	envelopes []facts.Envelope,
) []map[string]any {
	for _, source := range embeddedSQLQuerySources(envelopes) {
		target, ok := resolveSQLRelationshipTarget(
			entityByName,
			source.tableName,
			"SqlTable",
			source.repoID,
			source.relativePath,
		)
		if !ok {
			continue
		}
		edgeKey := source.functionEntityID + "->QUERIES_TABLE->" + target.entityID
		if _, seen := seenEdges[edgeKey]; seen {
			continue
		}
		seenEdges[edgeKey] = struct{}{}
		rows = append(rows, map[string]any{
			"source_entity_id":   source.functionEntityID,
			"target_entity_id":   target.entityID,
			"source_entity_type": "Function",
			"target_entity_type": target.entityType,
			"source_path":        source.sourcePath,
			"repo_id":            source.repoID,
			"relationship_type":  "QUERIES_TABLE",
		})
	}
	return rows
}

func embeddedSQLQuerySources(envelopes []facts.Envelope) []sqlEmbeddedQuerySource {
	var sources []sqlEmbeddedQuerySource
	for _, env := range envelopes {
		if env.FactKind != factKindFile || env.IsTombstone {
			continue
		}
		parsedFileData := payloadMap(env.Payload, "parsed_file_data")
		if parsedFileData == nil {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		relativePath := semanticPayloadString(env.Payload, "relative_path")
		sourcePath := semanticPayloadString(env.Payload, "path")
		if sourcePath == "" {
			sourcePath = semanticPayloadString(parsedFileData, "path")
		}
		if sourcePath == "" {
			continue
		}
		functionIDs := EmbeddedSQLFunctionIDsByNameLine(parsedFileData)
		for _, query := range mapSlice(parsedFileData["embedded_sql_queries"]) {
			functionName := anyToString(query["function_name"])
			functionLine := codeCallInt(query["function_line_number"])
			tableName := anyToString(query["table_name"])
			if repoID == "" || functionName == "" || functionLine <= 0 || tableName == "" {
				continue
			}
			functionEntityID := functionIDs[EmbeddedSQLFunctionKey(functionName, functionLine)]
			if functionEntityID == "" {
				continue
			}
			sources = append(sources, sqlEmbeddedQuerySource{
				functionEntityID: functionEntityID,
				repoID:           repoID,
				relativePath:     relativePath,
				sourcePath:       sourcePath,
				tableName:        tableName,
			})
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		left := sources[i].repoID + ":" + sources[i].relativePath + ":" + sources[i].functionEntityID + ":" + sources[i].tableName
		right := sources[j].repoID + ":" + sources[j].relativePath + ":" + sources[j].functionEntityID + ":" + sources[j].tableName
		return left < right
	})
	return sources
}

// EmbeddedSQLFunctionIDsByNameLine indexes parsedFileData's "functions" entries
// by (name, line) so a per-file embedded-code scanner can resolve the entity ID
// of the enclosing function for one embedded_line-carrying record. Exported
// (rather than kept package-private, as most of this file's helpers are)
// because the shell_exec family, which has not moved out of the reducer root
// yet, resolves its own embedded_shell_commands records through this exact
// same name+line index (shell_exec_materialization.go, issue #6061).
func EmbeddedSQLFunctionIDsByNameLine(parsedFileData map[string]any) map[string]string {
	out := make(map[string]string)
	for _, fn := range mapSlice(parsedFileData["functions"]) {
		name := anyToString(fn["name"])
		line := codeCallInt(fn["line_number"], fn["start_line"])
		entityID := anyToString(fn["uid"])
		if name == "" || line <= 0 || entityID == "" {
			continue
		}
		out[EmbeddedSQLFunctionKey(name, line)] = entityID
	}
	return out
}

// EmbeddedSQLFunctionKey builds the (name, line) index key
// EmbeddedSQLFunctionIDsByNameLine uses. Exported for the same cross-family
// reuse reason as EmbeddedSQLFunctionIDsByNameLine.
func EmbeddedSQLFunctionKey(name string, line int) string {
	return name + "\x00" + anyToString(line)
}
