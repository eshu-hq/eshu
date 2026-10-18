// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package internetexposure

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "internetexposure/internetexposure_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/internetexposure", Symbol: "Handle",
	Kinds: []string{"aws_relationship", "aws_resource", "aws_security_group_rule"},
})

// EC2InstancePostureHandleBinding declares this package's verified reducer consumption of
// ec2_instance_posture kinds (issue #6629). Build-time metadata only.
var EC2InstancePostureHandleBinding = contract.Bind(contract.Binding{
	ID: "internetexposure/internetexposure_handle_ec2_instance_posture", FamilyID: "ec2_instance_posture",
	ReducerDir: "go/internal/reducer/internetexposure", Symbol: "Handle",
	Kinds: []string{"ec2_instance_posture"},
})

// S3BucketPostureHandleBinding declares this package's verified reducer consumption of
// s3_bucket_posture kinds (issue #6629). Build-time metadata only.
var S3BucketPostureHandleBinding = contract.Bind(contract.Binding{
	ID: "internetexposure/internetexposure_handle_s3_bucket_posture", FamilyID: "s3_bucket_posture",
	ReducerDir: "go/internal/reducer/internetexposure", Symbol: "Handle",
	Kinds: []string{"s3_bucket_posture"},
})
