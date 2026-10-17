// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package owners

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// CodeownersHandleBinding declares this package's verified reducer consumption of
// codeowners kinds (issue #6629). Build-time metadata only.
var CodeownersHandleBinding = contract.Bind(contract.Binding{
	ID: "owners/owners_handle", FamilyID: "codeowners",
	ReducerDir: "go/internal/reducer/code/owners", Symbol: "Handle",
	Kinds: []string{"codeowners.ownership"},
})
