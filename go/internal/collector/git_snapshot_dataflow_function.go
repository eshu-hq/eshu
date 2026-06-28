// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// DataflowFunctionSnapshot is one parser-emitted function-level dataflow record.
// The parser owns the CFG/reaching-definition bounds; the collector preserves the
// rows as exact parser evidence for query readbacks and resolves the graph
// Function uid when content materialization produced one.
type DataflowFunctionSnapshot struct {
	FunctionUID         string           `json:"function_uid,omitempty"`
	RelativePath        string           `json:"relative_path"`
	FunctionName        string           `json:"function_name"`
	Language            string           `json:"language,omitempty"`
	LineNumber          int              `json:"line_number,omitempty"`
	CFGBlocks           []any            `json:"cfg_blocks,omitempty"`
	CFGEdges            []any            `json:"cfg_edges,omitempty"`
	DefUse              []map[string]any `json:"def_use,omitempty"`
	ControlDependencies []map[string]any `json:"control_dependencies,omitempty"`
	Overflow            bool             `json:"overflow,omitempty"`
	OverflowReason      string           `json:"overflow_reason,omitempty"`
}

// buildDataflowFunctions reads each parsed file's dataflow_functions bucket.
// Empty when the parser emitted no bucket, preserving the dataflow-gate-off
// snapshot shape.
func buildDataflowFunctions(
	repoPath string,
	parsedFiles []map[string]any,
	entities []content.EntityRecord,
) []DataflowFunctionSnapshot {
	lookup := make(map[string]string, len(entities))
	for _, entity := range entities {
		key := entityLookupKey(entity.Path, entity.EntityType, entity.EntityName, entity.StartLine)
		lookup[key] = entity.EntityID
	}

	var functions []DataflowFunctionSnapshot
	for _, parsedFile := range parsedFiles {
		rows, _ := parsedFile["dataflow_functions"].([]map[string]any)
		if len(rows) == 0 {
			continue
		}
		relativePath, err := filepath.Rel(repoPath, snapshotPayloadString(parsedFile, "path"))
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		for _, row := range rows {
			functionName := snapshotPayloadString(row, "function_name", "name")
			line := snapshotPayloadInt(row, "line_number")
			if functionName == "" {
				continue
			}
			function := DataflowFunctionSnapshot{
				RelativePath:        relativePath,
				FunctionName:        functionName,
				Language:            snapshotPayloadString(row, "lang", "language"),
				LineNumber:          line,
				DefUse:              snapshotPayloadMapSlice(row, "def_use"),
				ControlDependencies: snapshotPayloadMapSlice(row, "control_dependencies"),
				Overflow:            snapshotPayloadBool(row, "overflow"),
				OverflowReason:      snapshotPayloadString(row, "overflow_reason", "limit_reason"),
			}
			if cfg, ok := row["cfg"].(map[string]any); ok {
				function.CFGBlocks = snapshotPayloadAnySlice(cfg, "blocks")
				function.CFGEdges = snapshotPayloadAnySlice(cfg, "edges")
			}
			key := entityLookupKey(relativePath, taintEvidenceFunctionLabel, functionName, line)
			function.FunctionUID = lookup[key]
			functions = append(functions, function)
		}
	}
	return functions
}

// dataflowFunctionFactEnvelope builds the exact parser evidence fact for one
// function-level CFG/reaching-def record.
func dataflowFunctionFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	function DataflowFunctionSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_kind":    facts.CodeDataflowFunctionFactKind,
		"repo_id":       repoID,
		"relative_path": function.RelativePath,
		"function_name": function.FunctionName,
	}
	if function.FunctionUID != "" {
		payload["function_uid"] = function.FunctionUID
	}
	if function.Language != "" {
		payload["language"] = function.Language
	}
	if function.LineNumber > 0 {
		payload["line_number"] = function.LineNumber
	}
	if len(function.CFGBlocks) > 0 {
		payload["cfg_blocks"] = function.CFGBlocks
	}
	if len(function.CFGEdges) > 0 {
		payload["cfg_edges"] = function.CFGEdges
	}
	if len(function.DefUse) > 0 {
		payload["def_use"] = function.DefUse
	}
	if len(function.ControlDependencies) > 0 {
		payload["control_dependencies"] = function.ControlDependencies
	}
	if function.Overflow {
		payload["overflow"] = true
	}
	if function.OverflowReason != "" {
		payload["overflow_reason"] = function.OverflowReason
	}

	factKey := facts.CodeDataflowFunctionFactKind + ":" + repoID + ":" +
		function.RelativePath + ":" + function.FunctionName + ":" +
		strconv.Itoa(function.LineNumber)
	return factEnvelope(
		facts.CodeDataflowFunctionFactKind,
		scopeID,
		generationID,
		observedAt,
		factKey,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(function.RelativePath)),
	)
}

func snapshotPayloadAnySlice(payload map[string]any, key string) []any {
	switch typed := payload[key].(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, row := range typed {
			out = append(out, row)
		}
		return out
	default:
		return nil
	}
}
