// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "awscloud/awscloud_handle", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/awscloud", Symbol: "Handle",
	Kinds: []string{"aws_relationship", "aws_resource"},
})
