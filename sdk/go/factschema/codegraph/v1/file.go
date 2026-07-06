// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// File is the schema-version-1 typed payload for the "file" fact kind
// (Contract System v1 §3.1, docs/internal/design/contract-system-v1.md).
//
// A file fact is the git collector's per-source-file emission
// (go/internal/collector/git_fact_builder.go fileFactEnvelope). RepoID,
// RelativePath, and ParsedFileData are the join identity the code-graph-core
// reducer handlers key their extraction on
// (go/internal/reducer/code_call_materialization_extract.go,
// code_import_repo_edge.go, code_import_repo_edge_retract.go): before this
// contract, a fact missing repo_id or relative_path decoded through
// payloadStr(env.Payload, "repo_id") to "", producing an empty-string graph
// identity segment instead of a visible failure (issue #4749 §1 accuracy
// hole). They are REQUIRED here so that gap dead-letters as input_invalid
// instead.
//
// The required set tracks what the reducer READS for identity/extraction, not
// what the collector always emits. GraphID, GraphKind, and IsDependency are
// OPTIONAL despite being unconditionally emitted, because no reducer read site
// consumes them from the file payload: GraphID is a redundant derivation of
// RepoID:RelativePath, GraphKind is a constant discriminator, and no
// code-graph-core handler branches on IsDependency. Requiring an emit-only
// field the reducer ignores would dead-letter usable graph truth — the wrong
// contract (Contract System v1's accuracy guarantee is "don't drop right
// results", not "assert the full wire shape").
//
// ParsedFileData carries the parser's untyped per-file AST/analysis map
// (go/internal/parser) verbatim. It is REQUIRED-PRESENT and MUST decode as a
// JSON object (map[string]any) — the code-graph-core handlers reach into it
// for every edge — but its INNER shape is intentionally left unmodeled: the
// parser's output varies by language and by parser version, and typing that
// AST is deferred follow-up work tracked by issue #4750. Reducer code must
// keep reading ParsedFileData's inner keys (`imports`, `functions`,
// `function_calls`, ...) as an untyped map, exactly as before this contract;
// only the outer envelope identity is typed here.
type File struct {
	// RepoID is the owning repository's canonical id. Required: it is the join
	// key every code-graph-core reducer handler groups file facts by
	// (collectCodeCallRepositoryIDs, BuildCodeImportRepoDependencyIntents). A
	// fact missing repo_id cannot be attributed to any repository and MUST
	// dead-letter rather than silently join under an empty-string repo id.
	RepoID string `json:"repo_id"`

	// RelativePath is the file's path relative to its repository root.
	// Required: combined with RepoID it forms the file's graph identity, and
	// code-call extraction reads it directly for edge provenance
	// (extractGenericCodeCallRows).
	RelativePath string `json:"relative_path"`

	// ParsedFileData is the parser's untyped per-file payload (imports,
	// functions, function_calls, classes, and other language-specific
	// analysis keys). Required-present and must decode as a JSON object; its
	// inner shape is intentionally opaque (deferred to issue #4750, the
	// inner-AST typing follow-up) and MUST be read as map[string]any by
	// consumers, mirroring the aws_resource/Attributes open-object pattern
	// documented in sdk/go/factschema/AGENTS.md.
	ParsedFileData map[string]any `json:"parsed_file_data"`

	// GraphID is the canonical graph node identity the collector assigns this
	// file ("<repo_id>:<relative_path>"). Optional: no reducer read site
	// consumes it — it is a redundant derivation of RepoID and RelativePath —
	// so requiring it would dead-letter a fact the reducer can fully process
	// from its RepoID/RelativePath alone.
	GraphID *string `json:"graph_id,omitempty"`

	// GraphKind discriminates this envelope's graph node kind (the emitter
	// always writes the literal "file"). Optional: a constant discriminator
	// no code-graph-core reducer read site consumes from the payload.
	GraphKind *string `json:"graph_kind,omitempty"`

	// IsDependency reports whether this file was observed in a vendored or
	// dependency-tree location rather than the primary repository source.
	// Optional: unconditionally emitted, but no code-graph-core handler
	// branches on it, so requiring it would dead-letter usable graph truth.
	IsDependency *bool `json:"is_dependency,omitempty"`

	// Language is the parser-detected source language, when the parser
	// reports one. Optional: fileFactEnvelope only sets this key when
	// payloadString(fileData, "language", "lang") returns a non-empty value,
	// so an absent Language is a valid "language not detected" observation,
	// not a malformed fact.
	Language *string `json:"language,omitempty"`
}
