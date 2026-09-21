// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 EC2 responses to the VPC scanner
// contract.
//
// The adapter intentionally uses only DescribeXxx and read paginator
// operations. No mutation method (Create/Delete/Modify/Associate/Disassociate/
// Authorize/Revoke/Allocate/Release/Replace/Accept/Reject/Attach/Detach) is
// embedded into the narrow apiClient interface, and a reflect-driven test
// pins that boundary against regressions.
package awssdk
