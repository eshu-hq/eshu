// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

// FactKindCodeownersOwnership is the "codeowners.ownership" fact kind: one
// CODEOWNERS pattern-to-owners mapping. Split into its own file (rather than
// fact_kinds.go) because that file is near this module's 500-line cap. The
// value MUST stay byte-equal to facts.CodeownersOwnershipFactKind
// (go/internal/facts/codeowners.go); the contracts module cannot import
// go/internal/facts, so the string is duplicated here, per Contract System
// v1 §3.1. Issue #5419 (branch-aware CODEOWNERS ingestion), Phase 1.
const FactKindCodeownersOwnership = "codeowners.ownership"
