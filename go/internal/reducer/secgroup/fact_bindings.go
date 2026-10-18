// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secgroup

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "secgroup/secgroup_handle", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/secgroup", Symbol: "Handle",
	Kinds: []string{"aws_resource", "aws_security_group_rule"},
})
