// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2blockkms

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "ec2blockkms/ec2blockkms_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/ec2blockkms", Symbol: "Handle",
	Kinds: []string{"aws_relationship", "aws_resource"},
})

// EC2InstancePostureHandleBinding declares this package's verified reducer consumption of
// ec2_instance_posture kinds (issue #6629). Build-time metadata only.
var EC2InstancePostureHandleBinding = contract.Bind(contract.Binding{
	ID: "ec2blockkms/ec2blockkms_handle_ec2_instance_posture", FamilyID: "ec2_instance_posture",
	ReducerDir: "go/internal/reducer/ec2blockkms", Symbol: "Handle",
	Kinds: []string{"ec2_instance_posture"},
})
