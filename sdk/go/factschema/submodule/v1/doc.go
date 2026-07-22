// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload struct for the
// "submodule" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_submodule.go).
//
// The submodule family has one fact kind, Pin ("submodule.pin"), one git
// submodule reference declared in a parent repository. This package
// (issue #5420) defines the CONTRACT — the fact-kind constant, the typed
// payload struct, its generated JSON Schema, and the registry entry. The
// git collector emits this fact kind and the reducer decodes and projects
// it into a Repository-[:PINS_SUBMODULE]->Repository graph edge. That edge
// needs no dedicated read surface — it is queryable through the generic
// graph tools — so the registry entry (specs/fact-kind-registry.v1.yaml)
// sets read_surface: none, the same recognized sentinel reducer_internal
// uses, rather than naming a route with no consumer behind it.
//
// Pin requires only ParentRepoID and SubmodulePath, the join identity the
// reducer's submodule-edge materializer keys off: every other field
// (SubmoduleURL, ResolvedRepoID, PinnedSHA) is optional because a
// non-dangling observation can legitimately be missing any one of them (see
// Pin's doc comment for the exact cases). The collector-side constraint
// that at least one of SubmoduleURL or PinnedSHA must be known before a
// fact is emitted at all belongs to the collector, not this schema.
//
// Required fields are non-pointer with no omitempty tag; the decode seam
// rejects a payload that omits one, or supplies an explicit JSON null for
// one, with a classified ClassificationInputInvalid error naming the field,
// never a zero-value struct. Optional fields are pointers carrying
// omitempty, so an absent value decodes to nil and stays distinct from an
// observed empty string.
//
// The reducer decodes only the latest struct for this kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package.
package v1
