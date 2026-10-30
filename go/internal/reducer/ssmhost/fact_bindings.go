// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// SSMHostApplicationHandleBinding declares this package's verified reducer
// consumption of aws_ssm_host_application facts (issue #9558). Build-time
// metadata only.
var SSMHostApplicationHandleBinding = contract.Bind(contract.Binding{
	ID: "ssmhost/ssmhost_handle_aws_ssm_host_application", FamilyID: "aws_ssm_host",
	ReducerDir: "go/internal/reducer/ssmhost", Symbol: "Handle",
	Kinds: []string{"aws_ssm_host_application"},
})

// SSMHostPlatformHandleBinding declares this package's verified reducer
// consumption of aws_ssm_host_platform facts (issue #9558). Build-time
// metadata only.
var SSMHostPlatformHandleBinding = contract.Bind(contract.Binding{
	ID: "ssmhost/ssmhost_handle_aws_ssm_host_platform", FamilyID: "aws_ssm_host",
	ReducerDir: "go/internal/reducer/ssmhost", Symbol: "Handle",
	Kinds: []string{"aws_ssm_host_platform"},
})
