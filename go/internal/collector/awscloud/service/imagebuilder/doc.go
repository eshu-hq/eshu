// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package imagebuilder maps EC2 Image Builder pipeline, recipe, container
// recipe, infrastructure configuration, and distribution configuration metadata
// into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for each Image Builder
// resource type plus relationships for pipeline-to-recipe/config wiring,
// pipeline-to-execution-role, infrastructure-configuration-to-instance-profile/
// subnet/security-group/SNS-topic/S3-log-bucket, and container-recipe-to-ECR-
// repository/KMS-key evidence. Component build-document bodies, Dockerfile
// template bodies, instance user data, and build artifacts stay outside this
// package contract: the scanner is metadata-only and synthesizes only
// partition-aware ARNs for the targets whose nodes the partner scanners publish.
package imagebuilder
