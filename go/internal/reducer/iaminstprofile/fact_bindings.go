// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iaminstprofile

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "iaminstprofile/iaminstprofile_handle", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/iaminstprofile", Symbol: "Handle",
	Kinds: []string{"aws_resource"},
})
