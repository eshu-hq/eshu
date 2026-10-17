// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rdsposture

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "rdsposture/rdsposture_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/rdsposture", Symbol: "Handle",
	Kinds: []string{"aws_resource"},
})

// RDSPostureHandleBinding declares this package's verified reducer consumption of
// rds_posture kinds (issue #6629). Build-time metadata only.
var RDSPostureHandleBinding = contract.Bind(contract.Binding{
	ID: "rdsposture/rdsposture_handle_rds_posture", FamilyID: "rds_posture",
	ReducerDir: "go/internal/reducer/rdsposture", Symbol: "Handle",
	Kinds: []string{"rds_instance_posture"},
})
