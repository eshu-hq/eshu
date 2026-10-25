// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// RuntimeDeclarationHandleBinding declares this package's verified reducer
// consumption of runtime_declaration kinds (issue #8816). Build-time metadata
// only: the fact-kind-registry generator reads it to bind the kind.
var RuntimeDeclarationHandleBinding = contract.Bind(contract.Binding{
	ID: "declaration/declaration_handle", FamilyID: "runtime_declaration",
	ReducerDir: "go/internal/reducer/runtime/declaration", Symbol: "Handle",
	Kinds: []string{"runtime_declaration.version"},
})
