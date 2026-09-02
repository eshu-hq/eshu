// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iaminstanceprofile builds the IAM instance-profile-role
// materialization reducer intent from one immutable scope generation: when the
// generation carries an aws_resource fact whose decoded resource_type is
// aws_iam_instance_profile, it asks the reducer to project the profiles'
// role_arns into canonical CloudResource HAS_ROLE edges (issue #1299). The
// trigger anchors to the first instance-profile fact even when role_arns is
// empty, because a no-role generation still has to retract stale reducer-owned
// HAS_ROLE edges from a prior generation. The intent shares the
// aws_resource_materialization entity key with the AWS node builders so the
// edge handler gates on the same canonical-nodes-committed phase and never
// writes an edge before the profile and role CloudResource nodes commit. The
// package decodes aws_resource through its own sdk/go/factschema seam
// (factschema_decode_aws.go) rather than root projector's wrapper. Root
// projector assembly owns lookup construction and lifetime, invocation order,
// queue writes, retries, and telemetry.
package iaminstanceprofile
