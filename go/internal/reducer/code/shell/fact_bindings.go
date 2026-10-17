// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shell

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// CodeHandleBinding declares this package's verified reducer consumption of
// code kinds (issue #6629). Build-time metadata only.
var CodeHandleBinding = contract.Bind(contract.Binding{
	ID: "shell/shell_handle", FamilyID: "code",
	ReducerDir: "go/internal/reducer/code/shell", Symbol: "Handle",
	Kinds: []string{"file", "repository"},
})
