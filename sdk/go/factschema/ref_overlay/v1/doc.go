// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "ref_overlay" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #5393).
//
// This package types the family's two fact kinds: BranchObservation
// (ref_overlay.branch_observation), one deployment-referenced non-default
// ref, and ConfigKeyDivergence (config_key_divergence), one per-environment
// config-key divergence row carrying keys plus a divergence boolean only.
// Both are decoded through the parent factschema package's kind-keyed seam
// (factschema.DecodeRefOverlayBranchObservation and
// factschema.DecodeConfigKeyDivergence, decode_ref_overlay.go).
//
// This package is the payload contract for issue #5393's Phase 1 isolated
// branch/config observations. The collector that extracts deployed refs and
// compares configs (go/internal/collector), the reducer-side isolation
// handling, and the ref-scoped read surfaces (go/internal/query,
// go/internal/mcp) all consume it — that runtime wiring lives in those
// go/internal packages and lands in sibling Phase 1 leaves, keeping this SDK
// module standalone with no Eshu-internal imports.
//
// Required fields are non-pointer with no omitempty tag; the decode seam
// rejects a payload that omits one, or supplies an explicit JSON null for
// one, with a classified ClassificationInputInvalid error naming the field,
// never a zero-value struct. Optional fields are pointers carrying
// omitempty, so an absent value decodes to nil and stays distinct from an
// observed empty string.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major belong in the parent factschema package's
// decode seam (decodeLatestMajor in decode.go), never in this package.
package v1
