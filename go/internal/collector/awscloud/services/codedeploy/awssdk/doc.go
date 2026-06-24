// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 CodeDeploy calls into scanner-owned
// metadata.
//
// The adapter only calls metadata read operations: ListApplications,
// BatchGetApplications, ListDeploymentGroups, BatchGetDeploymentGroups,
// ListDeploymentConfigs, GetDeploymentConfig, ListDeployments,
// BatchGetDeployments, and ListTagsForResource. It must never call any
// mutation API, the deployment/target instance data plane, or any revision-body
// API that returns appspec.yml content. On-premises instance tag values pass
// through the redaction library before they reach scanner types so PII-shaped
// values are never persisted raw.
package awssdk
