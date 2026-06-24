// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// functionSourceFactKind is the fact kind for one function's param-level
// value-flow taint source. The reducer persists these as interproc source ports
// for the cross-repo fixpoint.
const functionSourceFactKind = "code_function_source"

// FunctionSourceSnapshot is one param-level taint source read from the parser's
// dataflow_sources bucket: the durable FunctionID, the parameter index that is an
// entry point, and the source kind. The FunctionID already carries the repository
// identity, so no entity-uid resolution is needed.
type FunctionSourceSnapshot struct {
	FunctionID string `json:"function_id"`
	ParamIndex int    `json:"param_index"`
	Kind       string `json:"kind"`
	Language   string `json:"language,omitempty"`
}

// buildFunctionSources reads each parsed file's dataflow_sources bucket and
// returns one snapshot per source. Empty when the parser emitted no sources (the
// value-flow gate is off, or no RepositoryID was supplied), so the snapshot is
// byte-identical when value-flow emission is disabled.
func buildFunctionSources(parsedFiles []map[string]any) []FunctionSourceSnapshot {
	var sources []FunctionSourceSnapshot
	for _, parsedFile := range parsedFiles {
		rows, _ := parsedFile["dataflow_sources"].([]map[string]any)
		for _, row := range rows {
			functionID := snapshotPayloadString(row, "function_id")
			kind := snapshotPayloadString(row, "kind")
			if functionID == "" || kind == "" {
				continue
			}
			sources = append(sources, FunctionSourceSnapshot{
				FunctionID: functionID,
				ParamIndex: snapshotPayloadInt(row, "param_index"),
				Kind:       kind,
				Language:   snapshotPayloadString(row, "lang", "language"),
			})
		}
	}
	return sources
}

// functionSourceFactEnvelope builds the fact for one param-level source. The
// stable key is the FunctionID plus parameter index, so re-emission of the same
// generation is idempotent and a changed source set overwrites the prior rows.
func functionSourceFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	source FunctionSourceSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_kind":  functionSourceFactKind,
		"function_id": source.FunctionID,
		"param_index": source.ParamIndex,
		"kind":        source.Kind,
		"repo_id":     repoID,
	}
	if source.Language != "" {
		payload["language"] = source.Language
	}

	return factEnvelope(
		functionSourceFactKind,
		scopeID,
		generationID,
		observedAt,
		functionSourceFactKind+":"+repoID+":"+source.FunctionID+":"+strconv.Itoa(source.ParamIndex),
		payload,
		repoPath,
	)
}
