// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package escalation builds the IAM privilege-escalation materialization
// reducer intent from one immutable scope generation: when at least one
// aws_iam_permission fact decodes as an identity statement (policy_source
// "inline" or "attached_managed"), it asks the reducer to project the
// generation's identity statements into conservative CAN_ESCALATE_TO edges
// between committed IAM CloudResource nodes (#6228). A trust statement
// (policy_source "trust", owned by CAN_ASSUME) and any fact whose payload
// fails the typed decode are skipped as candidates, so a generation with
// only trust statements enqueues nothing and a malformed fact never fails
// the build. Either effect qualifies: an Allow can arm a catalog primitive
// and a Deny contributes to the grant's deny set. The intent shares the
// aws_resource_materialization entity key with the AWS node builders and
// the sibling assume/perform builders so the edge handler gates on the same
// canonical-nodes-committed phase and never projects a CAN_ESCALATE_TO edge
// before its principal and target endpoints exist. Root projector assembly
// owns lookup construction and lifetime, invocation order, queue writes,
// retries, and telemetry.
package escalation
