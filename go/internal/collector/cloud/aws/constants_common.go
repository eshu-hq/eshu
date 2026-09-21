// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

// Shared resource-type constants that are not owned by a single AWS service
// slice. These appear as relationship targets across multiple service scanners
// and therefore live in the common constants file rather than in a per-service
// file.

const (
	// ResourceTypeAWSAccount identifies an AWS account relationship target.
	ResourceTypeAWSAccount = "aws_account"
	// ResourceTypeGeneric is the fallback relationship target type a scanner
	// emits when a reported identifier does not match a known resource family.
	// It keeps an edge honest with a non-empty target type so downstream
	// correlation can resolve the target later instead of dropping evidence or
	// emitting a dangling edge. Scanners must still carry the original
	// service-reported type in the relationship attributes.
	ResourceTypeGeneric = "aws_resource"
)
