// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceECR identifies the regional Amazon Elastic Container Registry
	// service scan slice.
	ServiceECR = "ecr"
)

const (
	// ResourceTypeECRRepository identifies an ECR repository.
	ResourceTypeECRRepository = "aws_ecr_repository"
	// ResourceTypeECRLifecyclePolicy identifies an ECR repository lifecycle
	// policy child resource.
	ResourceTypeECRLifecyclePolicy = "aws_ecr_lifecycle_policy"
)
