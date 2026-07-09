// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// functionSummaryFactKind is the fact kind for one function's durable value-flow
// summary. The reducer reconstructs the structural Effects from the payload and
// persists them to the function-summary store for cross-repo composition.
const functionSummaryFactKind = "code_function_summary"

// FunctionSummarySnapshot is one function's raw value-flow Effects, read from the
// parser's dataflow_summaries bucket. The effect lists are carried verbatim as
// the parser rendered them (param_to_sink / param_to_call_arg are nested maps),
// so the reducer can rebuild summary.Effects without re-deriving them. The
// FunctionID is durable and already carries the repository identity, so no
// entity-uid resolution is needed.
type FunctionSummarySnapshot struct {
	FunctionID     string           `json:"function_id"`
	GraphUID       string           `json:"graph_uid,omitempty"`
	Language       string           `json:"language,omitempty"`
	ParamToReturn  []any            `json:"param_to_return,omitempty"`
	ParamToSink    []map[string]any `json:"param_to_sink,omitempty"`
	SourceToReturn []any            `json:"source_to_return,omitempty"`
	ParamToCallArg []map[string]any `json:"param_to_call_arg,omitempty"`
}

// buildFunctionSummaries reads each parsed file's dataflow_summaries bucket and
// returns one snapshot per function. Each function's graph Function uid is
// resolved here — where the entities and the parse payload are co-located —
// against the function's (relative path, receiver, name), so the cross-repo
// fixpoint can project findings as TAINT_FLOWS_TO edges by uid without
// re-resolving names. A function whose uid cannot be resolved still carries its
// summary (the FunctionID is durable); only the graph projection needs the uid.
// Empty when the parser emitted no summaries, so the snapshot is byte-identical
// when value-flow emission is disabled. functionUIDResolver is the shared
// resolver built once per snapshot by newFunctionUIDResolver; it is read-only.
func buildFunctionSummaries(repoPath string, parsedFiles []map[string]any, functionUIDResolver func(relativePath, receiver, name string) (string, bool)) []FunctionSummarySnapshot {
	var summaries []FunctionSummarySnapshot
	for _, parsedFile := range parsedFiles {
		rows, _ := parsedFile["dataflow_summaries"].([]map[string]any)
		if len(rows) == 0 {
			continue
		}
		relativePath, err := filepath.Rel(repoPath, snapshotPayloadString(parsedFile, "path"))
		if err != nil {
			relativePath = ""
		} else {
			relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		}
		for _, row := range rows {
			functionID := snapshotPayloadString(row, "function_id")
			if functionID == "" {
				continue
			}
			receiver, name := functionIDReceiverName(functionID)
			uid, _ := functionUIDResolver(relativePath, receiver, name)
			summary := FunctionSummarySnapshot{
				FunctionID: functionID,
				GraphUID:   uid,
				Language:   snapshotPayloadString(row, "lang", "language"),
			}
			if v, ok := row["param_to_return"].([]any); ok {
				summary.ParamToReturn = v
			}
			if v, ok := row["param_to_sink"].([]map[string]any); ok {
				summary.ParamToSink = v
			}
			if v, ok := row["source_to_return"].([]any); ok {
				summary.SourceToReturn = v
			}
			if v, ok := row["param_to_call_arg"].([]map[string]any); ok {
				summary.ParamToCallArg = v
			}
			summaries = append(summaries, summary)
		}
	}
	return summaries
}

// functionSummaryFactEnvelope builds the fact for one function summary. The stable
// key is the FunctionID, which is generation-independent, so re-emission of the
// same generation is idempotent and a changed effect set overwrites the prior
// summary for that function.
func functionSummaryFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	summary FunctionSummarySnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_kind":  functionSummaryFactKind,
		"function_id": summary.FunctionID,
		"repo_id":     repoID,
	}
	if summary.GraphUID != "" {
		payload["graph_uid"] = summary.GraphUID
	}
	if summary.Language != "" {
		payload["language"] = summary.Language
	}
	if len(summary.ParamToReturn) > 0 {
		payload["param_to_return"] = summary.ParamToReturn
	}
	if len(summary.ParamToSink) > 0 {
		payload["param_to_sink"] = summary.ParamToSink
	}
	if len(summary.SourceToReturn) > 0 {
		payload["source_to_return"] = summary.SourceToReturn
	}
	if len(summary.ParamToCallArg) > 0 {
		payload["param_to_call_arg"] = summary.ParamToCallArg
	}

	return factEnvelope(
		functionSummaryFactKind,
		scopeID,
		generationID,
		observedAt,
		functionSummaryFactKind+":"+repoID+":"+summary.FunctionID,
		payload,
		repoPath,
	)
}
