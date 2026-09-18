// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package perform builds the IAM CAN_PERFORM materialization reducer intent
// from one immutable scope generation: when at least one aws_iam_permission
// fact decodes as a trustable identity statement (policy_source "inline" or
// "attached_managed") or at least one aws_resource_policy_permission fact
// decodes, it asks the reducer to project the generation's identity and
// resource-policy permission statements into conservative CAN_PERFORM edges
// between committed IAM CloudResource nodes (issue #1134 PR4a/PR4b). A trust
// statement (policy_source "trust", owned by CAN_ASSUME) and any fact whose
// payload fails the typed decode are skipped as candidates, so a generation
// with only trust statements enqueues nothing and a malformed fact never
// fails the build. The intent shares the aws_resource_materialization entity
// key with the AWS node builders and the CAN_ASSUME trust-statement builder so
// the edge handler gates on the same canonical-nodes-committed phase and never
// projects a CAN_PERFORM edge before its principal and resource endpoints
// exist. Root projector assembly owns lookup construction and lifetime,
// invocation order, queue writes, retries, and telemetry.
package perform
