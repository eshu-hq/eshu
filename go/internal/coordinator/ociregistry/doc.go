// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ociregistry plans workflow rows for configured OCI registry
// repository targets without opening any registry connection.
//
// WorkPlanner validates one enabled, claim-capable collector instance,
// parses and normalizes its configured Docker Hub, GHCR, ECR, Google
// Artifact Registry, Azure Container Registry, JFrog, and Harbor targets
// into a shared repository identity, rejects duplicate normalized targets,
// and returns one deterministic workflow run and one claimable work item per
// target. The parent coordinator owns scheduling order, the plan-key clock,
// durable open-target admission, retries, and telemetry; this package
// resolves no credentials and calls no registry API.
package ociregistry
