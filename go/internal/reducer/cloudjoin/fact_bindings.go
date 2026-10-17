// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudjoin

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSBuildCloudResourceJoinIndexBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSBuildCloudResourceJoinIndexBinding = contract.Bind(contract.Binding{
	ID: "cloudjoin/cloudjoin_build_cloud_resource_join_index", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/cloudjoin", Symbol: "BuildCloudResourceJoinIndex",
	Kinds: []string{"aws_relationship", "aws_resource"},
})
