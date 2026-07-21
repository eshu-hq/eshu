// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload struct for the
// "codeowners" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #5419).
//
// This package types the family's one fact kind today: Ownership
// (codeowners.ownership), one CODEOWNERS pattern-to-owners mapping per
// struct value. It is decoded through the parent factschema package's
// kind-keyed seam (factschema.DecodeCodeownersOwnership,
// decode_codeowners.go).
//
// This is Phase 1 of issue #5419's branch-aware CODEOWNERS ingestion epic
// (#5415): the contract only. The collector that parses CODEOWNERS files and
// emits this fact, and the reducer/query consumer that decodes it, land in
// later phases. Until then this struct has no decode-side caller other than
// this module's own conformance tests.
//
// RepoID, SourcePath, Pattern, Owners, and OrderIndex are all required:
// non-pointer fields with no omitempty tag decode-reject a payload that
// omits one, or supplies an explicit JSON null for one, with a classified
// ClassificationInputInvalid error naming the field, never a zero-value
// struct. CollectorInstanceID is optional (a pointer field carrying
// omitempty): an absent value decodes to nil, not a defaulted empty string.
//
// The reducer would decode only the latest struct for this kind. Version
// shims for an older schema major belong in the parent factschema package's
// decode seam (decodeLatestMajor in decode.go), never in this package.
package v1
