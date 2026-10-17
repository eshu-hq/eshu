// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3grant

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "s3grant/s3grant_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/s3grant", Symbol: "Handle",
	Kinds: []string{"aws_resource"},
})

// S3ExternalPrincipalGrantHandleBinding declares this package's verified reducer consumption of
// s3_external_principal_grant kinds (issue #6629). Build-time metadata only.
var S3ExternalPrincipalGrantHandleBinding = contract.Bind(contract.Binding{
	ID: "s3grant/s3grant_handle_s3_external_principal_grant", FamilyID: "s3_external_principal_grant",
	ReducerDir: "go/internal/reducer/s3grant", Symbol: "Handle",
	Kinds: []string{"s3_external_principal_grant"},
})
