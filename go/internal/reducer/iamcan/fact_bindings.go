// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "iamcan/iamcan_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/iamcan", Symbol: "Handle",
	Kinds: []string{"aws_iam_permission", "aws_resource", "aws_resource_policy_permission"},
})

// SecretsIAMHandleBinding declares this package's verified reducer consumption of
// secrets_iam kinds (issue #6629). Build-time metadata only.
var SecretsIAMHandleBinding = contract.Bind(contract.Binding{
	ID: "iamcan/iamcan_handle_secrets_iam", FamilyID: "secrets_iam",
	ReducerDir: "go/internal/reducer/iamcan", Symbol: "Handle",
	Kinds: []string{"aws_iam_permission_boundary"},
})
