// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// This file holds the typed accessors for the closed-shape, single-producer
// inner keys of a File fact's parsed_file_data map (issue #4750 S1). Each
// accessor decodes ONE inner key on demand from the open ParsedFileData
// map[string]any that DecodeCodegraphFile returns, leaving the container
// untyped so the wire schema for parsed_file_data stays byte-identical (no
// schema major bump — see codegraphv1's parsed_file_data.go). A code-graph-core
// reducer read site calls the accessor for the key it consumes instead of
// reaching into the map with a raw string lookup, so a producer that renames or
// retypes the inner shape becomes a typed decode change here rather than a
// silent wrong read in the reducer.
//
// These accessors decode an inner sub-object, not a fact envelope, so they do
// NOT enforce the envelope required-field / dead-letter contract
// decodeAndValidate applies to a fact kind. A malformed inner value returns a
// wrapped error the caller may treat as "no typed value" (the reducer's
// pre-typing behavior read the same absent/typed-wrong key as a nil/empty
// slice); a structurally invalid element is surfaced as an error so a caller
// that wants strictness can observe it. The reducer keeps its existing
// tolerant read semantics by treating an error/absent result as empty.

// DecodeParsedFileDataGomodState decodes the "gomod_state" inner key of a
// parsed_file_data map into a typed codegraphv1.GomodState. It returns
// (state, true, nil) when the key is present and decodes, (zero, false, nil)
// when the key is absent (a non-gomod file), and (zero, false, err) when the
// key is present but not a JSON object or its typed fields do not coerce. The
// non-read producer fields (go_version, toolchain, counts, replaced_modules,
// parse_error, checksum_count, ambiguous_entry) are preserved in the returned
// struct's open Attributes pass-through.
func DecodeParsedFileDataGomodState(parsedFileData map[string]any) (codegraphv1.GomodState, bool, error) {
	raw, present := parsedFileData["gomod_state"]
	if !present || raw == nil {
		return codegraphv1.GomodState{}, false, nil
	}
	obj, ok := asObjectMap(raw)
	if !ok {
		return codegraphv1.GomodState{}, false, fmt.Errorf("factschema: gomod_state: want JSON object, got %T", raw)
	}
	var state codegraphv1.GomodState
	if err := decodeMapInto(obj, &state); err != nil {
		return codegraphv1.GomodState{}, false, fmt.Errorf("factschema: gomod_state: %w", err)
	}
	return state, true, nil
}

// DecodeParsedFileDataSCIPFunctionCalls decodes the "function_calls_scip" inner
// slice of a parsed_file_data map into a typed []codegraphv1.SCIPFunctionCall.
// An absent key decodes to a nil slice with no error, matching the reducer's
// mapSlice(nil) read (a file with no SCIP edges yields no rows). A present value
// that is not a slice of objects, or an element whose typed fields do not
// coerce, returns an error so a strict caller can observe the malformed edge.
func DecodeParsedFileDataSCIPFunctionCalls(parsedFileData map[string]any) ([]codegraphv1.SCIPFunctionCall, error) {
	raw, present := parsedFileData["function_calls_scip"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: function_calls_scip: want slice of JSON objects, got %T", raw)
	}
	edges := make([]codegraphv1.SCIPFunctionCall, 0, len(elems))
	for i, elem := range elems {
		var edge codegraphv1.SCIPFunctionCall
		if err := decodeMapInto(elem, &edge); err != nil {
			return nil, fmt.Errorf("factschema: function_calls_scip[%d]: %w", i, err)
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// DecodeParsedFileDataDockerfileStages decodes the "dockerfile_stages" inner
// slice of a parsed_file_data map into a typed []codegraphv1.DockerfileStage.
// An absent key decodes to a nil slice with no error. The optional runtime
// fields (platform, workdir, ...) survive in each stage's open Attributes
// pass-through.
func DecodeParsedFileDataDockerfileStages(parsedFileData map[string]any) ([]codegraphv1.DockerfileStage, error) {
	raw, present := parsedFileData["dockerfile_stages"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: dockerfile_stages: want slice of JSON objects, got %T", raw)
	}
	stages := make([]codegraphv1.DockerfileStage, 0, len(elems))
	for i, elem := range elems {
		var stage codegraphv1.DockerfileStage
		if err := decodeMapInto(elem, &stage); err != nil {
			return nil, fmt.Errorf("factschema: dockerfile_stages[%d]: %w", i, err)
		}
		stages = append(stages, stage)
	}
	return stages, nil
}

// DecodeParsedFileDataPipelineCalls decodes the "pipeline_calls" inner key of a
// parsed_file_data map into a []string, tolerating both the []string the groovy
// producer emits and the []any a Postgres JSONB round trip yields. An absent or
// non-slice value decodes to nil, matching the reducer's sliceValue read (which
// only tests len() > 0).
func DecodeParsedFileDataPipelineCalls(parsedFileData map[string]any) []string {
	return decodeParsedFileDataStringSlice(parsedFileData, "pipeline_calls")
}

// DecodeParsedFileDataDeadCodeFileRootKinds decodes the
// "dead_code_file_root_kinds" inner key into a []string, matching the JavaScript
// dead-code root-kind values the reducer compares against literal root-kind
// strings (code_call_materialization_javascript_roots.go).
func DecodeParsedFileDataDeadCodeFileRootKinds(parsedFileData map[string]any) []string {
	return decodeParsedFileDataStringSlice(parsedFileData, "dead_code_file_root_kinds")
}

// decodeParsedFileDataStringSlice reads one inner key as a []string, accepting
// []string, []any (JSONB), or []map-free string elements, and returning nil for
// an absent or non-slice value. It mirrors the reducer's tolerant toStringSlice
// read: a non-string element is skipped rather than failing the whole slice, so
// the accessor never surfaces an error for a shape the pre-typing read tolerated.
func decodeParsedFileDataStringSlice(parsedFileData map[string]any, key string) []string {
	raw, present := parsedFileData[key]
	if !present || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		if len(v) == 0 {
			return nil
		}
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

// asObjectSlice coerces a raw parsed_file_data value into a []map[string]any,
// accepting both the []map[string]any the parser producers build in memory and
// the []any of map[string]any a Postgres JSONB round trip yields. ok is false
// when raw is not a slice, or when an element is not a JSON object.
func asObjectSlice(raw any) ([]map[string]any, bool) {
	switch v := raw.(type) {
	case []map[string]any:
		return v, true
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			obj, ok := asObjectMap(item)
			if !ok {
				return nil, false
			}
			out = append(out, obj)
		}
		return out, true
	default:
		return nil, false
	}
}
