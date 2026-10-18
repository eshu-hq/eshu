// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// ReducerDerivedWriteDriftedFindingsBinding declares this package's verified reducer consumption of
// reducer_derived kinds (issue #6629). Build-time metadata only.
var ReducerDerivedWriteDriftedFindingsBinding = contract.Bind(contract.Binding{
	ID: "codedivergence/codedivergence_write_drifted_findings", FamilyID: "reducer_derived",
	ReducerDir: "go/internal/reducer/codedivergence", Symbol: "WriteDriftedFindings",
	Kinds: []string{"reducer_code_drifted_finding"},
})
