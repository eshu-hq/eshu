// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

// FactKindRuntimeDeclarationVersion is the "runtime_declaration.version" fact
// kind: one runtime version a repository declares in one file. The value MUST
// stay byte-equal to facts.RuntimeDeclarationVersionFactKind
// (go/internal/facts/runtime_declaration.go); the contracts module cannot
// import go/internal/facts, so the string is duplicated here, per Contract
// System v1 §3.1. Issue #8815.
const FactKindRuntimeDeclarationVersion = "runtime_declaration.version"
