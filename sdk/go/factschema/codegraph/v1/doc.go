// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "code" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_codegraph.go).
//
// The code family has two fact kinds emitted by the git collector
// (go/internal/collector/git_fact_builder.go): File ("file") and Repository
// ("repository"). This package types only the OUTER envelope identity fields
// the code-graph-core reducer handlers READ to attribute extracted rows to a
// repository and file. The required set tracks what the reducer reads for
// identity, not the full wire shape: File requires repo_id, relative_path,
// and parsed_file_data; Repository requires only repo_id. Fields the collector
// always emits but no reducer read site consumes (graph_id, graph_kind,
// is_dependency, name, parsed_file_count) are OPTIONAL — requiring an emit-only
// field the reducer ignores would dead-letter usable graph truth, the wrong
// contract under Contract System v1's "don't drop right results" accuracy
// guarantee.
//
// File.ParsedFileData stays an UNTYPED map[string]any pass-through by design:
// there is no producer-side struct for the parser's per-file AST, its shape
// varies by language and parser version, and typing it is deferred to issue
// #4750. This mirrors the shipped aws_resource/Attributes open-object pattern
// (sdk/go/factschema/AGENTS.md) — the difference is that ParsedFileData is a
// single named required field, not a remainder-catching Attributes map, so it
// carries no custom MarshalJSON/UnmarshalJSON: the parent module's
// decodeMapInto already assigns a payload map value directly onto a
// map[string]any field of any name (decode_map.go), and a non-object payload
// value fails that assignment with a classified decode error, giving the
// "must be a JSON object" guarantee with no extra code here.
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers, slices, or
// maps carrying omitempty, so an absent value decodes to nil and stays
// distinct from an observed zero. Repository.ParsedFileCount is a STRING (the
// collector formats it with fmt.Sprintf("%d", parsedFileCount)); do not retype
// it as a number.
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
