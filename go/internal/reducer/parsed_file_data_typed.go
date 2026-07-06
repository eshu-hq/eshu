// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/sdk/go/factschema"

// This file holds the reducer-side typed readers for the closed-shape,
// single-producer inner keys of a File fact's parsed_file_data map (issue #4750
// S1). Each reader decodes ONE inner key through the factschema contracts seam
// (sdk/go/factschema/decode_parsed_file_data.go) instead of the code-graph-core
// handlers reaching into the untyped map with a raw string lookup, so a
// producer that renames or retypes an inner shape becomes a typed decode change
// at the seam rather than a silent wrong read here.
//
// These readers deliberately keep the reducer's PRE-TYPING tolerant semantics:
// the raw map reads they replace never errored, they returned an empty slice or
// empty string for an absent/typed-wrong key and let the caller's presence/
// literal checks decide. The accessors surface a structural decode error for a
// malformed inner value, but the graph-truth accuracy anchor for a "file" fact
// is its OUTER envelope (repo_id, relative_path, parsed_file_data-is-object),
// already dead-lettered by partitionCodegraphFileFacts (Wave 4f S1). A
// malformed inner sub-object is therefore read as empty here, exactly as before
// this typing, rather than dead-lettering the whole fact on an inner shape the
// outer contract does not require. Byte-identity of the resulting graph rows is
// the S1 gate, proven by the accessor/raw-read equivalence tests in
// parsed_file_data_typed_test.go and the golden-corpus gate.

// parsedFileDataDeadCodeFileRootKinds returns the JavaScript dead-code file
// root-kind literals from a parsed_file_data map through the typed accessor,
// or nil when the key is absent or not a string slice. It mirrors the tolerant
// toStringSlice read resolveFileRootCodeCallCallerID used before this typing.
func parsedFileDataDeadCodeFileRootKinds(fileData map[string]any) []string {
	return factschema.DecodeParsedFileDataDeadCodeFileRootKinds(fileData)
}

// parsedFileDataGomodModulePath returns the declared Go module path from a
// parsed_file_data map's typed gomod_state, or "" when the file is not a parsed
// go.mod carrying a module_path. It replaces the raw
// fileData["gomod_state"].(map[string]any)["module_path"] read in
// goModuleDeclaredPath; a malformed or absent gomod_state reads as "" so the
// caller falls back to its variables-row scan exactly as before.
func parsedFileDataGomodModulePath(fileData map[string]any) string {
	state, ok, err := factschema.DecodeParsedFileDataGomodState(fileData)
	if err != nil || !ok || state.ModulePath == nil {
		return ""
	}
	return *state.ModulePath
}
