// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3logsto

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "s3logsto/s3logsto_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/s3logsto", Symbol: "Handle",
	Kinds: []string{"aws_resource"},
})

// S3BucketPostureHandleBinding declares this package's verified reducer consumption of
// s3_bucket_posture kinds (issue #6629). Build-time metadata only.
var S3BucketPostureHandleBinding = contract.Bind(contract.Binding{
	ID: "s3logsto/s3logsto_handle_s3_bucket_posture", FamilyID: "s3_bucket_posture",
	ReducerDir: "go/internal/reducer/s3logsto", Symbol: "Handle",
	Kinds: []string{"s3_bucket_posture"},
})
