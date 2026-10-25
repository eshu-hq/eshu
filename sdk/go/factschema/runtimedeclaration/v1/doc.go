// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload struct for the
// "runtime_declaration" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #8815, epic #8807).
//
// The family has one fact kind today, Version ("runtime_declaration.version"):
// one runtime version a repository declares in one file, such as a ".nvmrc"
// line or one token of a ".tool-versions" line. It is decoded through the
// parent factschema package's kind-keyed seam
// (factschema.DecodeRuntimeDeclarationVersion, decode_runtime_declaration.go).
//
// This package is the payload contract only. The git collector emits the fact
// from the version-pin files, the engines, volta and packageManager sections of
// package.json, and the go and toolchain directives of go.mod
// (go/internal/collector/repo/runtime/declaration).
// The reducer projection (#8816) and the query tool (#8817) are separate
// changes, so the kind is disclosed unconsumed until they land.
//
// Nine fields are required: non-pointer with no omitempty tag, so the decode
// seam rejects a payload that omits one, or supplies an explicit JSON null for
// one, with a classified ClassificationInputInvalid error naming the field and
// never a zero-value struct. Every other field is an omitempty pointer, so an
// absent version part or range bound decodes to nil and stays distinct from
// zero.
//
// SpecKind is a closed set for producers with a tolerant reader: the decoder
// does not check membership, and the generated JSON Schema carries no enum, so
// a future member never turns a rolling upgrade into a rejected fact.
//
// The reducer decodes only the latest struct for this kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package.
package v1
