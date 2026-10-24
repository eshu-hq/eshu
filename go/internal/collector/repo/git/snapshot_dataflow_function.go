// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/code"
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
// snapshot shape. entityUIDLookup is the shared (path, type, name, line) →
// entity ID map built once per snapshot by buildEntityUIDLookup; it is
// read-only.
func buildDataflowFunctions(
	repoPath string,
	parsedFiles []map[string]any,
	entityUIDLookup map[string]string,
) []DataflowFunctionSnapshot {
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
				CFGBlocks:           snapshotPayloadAnySlice(row, "blocks"),
				DefUse:              snapshotPayloadMapSlice(row, "def_uses"),
				ControlDependencies: snapshotPayloadMapSlice(row, "control_dependencies"),
				Overflow:            dataflowOverflowPresent(row),
				OverflowReason:      dataflowOverflowReason(row),
			}
			if cfg, ok := row["cfg"].(map[string]any); ok {
				function.CFGBlocks = snapshotPayloadAnySlice(cfg, "blocks")
				function.CFGEdges = snapshotPayloadAnySlice(cfg, "edges")
			}
			if len(function.CFGEdges) == 0 {
				function.CFGEdges = dataflowCFGEdges(function.CFGBlocks)
			}
			if len(function.DefUse) == 0 {
				function.DefUse = snapshotPayloadMapSlice(row, "def_use")
			}
			key := entityLookupKey(relativePath, taintEvidenceFunctionLabel, functionName, line)
			function.FunctionUID = entityUIDLookup[key]
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
		"graph_kind":    code.DataflowFunctionFactKind,
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

	factKey := code.DataflowFunctionFactKind + ":" + repoID + ":" +
		function.RelativePath + ":" + function.FunctionName + ":" +
		strconv.Itoa(function.LineNumber)
	return model.FactEnvelope(
		code.DataflowFunctionFactKind,
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

func dataflowCFGEdges(blocks []any) []any {
	var edges []any
	for _, block := range blocks {
		blockMap, ok := block.(map[string]any)
		if !ok {
			continue
		}
		from, ok := dataflowInt(blockMap["id"])
		if !ok {
			continue
		}
		for _, succ := range dataflowIntSlice(blockMap["succs"]) {
			edges = append(edges, map[string]any{"from": from, "to": succ})
		}
	}
	return edges
}

func dataflowOverflowPresent(row map[string]any) bool {
	switch typed := row["overflow"].(type) {
	case bool:
		return typed
	case map[string]any:
		for _, value := range typed {
			if n, ok := dataflowInt(value); ok && n != 0 {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func dataflowOverflowReason(row map[string]any) string {
	if reason := snapshotPayloadString(row, "overflow_reason", "limit_reason"); reason != "" {
		return reason
	}
	details, ok := row["overflow"].(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(details))
	for key, value := range details {
		if n, ok := dataflowInt(value); ok && n != 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		n, _ := dataflowInt(details[key])
		parts = append(parts, key+"="+strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}

func dataflowIntSlice(value any) []int {
	switch typed := value.(type) {
	case []int:
		return typed
	case []any:
		out := make([]int, 0, len(typed))
		for _, item := range typed {
			if n, ok := dataflowInt(item); ok {
				out = append(out, n)
			}
		}
		return out
	default:
		return nil
	}
}

func dataflowInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

// emitValueFlowFacts streams the opt-in value-flow facts (ESHU_EMIT_DATAFLOW):
// taint and interprocedural evidence, function summaries, param-level sources,
// and dataflow functions. Every slice is empty when the gate is off, so each
// loop is then a no-op. Each slice is released as it is sent.
func emitValueFlowFacts(
	w *model.FactStreamWriter,
	repoPath, repoID, scopeID, generationID string,
	observedAt time.Time,
	snapshot *RepositorySnapshot,
) {
	// Value-flow taint evidence facts (opt-in via ESHU_EMIT_DATAFLOW; the slice is
	// empty otherwise so this loop is a no-op when the gate is off).
	for _, evidence := range snapshot.TaintEvidence {
		w.Send(taintEvidenceFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, evidence))
	}
	snapshot.TaintEvidence = nil
	for _, evidence := range snapshot.InterprocTaintEvidence {
		w.Send(interprocEvidenceFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, evidence))
	}
	snapshot.InterprocTaintEvidence = nil

	// Value-flow function summary facts (opt-in via ESHU_EMIT_DATAFLOW; the slice
	// is empty otherwise so this loop is a no-op when the gate is off). Emitted on
	// both delta and full generations: each summary upserts by its durable
	// FunctionID, so a delta that only re-summarizes changed files refreshes those
	// functions without disturbing the rest.
	for _, summary := range snapshot.FunctionSummaries {
		w.Send(functionSummaryFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, summary))
	}
	snapshot.FunctionSummaries = nil

	// Value-flow param-level source facts (opt-in via ESHU_EMIT_DATAFLOW; empty
	// otherwise). Emitted on both delta and full generations: each upserts by its
	// (FunctionID, param index) so a delta refreshes only changed files.
	for _, fnSource := range snapshot.FunctionSources {
		w.Send(functionSourceFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, fnSource))
	}
	snapshot.FunctionSources = nil
	for _, function := range snapshot.DataflowFunctions {
		w.Send(dataflowFunctionFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, function))
	}
	snapshot.DataflowFunctions = nil
}
