// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iamcanassume builds the IAM CAN_ASSUME materialization reducer
// intent from one immutable scope generation: when at least one
// aws_iam_permission fact decodes as a role trust statement (policy_source
// "trust"), it asks the reducer to project the generation's trust statements
// into canonical CAN_ASSUME edges between committed IAM CloudResource nodes.
// Identity-policy statements and facts whose payload fails the typed decode are
// skipped as candidates, so an identity-only generation enqueues nothing and a
// malformed fact never fails the build. The intent shares the
// aws_resource_materialization entity key with the AWS node builders so the
// edge handler gates on the same canonical-nodes-committed phase and never
// projects a trust edge before its role and user endpoints exist. Root
// projector assembly owns lookup construction and lifetime, invocation order,
// queue writes, retries, and telemetry.
package iamcanassume
